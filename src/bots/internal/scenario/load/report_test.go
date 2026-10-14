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

func TestBuildReportWithRanking(t *testing.T) {
	m := measurement()
	m.BaselineP95 = ms(50)
	m.Ranking = &RankingMeasurement{
		Sent:      60,
		Success:   58,
		Errors:    2,
		Latencies: []time.Duration{ms(15), ms(20), ms(25), ms(50)},
	}
	r := BuildReport(m)
	if r.Ranking == nil {
		t.Fatal("expected non-nil Ranking in Report")
	}
	if r.Ranking.QueriesSent != 60 || r.Ranking.QueriesOK != 58 || r.Ranking.Errors != 2 {
		t.Errorf("unexpected ranking counts: %+v", r.Ranking)
	}
	if r.Ranking.LatencyMS.P50 <= 0 || r.Ranking.LatencyMS.P95 <= 0 {
		t.Errorf("unexpected ranking latency: %+v", r.Ranking.LatencyMS)
	}
	if !r.Ranking.RNF03OK || !r.Passed() {
		t.Errorf("healthy run with ranking must satisfy RNF03 and pass: %+v", r)
	}

	// Test JSON serialization includes ranking
	data, err := MarshalReport(r)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"ranking"`) || !strings.Contains(string(data), `"rnf03_ok"`) {
		t.Errorf("report JSON lacks ranking: %s", data)
	}

	// Test FormatSummary with ranking
	out := FormatSummary(r)
	if !strings.Contains(out, "Ranking en bucle: 60 consultas (58 exitosas, 2 errores)") {
		t.Errorf("summary lacks ranking line:\n%s", out)
	}
	if !strings.Contains(out, "RNF-03") || !strings.Contains(out, "cumple") {
		t.Errorf("summary lacks RNF-03 verdict:\n%s", out)
	}
}

// withRanking arma una medición con ranking cuyas acciones tienen la latencia
// p95 indicada y la línea base indicada (0 = sin línea base).
func withRanking(p95, baseline time.Duration) Measurement {
	m := measurement()
	lat := make([]time.Duration, 100)
	for i := range lat {
		lat[i] = p95 / 2
	}
	for i := 94; i < 100; i++ { // por rango más cercano, el p95 de 100 muestras es la 95.ª (índice 94)
		lat[i] = p95
	}
	m.Latencies, m.Sent, m.Window = lat, 100, time.Second // 100 gotas/s: RNF-01 cumple
	m.Results = map[string]int{"applied": 100}
	m.Ranking = &RankingMeasurement{Sent: 30, Success: 30, Latencies: []time.Duration{ms(5)}}
	m.BaselineP95 = baseline
	return m
}

func TestRNF03ComparesTheP95WithTheBaselineAndTheLimit(t *testing.T) {
	cases := []struct {
		name          string
		p95, baseline time.Duration
		want          bool
	}{
		{"igual a la línea base", ms(100), ms(100), true},
		{"dentro del margen", ms(120), ms(100), true},
		{"justo en el margen", ms(60), ms(50), true},
		{"fuera del margen aunque sea rápido", ms(61), ms(50), false},
		{"dentro del margen pero sobre 200 ms", ms(210), ms(190), false},
		{"justo en 200 ms", ms(200), ms(190), true},
		{"mejor que la línea base", ms(80), ms(100), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := BuildReport(withRanking(c.p95, c.baseline))
			if r.Ranking == nil || !r.Ranking.RNF03Evaluated {
				t.Fatalf("RNF-03 debe poder evaluarse con línea base: %+v", r.Ranking)
			}
			// Con p95 justo en 200 ms RNF-03 (<=) cumple, pero RNF-02 (< 200 ms para
			// el 95 %) no: por eso solo se exige Passed cuando hay holgura.
			if (!c.want && r.Passed()) || (c.want && c.p95 < RNF02MaxLatency && !r.Passed()) {
				t.Errorf("Passed = %v con RNF-03 = %v: %+v", r.Passed(), c.want, r)
			}
			if r.Ranking.RNF03OK != c.want {
				t.Errorf("RNF03OK = %v, want %v (p95 %v, base %v, report p95 %v)", r.Ranking.RNF03OK, c.want, c.p95, c.baseline, r.LatencyMS.P95)
			}
			if r.Ranking.BaselineP95MS != toMS(c.baseline) {
				t.Errorf("BaselineP95MS = %v, want %v", r.Ranking.BaselineP95MS, toMS(c.baseline))
			}
		})
	}
}

func TestRNF03IsNotEvaluableWithoutABaseline(t *testing.T) {
	r := BuildReport(withRanking(ms(20), 0))
	if r.Ranking.RNF03Evaluated || r.Ranking.RNF03OK {
		t.Errorf("sin línea base RNF-03 ni se evalúa ni se da por cumplido: %+v", r.Ranking)
	}
	if r.Passed() {
		t.Error("una ejecución con ranking sin línea base no puede darse por cumplida")
	}
	if !r.RNF01OK || !r.RNF02OK || !r.InvariantOK {
		t.Fatalf("la medición debe cumplir todo lo demás para que esta prueba tenga sentido: %+v", r)
	}
	out := FormatSummary(r)
	if !strings.Contains(out, "RNF-03") || !strings.Contains(out, "sin línea base") {
		t.Errorf("el resumen debe explicar que falta la línea base:\n%s", out)
	}
}

func TestSummaryShowsTheRNF03Comparison(t *testing.T) {
	out := FormatSummary(BuildReport(withRanking(ms(110), ms(100))))
	for _, want := range []string{"RNF-03", "línea base", "100,0 ms", "110,0 ms", "20 %"} {
		if !strings.Contains(out, want) {
			t.Errorf("el resumen no contiene %q:\n%s", want, out)
		}
	}
}

func TestBaselineP95FromReport(t *testing.T) {
	base := BuildReport(withRanking(ms(100), 0))
	base.Ranking = nil
	data, err := MarshalReport(base)
	if err != nil {
		t.Fatal(err)
	}
	got, err := BaselineP95FromReport(data)
	if err != nil || got != ms(100) {
		t.Fatalf("BaselineP95FromReport = %v, %v; want 100ms", got, err)
	}

	for name, in := range map[string]string{
		"no es JSON":           "nada",
		"sin latencia":         `{"latency_ms":{"p95":0}}`,
		"con ranking en bucle": `{"latency_ms":{"p95":50},"ranking":{"queries_sent":3}}`,
	} {
		if _, err := BaselineP95FromReport([]byte(in)); err == nil {
			t.Errorf("%s: se esperaba un error", name)
		}
	}
}
