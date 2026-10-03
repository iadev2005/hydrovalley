package cheater

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/iadev2005/hydrovalley/src/bots/internal/client"
)

// samples genera n muestras separadas por step, todas con el mismo estado.
func samples(n int, step time.Duration, status string) []FloodSample {
	out := make([]FloodSample, n)
	for i := range out {
		out[i] = FloodSample{Offset: time.Duration(i) * step, Status: status, Resolved: true}
	}
	return out
}

func TestBuildFloodPassesWhenTheHubAdmitsAtMostTheLimitPerSecond(t *testing.T) {
	// 50 por segundo durante 2 s; por segundo, 15 admitidas y 35 limitadas.
	var s []FloodSample
	for i := range 100 {
		st := "rate_limited"
		if i%50 < 15 {
			st = "applied"
		}
		s = append(s, FloodSample{Offset: time.Duration(i) * 20 * time.Millisecond, Status: st, Resolved: true})
	}
	f := BuildFlood(FloodMeasurement{Window: 2 * time.Second, Samples: s, Limit: 15, Tolerance: 1})
	if !f.OK || f.Admitted != 30 || f.RateLimited != 70 || f.MaxPerBucket != 15 {
		t.Fatalf("unexpected %+v", f)
	}
	if f.AcceptedPerSecond != 15 {
		t.Fatalf("accepted_per_second = %v, want 15", f.AcceptedPerSecond)
	}
}

func TestBuildFloodCountsRejectedAsAdmittedByTheHub(t *testing.T) {
	s := samples(10, 10*time.Millisecond, "rejected")
	s[0].Reason = "insufficient_water"
	f := BuildFlood(FloodMeasurement{Window: time.Second, Samples: s, Limit: 15, Tolerance: 1})
	if f.Admitted != 10 || f.RateLimited != 0 || f.Results["rejected:insufficient_water"] != 1 {
		t.Fatalf("unexpected %+v", f)
	}
}

func TestBuildFloodFailsWithoutRateLimiting(t *testing.T) {
	f := BuildFlood(FloodMeasurement{Window: time.Second, Samples: samples(50, 20*time.Millisecond, "applied"), Limit: 15, Tolerance: 1})
	if f.OK || f.MaxPerBucket != 50 {
		t.Fatalf("a hub that admits 50/s must fail: %+v", f)
	}
}

func TestBuildFloodToleratesOneExtraAtTheWindowEdge(t *testing.T) {
	s := samples(16, 10*time.Millisecond, "applied")
	s = append(s, samples(30, 10*time.Millisecond, "rate_limited")...)
	for i := 16; i < len(s); i++ {
		s[i].Offset = time.Duration(i) * 10 * time.Millisecond
	}
	if f := BuildFlood(FloodMeasurement{Window: time.Second, Samples: s, Limit: 15, Tolerance: 1}); !f.OK {
		t.Fatalf("16 admitted in one bucket is within limit+1: %+v", f)
	}
	if f := BuildFlood(FloodMeasurement{Window: time.Second, Samples: s, Limit: 15, Tolerance: 0}); f.OK {
		t.Fatalf("16 admitted must fail without tolerance: %+v", f)
	}
}

func TestBuildFloodFailsWhenNothingIsRateLimitedOrActionsAreUnresolved(t *testing.T) {
	few := BuildFlood(FloodMeasurement{Window: time.Second, Samples: samples(5, 100*time.Millisecond, "applied"), Limit: 15, Tolerance: 1})
	if few.OK {
		t.Fatalf("no rate_limited at all must fail: %+v", few)
	}
	s := samples(40, 10*time.Millisecond, "rate_limited")
	s[3].Resolved = false
	un := BuildFlood(FloodMeasurement{Window: time.Second, Samples: s, Limit: 15, Tolerance: 1})
	if un.OK || un.Unresolved != 1 {
		t.Fatalf("unresolved actions must fail: %+v", un)
	}
}

