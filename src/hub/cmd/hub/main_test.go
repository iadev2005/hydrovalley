package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProbeURL(t *testing.T) {
	cases := map[string]string{
		":8081":          "http://127.0.0.1:8081/healthz",
		"0.0.0.0:8081":   "http://127.0.0.1:8081/healthz",
		"[::]:9000":      "http://127.0.0.1:9000/healthz",
		"10.0.0.5:8081":  "http://10.0.0.5:8081/healthz",
		"127.0.0.1:8082": "http://127.0.0.1:8082/healthz",
	}
	for listen, want := range cases {
		got, err := probeURL(listen)
		if err != nil || got != want {
			t.Errorf("probeURL(%q) = %q, %v; want %q", listen, got, err, want)
		}
	}
	if _, err := probeURL("nonsense"); err == nil {
		t.Error("probeURL must reject an address without port")
	}
}

func TestHealthcheck(t *testing.T) {
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer ok.Close()
	if code := healthcheck(strings.TrimPrefix(ok.URL, "http://")); code != 0 {
		t.Fatalf("healthcheck against a healthy server = %d, want 0", code)
	}
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer bad.Close()
	if code := healthcheck(strings.TrimPrefix(bad.URL, "http://")); code != 1 {
		t.Fatalf("healthcheck against an unhealthy server = %d, want 1", code)
	}
}
