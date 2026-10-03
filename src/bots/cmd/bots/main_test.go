package main

import "testing"

func TestCanonicalCommand(t *testing.T) {
	cases := map[string]string{
		"load":        "load",
		"load-test":   "load",
		"neighbors":   "neighbors",
		"cheater":     "cheater",
		"firefighter": "firefighter",
		"unknown":     "unknown",
	}
	for in, want := range cases {
		if got := canonicalCommand(in); got != want {
			t.Errorf("canonicalCommand(%q) = %q, want %q", in, got, want)
		}
	}
}
