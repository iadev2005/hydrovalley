package load

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func ms(n int) time.Duration { return time.Duration(n) * time.Millisecond }

func TestPercentileUsesNearestRank(t *testing.T) {
	var sorted []time.Duration
	for i := 1; i <= 100; i++ {
		sorted = append(sorted, ms(i))
	}
	cases := []struct {
		p    float64
		want time.Duration
	}{{0.50, ms(50)}, {0.95, ms(95)}, {1, ms(100)}, {0, ms(1)}}
	for _, c := range cases {
		if got := Percentile(sorted, c.p); got != c.want {
			t.Errorf("Percentile(%v) = %v, want %v", c.p, got, c.want)
		}
	}
	if got := Percentile(nil, 0.95); got != 0 {
		t.Errorf("empty sample = %v, want 0", got)
	}
	if got := Percentile([]time.Duration{ms(7)}, 0.95); got != ms(7) {
		t.Errorf("single sample = %v, want 7ms", got)
	}
}

// measurement arma una medición sana: 30 bots, 60 s, 6.500 gotas aplicadas.
func measurement() Measurement {
	lat := make([]time.Duration, 6500)
	for i := range lat {
		lat[i] = ms(10 + i%40)
	}
	return Measurement{
		Bots: 30, Window: 60 * time.Second, Sent: 6500,
		Results: map[string]int{"applied": 6500}, Latencies: lat,
		Baseline: Counters{Humidity: 100, Accepted: 100},
		Final:    Counters{Humidity: 6600, Accepted: 6600},
	}
}

func TestBuildReportComputesTheMeasuredValues(t *testing.T) {
	r := BuildReport(measurement())
	if r.Bots != 30 || r.DurationS != 60 || r.Sent != 6500 || r.Results["applied"] != 6500 || r.Unresolved != 0 {
		t.Fatalf("header = %+v", r)
	}
	if want := 6500.0 / 60.0; r.DropsPerSecond != want {
		t.Fatalf("drops_per_second = %v, want %v", r.DropsPerSecond, want)
	}
	if r.HumidityContributed != 6500 || r.AcceptedDrops != 6500 || r.BotsApplied != 6500 {
		t.Fatalf("deltas = %d/%d/%d", r.HumidityContributed, r.AcceptedDrops, r.BotsApplied)
	}
	if r.LatencyMS.P50 <= 0 || r.LatencyMS.P95 < r.LatencyMS.P50 || r.LatencyMS.Max < r.LatencyMS.P95 {
		t.Fatalf("latency = %+v", r.LatencyMS)
	}
	if !r.InvariantOK || !r.RNF01OK || !r.RNF02OK || !r.Passed() {
		t.Fatalf("a healthy run must pass: %+v", r)
	}
}

func TestBuildReportNeverInventsNumbersForAnEmptyRun(t *testing.T) {
	r := BuildReport(Measurement{Bots: 30, Window: 60 * time.Second, Results: map[string]int{}})
	if r.Sent != 0 || r.DropsPerSecond != 0 || r.LatencyMS.P95 != 0 {
		t.Fatalf("empty run = %+v", r)
	}
	if r.RNF01OK || r.RNF02OK || r.Passed() {
		t.Fatalf("a run that sent nothing proves nothing and must fail: %+v", r)
	}
}

func TestRNF01NeedsAtLeastTheThresholdRate(t *testing.T) {
	m := measurement()
	m.Sent, m.Results = 6000, map[string]int{"applied": 6000}
	m.Final = Counters{Humidity: 6100, Accepted: 6100}
	if r := BuildReport(m); !r.RNF01OK { // exactamente 100/s
		t.Fatalf("exactly %v/s must pass RNF-01: %+v", RNF01MinDropsPerSecond, r)
	}
	m.Sent, m.Results = 5999, map[string]int{"applied": 5999}
	m.Final = Counters{Humidity: 6099, Accepted: 6099}
	if r := BuildReport(m); r.RNF01OK {
		t.Fatal("just under the threshold must fail RNF-01")
	}
}

func TestRNF01CountsOnlyAcceptedDrops(t *testing.T) {
	m := measurement()
	m.Results = map[string]int{"applied": 3000, "duplicate": 500, "rate_limited": 3000}
	m.Final = Counters{Humidity: 3600, Accepted: 3600}
	r := BuildReport(m)
	if r.BotsApplied != 3500 || r.DropsPerSecond != 3500.0/60.0 || r.RNF01OK {
		t.Fatalf("applied+duplicate are accepted, the rest is not: %+v", r)
	}
}

func TestRNF02RequiresNinetyFivePercentUnderTheLimit(t *testing.T) {
	m := measurement()
	lat := make([]time.Duration, 100)
	m.Sent, m.Results = 100, map[string]int{"applied": 100}
	m.Window = time.Second
	m.Baseline, m.Final = Counters{}, Counters{Humidity: 100, Accepted: 100}
	for i := range lat {
		lat[i] = ms(50)
	}
	for i := range 5 {
		lat[i] = ms(500) // 5 % lentas: el p95 es 50 ms
	}
	m.Latencies = lat
	if r := BuildReport(m); !r.RNF02OK || r.LatencyMS.P95 != 50 {
		t.Fatalf("5%% slow still meets RNF-02: %+v", r)
	}
	lat[5] = ms(500) // 6 % lentas
	if r := BuildReport(m); r.RNF02OK {
		t.Fatalf("6%% slow must fail RNF-02: %+v", r)
	}
}