func TestBuildFloodHandlesAnEmptyWindow(t *testing.T) {
	f := BuildFlood(FloodMeasurement{Limit: 15})
	if f.OK || f.AcceptedPerSecond != 0 {
		t.Fatalf("unexpected %+v", f)
	}
}

func TestBuildDuplicates(t *testing.T) {
	dup := client.Frame{Type: "result", Status: "duplicate"}
	cases := []struct {
		name   string
		first  []string
		frames []client.Frame
		want   bool
	}{
		{"applied once, two duplicates", []string{"applied"}, []client.Frame{dup, dup}, true},
		{"late immediate resend also duplicate", []string{"applied"}, []client.Frame{dup, dup, dup}, true},
		{"second applied", []string{"applied"}, []client.Frame{dup, {Type: "result", Status: "applied"}}, false},
		{"applied twice as outcomes", []string{"applied", "applied"}, []client.Frame{dup, dup}, false},
		{"original not applied", []string{"rejected"}, []client.Frame{dup, dup}, false},
		{"no duplicate answer after the final result", []string{"applied"}, nil, false},
	}
	for _, c := range cases {
		d := BuildDuplicates(c.first, c.frames, 2, 1)
		if d.OK != c.want {
			t.Errorf("%s: OK = %v, want %v (%+v)", c.name, d.OK, c.want, d)
		}
	}
}

func TestBuildInvalid(t *testing.T) {
	bad := client.Frame{Type: "error", Reason: "invalid_message"}
	unk := client.Frame{Type: "error", Reason: "unknown_type"}
	if v := BuildInvalid([]client.Frame{bad, unk}, true); !v.OK || !v.MalformedOK || !v.UnknownTypeOK {
		t.Fatalf("unexpected %+v", v)
	}
	if v := BuildInvalid([]client.Frame{unk}, true); v.OK || v.MalformedOK {
		t.Fatalf("missing invalid_message must fail: %+v", v)
	}
	if v := BuildInvalid([]client.Frame{bad, unk}, false); v.OK {
		t.Fatalf("a dead connection must fail: %+v", v)
	}
}

func TestReportPassedNeedsAllThreePhases(t *testing.T) {
	r := Report{Flood: Flood{OK: true}, Duplicates: Duplicates{OK: true}, Invalid: Invalid{OK: true}}
	if !r.Passed() {
		t.Fatal("all OK must pass")
	}
	r.Invalid.OK = false
	if r.Passed() {
		t.Fatal("one failing phase must fail")
	}
}

func TestFormatSummaryAndJSON(t *testing.T) {
	r := Report{
		Flood:      Flood{DurationS: 10, Sent: 500, Admitted: 150, RateLimited: 350, AcceptedPerSecond: 15, MaxPerBucket: 15, Limit: 15, Tolerance: 1, OK: true, Results: map[string]int{"applied": 20}},
		Duplicates: Duplicates{Applied: 1, DuplicateFrames: 2, OK: true},
		Invalid:    Invalid{MalformedOK: true, UnknownTypeOK: true, ConnectionOK: true, OK: true},
	}
	out := FormatSummary(r)
	for _, want := range []string{"CP-03", "15,0", "350", "RF-09", "RF-06", "Resultado: CUMPLE"} {
		if !strings.Contains(out, want) {
			t.Errorf("summary lacks %q:\n%s", want, out)
		}
	}
	r.Flood.OK = false
	if !strings.Contains(FormatSummary(r), "Resultado: NO CUMPLE") {
		t.Error("a failing phase must say NO CUMPLE")
	}
	data, err := MarshalReport(r)
	if err != nil {
		t.Fatal(err)
	}
	var back map[string]any
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	flood := back["flood"].(map[string]any)
	if flood["accepted_per_second"] != float64(15) || flood["rate_limited"] != float64(350) {
		t.Fatalf("unexpected JSON %v", flood)
	}
}
