package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func newTestAPI(t *testing.T, h http.Handler) (*API, *[]time.Duration) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	api, err := NewAPI(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	slept := []time.Duration{}
	api.Sleep = func(_ context.Context, d time.Duration) error {
		mu.Lock()
		defer mu.Unlock()
		slept = append(slept, d)
		return nil
	}
	return api, &slept
}

func TestLoginReturnsToken(t *testing.T) {
	api, _ := newTestAPI(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/auth/login" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["username"] != "admin" || body["password"] != "secret" {
			t.Errorf("unexpected body %v", body)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"token":"tok-1","expires_at":"2026-10-03T12:00:00Z"}`))
	}))
	tok, err := api.Login(context.Background(), "admin", "secret")
	if err != nil || tok != "tok-1" {
		t.Fatalf("Login = %q, %v", tok, err)
	}
}

func TestLoginRejectedIsErrUnauthorized(t *testing.T) {
	api, _ := newTestAPI(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid credentials"}`))
	}))
	_, err := api.Login(context.Background(), "admin", "bad")
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("want ErrUnauthorized, got %v", err)
	}
}

func TestProvisionBatchesOfAtMost100(t *testing.T) {
	var mu sync.Mutex
	var counts []int
	api, _ := newTestAPI(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer admin-tok" {
			t.Errorf("missing bearer token")
		}
		var body struct {
			Count  int    `json:"count"`
			Prefix string `json:"prefix"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		counts = append(counts, body.Count)
		mu.Unlock()
		accs := make([]Account, body.Count)
		for i := range accs {
			accs[i] = Account{PlayerID: "p", Username: "bot-x", Token: "t"}
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"accounts": accs})
	}))
	accs, err := api.ProvisionBots(context.Background(), "admin-tok", 250, "bot")
	if err != nil || len(accs) != 250 {
		t.Fatalf("got %d accounts, err %v", len(accs), err)
	}
	if len(counts) != 3 || counts[0] != 100 || counts[1] != 100 || counts[2] != 50 {
		t.Fatalf("batches = %v", counts)
	}
}

func TestProvisionRetriesOn429HonoringRetryAfter(t *testing.T) {
	var calls int
	api, slept := newTestAPI(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Retry-After", "7")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"slow down"}`))
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"accounts":[{"player_id":"p","username":"bot-a","token":"t"}]}`))
	}))
	accs, err := api.ProvisionBots(context.Background(), "tok", 1, "bot")
	if err != nil || len(accs) != 1 {
		t.Fatalf("got %v, %v", accs, err)
	}
	if calls != 2 || len(*slept) != 1 || (*slept)[0] != 7*time.Second {
		t.Fatalf("calls=%d slept=%v", calls, *slept)
	}
}

func TestProvisionGivesUpAfterMaxRetries(t *testing.T) {
	api, _ := newTestAPI(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	api.MaxRetries = 2
	_, err := api.ProvisionBots(context.Background(), "tok", 1, "bot")
	var he *HTTPError
	if !errors.As(err, &he) || he.Status != http.StatusTooManyRequests {
		t.Fatalf("want 429 HTTPError, got %v", err)
	}
}

func TestProvisionForbiddenAndServerErrorAreNotRetried(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusServiceUnavailable} {
		var calls int
		api, _ := newTestAPI(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			calls++
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"error":"nope","created":2}`))
		}))
		_, err := api.ProvisionBots(context.Background(), "tok", 3, "bot")
		var he *HTTPError
		if !errors.As(err, &he) || he.Status != status || calls != 1 {
			t.Fatalf("status %d: err=%v calls=%d", status, err, calls)
		}
	}
}

func TestProvisionRejectsShortResponse(t *testing.T) {
	api, _ := newTestAPI(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"accounts":[]}`))
	}))
	if _, err := api.ProvisionBots(context.Background(), "tok", 2, "bot"); err == nil {
		t.Fatal("expected error for a response with fewer accounts than requested")
	}
}

func TestGetRankingSuccess(t *testing.T) {
	api, _ := newTestAPI(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("want GET, got %s", r.Method)
		}
		if r.URL.Path != "/api/ranking" || r.URL.RawQuery != "limit=10" {
			t.Errorf("unexpected URL: %s?%s", r.URL.Path, r.URL.RawQuery)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("missing or invalid authorization header")
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"tick": "42",
			"entries": [
				{"rank": 1, "player_id": "p1", "username": "alice", "leaves_earned": "100"},
				{"rank": 2, "player_id": "p2", "username": "bob", "leaves_earned": "50"}
			]
		}`))
	}))

	res, lat, err := api.GetRanking(context.Background(), "test-token", 10)
	if err != nil {
		t.Fatalf("GetRanking error: %v", err)
	}
	if lat <= 0 {
		t.Errorf("want positive latency, got %v", lat)
	}
	if res.Tick != "42" || len(res.Entries) != 2 {
		t.Fatalf("unexpected response: %+v", res)
	}
	if res.Entries[0].Username != "alice" || res.Entries[0].LeavesEarned != "100" {
		t.Errorf("unexpected entry 0: %+v", res.Entries[0])
	}
}

func TestGetRankingUnauthorized(t *testing.T) {
	api, _ := newTestAPI(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"Hace falta un token válido: Authorization: Bearer <token>"}`))
	}))

	_, _, err := api.GetRanking(context.Background(), "invalid-token", 10)
	if err == nil {
		t.Fatal("expected error for unauthorized request")
	}
	var he *HTTPError
	if !errors.As(err, &he) || he.Status != http.StatusUnauthorized {
		t.Fatalf("want 401 HTTPError, got %v", err)
	}
}

