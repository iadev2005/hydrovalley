package uuid

import (
	"regexp"
	"testing"
)

var v4 = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestNewIsAVersion4UUIDInHyphenatedForm(t *testing.T) {
	for range 100 {
		if id := New(); !v4.MatchString(id) {
			t.Fatalf("%q no es un UUID v4", id)
		}
	}
}

func TestNewNeverRepeats(t *testing.T) {
	seen := make(map[string]struct{})
	for range 1000 {
		id := New()
		if _, repeated := seen[id]; repeated {
			t.Fatalf("UUID repetido: %s", id)
		}
		seen[id] = struct{}{}
	}
}
