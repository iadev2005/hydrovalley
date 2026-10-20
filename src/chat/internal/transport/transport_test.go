package transport

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/golang-jwt/jwt/v5"
	chatv1 "github.com/iadev2005/hydrovalley/gen/go/hydrovalley/chat/v1"
	"github.com/iadev2005/hydrovalley/src/chat/internal/auth"
	"github.com/iadev2005/hydrovalley/src/chat/internal/store"
	"google.golang.org/protobuf/encoding/protojson"
)

type testEnv struct {
	server   *httptest.Server
	pubKey   ed25519.PublicKey
	privKey  ed25519.PrivateKey
	store    *store.Store
	hub      *Hub
	verifier *auth.Verifier
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	st, err := store.New("")
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}

	h := NewHub(st.ServerEpoch())
	v := auth.NewVerifier(pub, time.Now)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	mux := NewHandler(st, h, v, logger, Options{})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	return &testEnv{
		server:   ts,
		pubKey:   pub,
		privKey:  priv,
		store:    st,
		hub:      h,
		verifier: v,
	}
}

func (e *testEnv) signToken(t *testing.T, playerID, username string) string {
	t.Helper()
	claims := jwt.MapClaims{
		"iss":       auth.Issuer,
		"aud":       auth.Audience,
		"sub":       playerID,
		"player_id": playerID,
		"username":  username,
		"role":      "player",
		"exp":       time.Now().Add(time.Hour).Unix(),
		"iat":       time.Now().Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	s, err := tok.SignedString(e.privKey)
	if err != nil {
		t.Fatalf("SignedString: %v", err)
	}
	return s
}

func uuidN(n int) string {
	return fmt.Sprintf("00000000-0000-4000-8000-%012d", n)
}

func TestHandler_HealthzAndReadyz(t *testing.T) {
	env := newTestEnv(t)

	// Healthz
	resp, err := http.Get(env.server.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("healthz status: got %d, want 200", resp.StatusCode)
	}

	// Readyz
	resp2, err := http.Get(env.server.URL + "/readyz")
	if err != nil {
		t.Fatalf("GET /readyz: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Errorf("readyz status: got %d, want 200", resp2.StatusCode)
	}
	var data map[string]any
	if err := json.NewDecoder(resp2.Body).Decode(&data); err != nil {
		t.Fatalf("decode readyz: %v", err)
	}
	if data["ready"] != true {
		t.Errorf("ready field: got %v, want true", data["ready"])
	}
}

func TestHandler_Chat_RejectUnauthorized(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	wsURL := "ws" + strings.TrimPrefix(env.server.URL, "http") + "/chat"

	// Sin token
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial without token: %v", err)
	}
	defer conn.CloseNow()
	_, _, readErr := conn.Read(ctx)
	if websocket.CloseStatus(readErr) != closeUnauthorized {
		t.Errorf("expected close 4401 on missing token, got %v", readErr)
	}

	// Token inválido
	conn2, _, err := websocket.Dial(ctx, wsURL+"?token=invalid.token.here", nil)
	if err != nil {
		t.Fatalf("dial with invalid token: %v", err)
	}
	defer conn2.CloseNow()
	_, _, readErr2 := conn2.Read(ctx)
	if websocket.CloseStatus(readErr2) != closeUnauthorized {
		t.Errorf("expected close 4401 on invalid token, got %v", readErr2)
	}
}

func TestHandler_Chat_SendAndBroadcast(t *testing.T) {
	env := newTestEnv(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wsURL := "ws" + strings.TrimPrefix(env.server.URL, "http") + "/chat"

	tok1 := env.signToken(t, uuidN(1), "jugador1")
	tok2 := env.signToken(t, uuidN(2), "jugador2")

	conn1, _, err := websocket.Dial(ctx, wsURL+"?token="+tok1, nil)
	if err != nil {
		t.Fatalf("dial conn1: %v", err)
	}
	defer conn1.CloseNow()

	conn2, _, err := websocket.Dial(ctx, wsURL+"?token="+tok2, nil)
	if err != nil {
		t.Fatalf("dial conn2: %v", err)
	}
	defer conn2.CloseNow()

	// El suscriptor se registra tras el upgrade: esperar a ambos para que el
	// mensaje no se difunda antes de que conn2 esté suscrito.
	deadline := time.Now().Add(2 * time.Second)
	for env.hub.SubscribersCount() < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}

	// Conn1 envía un SendFrame
	sendMsg := `{"type":"send","message_id":"` + uuidN(10) + `","text":"hola mundo"}`
	if err := conn1.Write(ctx, websocket.MessageText, []byte(sendMsg)); err != nil {
		t.Fatalf("conn1 Write: %v", err)
	}

	// Ambos deben recibir el MessageFrame difundido
	checkMessageFrame := func(conn *websocket.Conn, who string) {
		_, raw, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("%s Read: %v", who, err)
		}
		var mf chatv1.MessageFrame
		if err := protojson.Unmarshal(raw, &mf); err != nil {
			t.Fatalf("%s unmarshal: %v (raw=%s)", who, err, string(raw))
		}
		if mf.GetType() != "message" {
			t.Errorf("%s type: got %q, want message", who, mf.GetType())
		}
		if mf.GetSeq() != 1 {
			t.Errorf("%s seq: got %d, want 1", who, mf.GetSeq())
		}
		if mf.GetMessageId() != uuidN(10) {
			t.Errorf("%s message_id: got %q", who, mf.GetMessageId())
		}
		if mf.GetUsername() != "jugador1" {
			t.Errorf("%s username: got %q, want jugador1", who, mf.GetUsername())
		}
		if mf.GetText() != "hola mundo" {
			t.Errorf("%s text: got %q", who, mf.GetText())
		}
		if mf.GetKind() != "player" {
			t.Errorf("%s kind: got %q", who, mf.GetKind())
		}
	}

	checkMessageFrame(conn1, "conn1 (sender)")
	checkMessageFrame(conn2, "conn2 (receiver)")
}

