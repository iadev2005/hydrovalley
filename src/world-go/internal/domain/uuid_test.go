package domain

import "testing"

// isUUID imita Uuid::try_parse del crate uuid (world.rs:810): acepta las
// formas simple, con guiones, con llaves y urn.
func TestIsUUID(t *testing.T) {
	const hyphenated = "6f1c1a52-2d0e-4f4e-9c1e-0c6c2d9b8a01"
	cases := []struct {
		in   string
		want bool
	}{
		{hyphenated, true},
		{"6F1C1A52-2D0E-4F4E-9C1E-0C6C2D9B8A01", true},
		{"6f1c1a522d0e4f4e9c1e0c6c2d9b8a01", true},
		{"{" + hyphenated + "}", true},
		{"urn:uuid:" + hyphenated, true},
		{"", false},
		{"not-a-uuid", false},
		{"6f1c1a52-2d0e-4f4e-9c1e-0c6c2d9b8a0", false},
		{"6f1c1a52-2d0e-4f4e-9c1e-0c6c2d9b8a0g", false},
		{"6f1c1a522-d0e-4f4e-9c1e-0c6c2d9b8a01", false},
		{"{" + hyphenated, false},
		{"urn:uuid:" + hyphenated[:35], false},
		{"6f1c1a52-2d0e-4f4e-9c1e-0c6c2d9b8a0é", false},
	}
	for _, c := range cases {
		if got := isUUID(c.in); got != c.want {
			t.Errorf("isUUID(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
