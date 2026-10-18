package transport

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"
	chatv1 "github.com/iadev2005/hydrovalley/gen/go/hydrovalley/chat/v1"
	"github.com/iadev2005/hydrovalley/src/chat/internal/auth"
	"github.com/iadev2005/hydrovalley/src/chat/internal/store"
	"google.golang.org/protobuf/encoding/protojson"
)

const (
	// readLimit es el tamaño máximo de un frame entrante del navegador (4 KB).
	readLimit = 4 << 10
	// writeTimeout es el plazo máximo para escribir a un cliente WebSocket.
	writeTimeout = 5 * time.Second
	// closeUnauthorized es el código de cierre WebSocket (4401) ante token ausente o inválido.
	closeUnauthorized websocket.StatusCode = 4401
)

// Handler administra las rutas HTTP y WebSocket del servicio de chat.
type Handler struct {
	store    *store.Store
	hub      *Hub
	verifier *auth.Verifier
	log      *slog.Logger
	opts     Options
}

// NewHandler monta el multiplexor HTTP con /healthz, /readyz y /chat.
func NewHandler(st *store.Store, h *Hub, v *auth.Verifier, log *slog.Logger, opts Options) http.Handler {
	handler := &Handler{
		store:    st,
		hub:      h,
		verifier: v,
		log:      log,
		opts:     opts,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handler.handleHealthz)
	mux.HandleFunc("GET /readyz", handler.handleReadyz)
	mux.HandleFunc("GET /chat", handler.handleChat)
	return mux
}

func (h *Handler) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok\n"))
}

func (h *Handler) handleReadyz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ready":        true,
		"last_seq":     strconv.FormatUint(h.store.LastSeq(), 10),
		"server_epoch": h.store.ServerEpoch(),
		"subscribers":  h.hub.SubscribersCount(),
	})
}

func (h *Handler) handleChat(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: h.opts.OriginPatterns,
	})
	if err != nil {
		h.log.Debug("websocket upgrade failed", "err", err)
		return
	}
	defer conn.CloseNow()
	conn.SetReadLimit(readLimit)

	token := r.URL.Query().Get("token")
	if h.verifier == nil || strings.TrimSpace(token) == "" {
		_ = conn.Close(closeUnauthorized, "token is required")
		return
	}

	id, err := h.verifier.Verify(token)
	if err != nil {
		h.log.Debug("/chat unauthorized", "reason", err)
		_ = conn.Close(closeUnauthorized, "invalid or expired token")
		return
	}

	sub := h.hub.Subscribe()
	defer h.hub.Unsubscribe(sub)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// Goroutine de escritura: lee de sub.send y escribe en el socket
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.writeLoop(ctx, conn, sub)
	}()

	// Bucle de lectura de frames del cliente
	h.readLoop(ctx, conn, id)
	<-done
}

func (h *Handler) writeLoop(ctx context.Context, conn *websocket.Conn, sub *Subscriber) {
	for {
		select {
		case <-ctx.Done():
			return
		case data, ok := <-sub.send:
			if !ok {
				return
			}
			writeCtx, cancel := context.WithTimeout(ctx, writeTimeout)
			err := conn.Write(writeCtx, websocket.MessageText, data)
			cancel()
			if err != nil {
				return
			}
		}
	}
}

func (h *Handler) readLoop(ctx context.Context, conn *websocket.Conn, id auth.Identity) {
	for {
		typ, raw, err := conn.Read(ctx)
		if err != nil {
			return
		}
		if typ != websocket.MessageText {
			continue
		}

		var env chatv1.ChatEnvelope
		if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(raw, &env); err != nil {
			continue
		}

		switch env.GetType() {
		case "send":
			var sendFrame chatv1.SendFrame
			if err := protojson.Unmarshal(raw, &sendFrame); err != nil {
				continue
			}
			if !auth.IsUUID(sendFrame.GetMessageId()) || strings.TrimSpace(sendFrame.GetText()) == "" {
				continue
			}

			req := &chatv1.SendMessageRequest{
				MessageId: sendFrame.GetMessageId(),
				SenderId:  id.PlayerID,
				Username:  id.Username,
				Text:      sendFrame.GetText(),
				Kind:      chatv1.MessageKind_MESSAGE_KIND_PLAYER,
			}
			msg, isNew, err := h.store.Append(req, nil)
			if err != nil {
				continue
			}
			if isNew {
				h.hub.Broadcast(msg)
			}

		case "sync":
			var syncFrame chatv1.SyncFrame
			if err := protojson.Unmarshal(raw, &syncFrame); err != nil {
				continue
			}

			h.handleSync(ctx, conn, syncFrame.GetAfterSeq())
		}
	}
}

func (h *Handler) handleSync(ctx context.Context, conn *websocket.Conn, afterSeq uint64) {
	messages, lastSeq, epoch := h.store.ListAfter(afterSeq, 0)

	// Enviar cada mensaje omitido directamente a esta conexión
	for _, msg := range messages {
		kindStr := "player"
		if msg.GetKind() == chatv1.MessageKind_MESSAGE_KIND_SYSTEM {
			kindStr = "system"
		}
		msgFrame := &chatv1.MessageFrame{
			Type:        "message",
			Seq:         msg.GetSeq(),
			MessageId:   msg.GetMessageId(),
			SenderId:    msg.GetSenderId(),
			Username:    msg.GetUsername(),
			Text:        msg.GetText(),
			Kind:        kindStr,
			SentAt:      msg.GetSentAt(),
			ServerEpoch: epoch,
		}
		data, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(msgFrame)
		if err != nil {
			return
		}
		writeCtx, cancel := context.WithTimeout(ctx, writeTimeout)
		err = conn.Write(writeCtx, websocket.MessageText, data)
		cancel()
		if err != nil {
			return
		}
	}

	// Cerrar la respuesta de sincronización con SyncedFrame
	syncedFrame := &chatv1.SyncedFrame{
		Type:        "synced",
		ServerEpoch: epoch,
		LastSeq:     lastSeq,
	}
	syncedData, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(syncedFrame)
	if err != nil {
		return
	}
	writeCtx, cancel := context.WithTimeout(ctx, writeTimeout)
	_ = conn.Write(writeCtx, websocket.MessageText, syncedData)
	cancel()
}