func TestHandler_Chat_Sync(t *testing.T) {
	env := newTestEnv(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Pre-insertar 3 mensajes
	for i := 1; i <= 3; i++ {
		_, _, _ = env.store.Append(&chatv1.SendMessageRequest{
			MessageId: uuidN(i),
			SenderId:  uuidN(10),
			Username:  "sistema",
			Text:      fmt.Sprintf("noticia %d", i),
			Kind:      chatv1.MessageKind_MESSAGE_KIND_SYSTEM,
		}, nil)
	}

	wsURL := "ws" + strings.TrimPrefix(env.server.URL, "http") + "/chat"
	tok := env.signToken(t, uuidN(5), "jugador5")

	conn, _, err := websocket.Dial(ctx, wsURL+"?token="+tok, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.CloseNow()

	// Enviar SyncFrame pidiendo después del seq 1 (debe devolver mensajes 2 y 3 y luego SyncedFrame)
	syncMsg := `{"type":"sync","after_seq":"1"}`
	if err := conn.Write(ctx, websocket.MessageText, []byte(syncMsg)); err != nil {
		t.Fatalf("write sync: %v", err)
	}

	// Mensaje 2
	_, raw2, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read msg 2: %v", err)
	}
	var mf2 chatv1.MessageFrame
	if err := protojson.Unmarshal(raw2, &mf2); err != nil {
		t.Fatalf("unmarshal msg 2: %v", err)
	}
	if mf2.GetSeq() != 2 || mf2.GetKind() != "system" {
		t.Errorf("msg 2: seq=%d, kind=%q", mf2.GetSeq(), mf2.GetKind())
	}

	// Mensaje 3
	_, raw3, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read msg 3: %v", err)
	}
	var mf3 chatv1.MessageFrame
	if err := protojson.Unmarshal(raw3, &mf3); err != nil {
		t.Fatalf("unmarshal msg 3: %v", err)
	}
	if mf3.GetSeq() != 3 {
		t.Errorf("msg 3: seq=%d", mf3.GetSeq())
	}

	// SyncedFrame
	_, rawSynced, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read synced: %v", err)
	}
	var sf chatv1.SyncedFrame
	if err := protojson.Unmarshal(rawSynced, &sf); err != nil {
		t.Fatalf("unmarshal synced: %v", err)
	}
	if sf.GetType() != "synced" {
		t.Errorf("synced type: got %q", sf.GetType())
	}
	if sf.GetLastSeq() != 3 {
		t.Errorf("synced last_seq: got %d, want 3", sf.GetLastSeq())
	}
	if sf.GetServerEpoch() != env.store.ServerEpoch() {
		t.Errorf("synced server_epoch mismatch")
	}
}
