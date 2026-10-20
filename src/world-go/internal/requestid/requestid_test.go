package requestid

import (
	"context"
	"strings"
	"testing"

	"google.golang.org/grpc/metadata"
)

func incoming(values ...string) context.Context {
	md := metadata.MD{}
	for _, value := range values {
		md.Append(Key, value)
	}
	return metadata.NewIncomingContext(context.Background(), md)
}

func TestSafeIDsAreKept(t *testing.T) {
	for _, id := range []string{
		"3f2b8c1e-5a47-4d0e-9b6a-1c2d3e4f5a6b",
		"0123456789abcdef0123456789ABCDEF",
		"tick-42",
		"world-3f2b8c1e-5a47-4d0e-9b6a-1c2d3e4f5a6b",
		strings.Repeat("a", 64),
	} {
		if !IsSafe(id) {
			t.Errorf("%q debería ser seguro", id)
		}
		if got := FromContext(incoming(id)); got != id {
			t.Errorf("FromContext(%q) = %q", id, got)
		}
	}
}

func TestMissingOrUnusableIDsBecomeADash(t *testing.T) {
	if got := FromContext(context.Background()); got != None {
		t.Errorf("sin metadatos: %q", got)
	}
	if got := FromContext(incoming()); got != None {
		t.Errorf("sin el metadato: %q", got)
	}
	for _, id := range []string{"", "con espacio", "a/b", "<script>", "a\nb", strings.Repeat("a", 65)} {
		if IsSafe(id) {
			t.Errorf("%q no debería ser seguro", id)
		}
		if got := FromContext(incoming(id)); got != None {
			t.Errorf("FromContext(%q) = %q, quiero %q", id, got, None)
		}
	}
}

func TestOutgoingIDsAreSafeAndDistinct(t *testing.T) {
	a, b := NewOutgoing(), NewOutgoing()
	if !strings.HasPrefix(a, "world-") || !IsSafe(a) {
		t.Errorf("id saliente no válido: %q", a)
	}
	if a == b {
		t.Errorf("los ids deben ser distintos: %q", a)
	}
}

func TestTagPutsTheIDInTheOutgoingMetadata(t *testing.T) {
	ctx := Tag(context.Background(), "tick-7")
	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok || len(md.Get(Key)) != 1 || md.Get(Key)[0] != "tick-7" {
		t.Fatalf("metadatos salientes: %v", md)
	}
}
