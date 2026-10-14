package client

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func tlsServer(t *testing.T, h http.Handler) *httptest.Server {
	t.Helper()
	srv := httptest.NewTLSServer(h)
	t.Cleanup(srv.Close)
	return srv
}

func trusting(srv *httptest.Server) *tls.Config {
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	return &tls.Config{RootCAs: pool}
}

func loginHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"token":"tok-tls"}`))
	})
}

func TestAPIRejectsUntrustedCertificateByDefault(t *testing.T) {
	srv := tlsServer(t, loginHandler())
	api, err := NewAPI(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	_, err = api.Login(context.Background(), "a", "b")
	if err == nil || !strings.Contains(err.Error(), "certificate") {
		t.Fatalf("want a certificate error, got %v", err)
	}
}

func TestAPIAcceptsCertificateWithTrustedRootOrInsecure(t *testing.T) {
	srv := tlsServer(t, loginHandler())
	for name, cfg := range map[string]*tls.Config{
		"trusted root": trusting(srv),
		"insecure":     {InsecureSkipVerify: true},
	} {
		api, err := NewAPI(srv.URL)
		if err != nil {
			t.Fatal(err)
		}
		api.UseTLS(cfg)
		tok, err := api.Login(context.Background(), "a", "b")
		if err != nil || tok != "tok-tls" {
			t.Errorf("%s: Login = %q, %v", name, tok, err)
		}
	}
}

func TestSessionConnectsOverWSSWithTLSConfig(t *testing.T) {
	connected := make(chan struct{}, 1)
	srv := tlsServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		connected <- struct{}{}
		<-r.Context().Done()
	}))
	s, err := NewSession(SessionConfig{
		BaseURL: srv.URL,
		Token:   "tok",
		TLS:     trusting(srv),
		Backoff: func(int) time.Duration { return 5 * time.Millisecond },
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = s.Run(ctx) }()
	select {
	case <-connected:
	case <-time.After(3 * time.Second):
		t.Fatal("session did not connect over wss")
	}
}

func TestSessionDoesNotConnectToUntrustedCertificate(t *testing.T) {
	connected := make(chan struct{}, 1)
	srv := tlsServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connected <- struct{}{}
		http.Error(w, "no", http.StatusForbidden)
	}))
	s, err := NewSession(SessionConfig{
		BaseURL: srv.URL,
		Token:   "tok",
		Backoff: func(int) time.Duration { return 5 * time.Millisecond },
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_ = s.Run(ctx)
	select {
	case <-connected:
		t.Fatal("the session reached the server despite the untrusted certificate")
	default:
	}
}