func TestRNF02CountsUnansweredActionsAsSlow(t *testing.T) {
	m := measurement()
	m.Sent, m.Unresolved = 100, 10
	m.Results = map[string]int{"applied": 90}
	m.Window = time.Second
	m.Latencies = make([]time.Duration, 90)
	m.Baseline, m.Final = Counters{}, Counters{Humidity: 90, Accepted: 90}
	for i := range m.Latencies {
		m.Latencies[i] = ms(20)
	}
	r := BuildReport(m)
	if r.Unresolved != 10 || r.RNF02OK {
		t.Fatalf("10%% without a final result cannot meet 95%% under 200 ms: %+v", r)
	}
}

func TestInvariantComparesDeltasNotTotals(t *testing.T) {
	m := measurement()
	m.Baseline = Counters{Humidity: 5000, Accepted: 4000} // el valle ya traía diferencia
	m.Final = Counters{Humidity: 11500, Accepted: 10500}
	if r := BuildReport(m); !r.InvariantOK {
		t.Fatalf("equal deltas must pass: %+v", r)
	}
	m.Final = Counters{Humidity: 11501, Accepted: 10500}
	if r := BuildReport(m); r.InvariantOK || r.Passed() {
		t.Fatalf("one extra unit of humidity must fail: %+v", r)
	}
}

func TestInvariantFailsWhenTheHubReportsNoCounters(t *testing.T) {
	m := measurement()
	m.Baseline, m.Final = Counters{}, Counters{} // un hub sin los campos: todo cero
	r := BuildReport(m)
	if r.InvariantOK || len(r.Notes) == 0 {
		t.Fatalf("applied drops with zero counters is an unverifiable run: %+v", r)
	}
}

func TestInvariantFailsWhenCountersGoBackwards(t *testing.T) {
	m := measurement()
	m.Final = Counters{Humidity: 10, Accepted: 10} // alguien reinició el valle
	r := BuildReport(m)
	if r.InvariantOK || r.HumidityContributed != 0 || len(r.Notes) == 0 {
		t.Fatalf("counters that fell must fail with a note: %+v", r)
	}
}

func TestBotsAppliedDifferingFromHubDeltaIsOnlyInformational(t *testing.T) {
	m := measurement()
	m.Final = Counters{Humidity: 6700, Accepted: 6700} // otros jugadores sumaron 100
	r := BuildReport(m)
	if !r.InvariantOK || !r.Passed() {
		t.Fatalf("other players must not fail the run: %+v", r)
	}
	if r.BotsApplied != 6500 || r.AcceptedDrops != 6600 {
		t.Fatalf("both numbers must be reported: %+v", r)
	}
}

func TestFormatSummaryIsInSpanishWithThousandsSeparators(t *testing.T) {
	out := FormatSummary(BuildReport(measurement()))
	for _, want := range []string{
		"30 bots · 60 s · modo demo",
		"Gotas enviadas: 6.500 · aceptadas: 6.500 · rechazadas: 0",
		"Ritmo: 108,3 gotas por segundo",
		"Latencia: mediana",
		"Humedad aportada: 6.500 · gotas aceptadas del hub: 6.500 -> coincide",
		"RNF-01", "RNF-02",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("summary lacks %q:\n%s", want, out)
		}
	}
	m := measurement()
	m.Final.Humidity++
	if out := FormatSummary(BuildReport(m)); !strings.Contains(out, "NO coincide") {
		t.Errorf("a broken invariant must say so:\n%s", out)
	}
}

func TestFormatSummaryShowsRejectionsAndUnresolved(t *testing.T) {
	m := measurement()
	m.Sent, m.Unresolved = 120, 5
	m.Results = map[string]int{"applied": 100, "rate_limited": 10, "rejected:out_of_bounds": 5}
	out := FormatSummary(BuildReport(m))
	if !strings.Contains(out, "aceptadas: 100 · rechazadas: 15") || !strings.Contains(out, "sin resultado: 5") {
		t.Errorf("summary = \n%s", out)
	}
}

func TestReportJSONFieldNames(t *testing.T) {
	data, err := MarshalReport(BuildReport(measurement()))
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"bots", "duration_s", "sent", "results", "unresolved", "drops_per_second", "latency_ms",
		"humidity_contributed", "accepted_drops", "invariant_ok", "rnf01_ok", "rnf02_ok"} {
		if _, ok := got[k]; !ok {
			t.Errorf("report lacks %q: %s", k, data)
		}
	}
	lat, _ := got["latency_ms"].(map[string]any)
	for _, k := range []string{"p50", "p95", "max"} {
		if _, ok := lat[k]; !ok {
			t.Errorf("latency_ms lacks %q", k)
		}
	}
}
