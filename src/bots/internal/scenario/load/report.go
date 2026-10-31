package load

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// Umbrales de la prueba de carga (CP-11, documento inicial).
const (
	// RNF01MinDropsPerSecond es el mínimo de gotas aceptadas por segundo
	// (RNF-01).
	RNF01MinDropsPerSecond = 100.0
	// RNF02MaxLatency y RNF02Percent: el 95 % de las acciones debe tener su
	// resultado final en menos de 200 ms (RNF-02), medido desde el primer
	// envío.
	RNF02MaxLatency = 200 * time.Millisecond
	RNF02Percent    = 95
	// RNF03Margin es cuánto puede subir el p95 de las acciones con el ranking
	// en bucle respecto a la misma carga sin ranking (la línea base). El
	// documento inicial solo pide que el ranking no haga pasar a las acciones
	// de los 200 ms de RNF-02, sin cifrar «cuánto» empeorar es aceptable: el
	// 20 % es un umbral elegido por el equipo (decisión pendiente de
	// confirmar), pensado para absorber el ruido entre dos ejecuciones sin
	// dejar pasar un deterioro claro. RNF-03 exige además p95 <= RNF02MaxLatency.
	RNF03Margin = 0.20
	// RNF03MinRankingQueries es el mínimo de consultas de ranking exitosas
	// para que RNF-03 se pueda evaluar. Con 2 s de intervalo y 60 s de carga
	// salen unas 30; con menos de 10 el ranking apenas ejerció presión, y con
	// cualquier error (salvo la consulta que la propia prueba cancela al
	// cerrar la ventana, que no se cuenta) lo medido no es el ranking en
	// funcionamiento. Un valor elegido por el equipo, como RNF03Margin.
	RNF03MinRankingQueries = 10
)

// Counters son los contadores del modo demo que el hub difunde en `state`.
type Counters struct {
	Humidity uint64
	Accepted uint64
}

// RankingMeasurement reúne lo medido de las consultas periódicas de ranking.
type RankingMeasurement struct {
	Sent      int
	Success   int
	Errors    int
	Latencies []time.Duration
}

// Measurement es lo medido, sin interpretar: la entrada de BuildReport.
type Measurement struct {
	Bots int
	// Window es el tiempo en que los bots estuvieron enviando.
	Window time.Duration
	Sent   int
	// Results cuenta por estado ("applied", "duplicate", "rejected:<motivo>",
	// "rate_limited", ...). Unresolved son las enviadas sin resultado final.
	Results    map[string]int
	Unresolved int
	SendErrors int
	// Latencies son las de las acciones con resultado del hub.
	Latencies       []time.Duration
	Baseline, Final Counters
	// Ranking contiene las consultas de GET /api/ranking si se ejecutó con ranking.
	Ranking     *RankingMeasurement
	BaselineP95 time.Duration
}

// LatencyMS resume la latencia, en milisegundos.
type LatencyMS struct {
	P50 float64 `json:"p50"`
	P95 float64 `json:"p95"`
	Max float64 `json:"max"`
}

// RankingStats resume las métricas de las consultas de ranking en el informe.
type RankingStats struct {
	QueriesSent    int       `json:"queries_sent"`
	QueriesOK      int       `json:"queries_ok"`
	Errors         int       `json:"errors"`
	LatencyMS      LatencyMS `json:"latency_ms"`
	RNF03OK        bool      `json:"rnf03_ok"`
	RNF03Evaluated bool      `json:"rnf03_evaluated"`
	BaselineP95MS  float64   `json:"baseline_p95_ms"`
}

// Report es el informe de la prueba, tal como se escribe en JSON.
type Report struct {
	Bots       int            `json:"bots"`
	DurationS  float64        `json:"duration_s"`
	Sent       int            `json:"sent"`
	Results    map[string]int `json:"results"`
	Unresolved int            `json:"unresolved"`
	SendErrors int            `json:"send_errors"`
	// BotsApplied son las gotas que los bots vieron aplicadas (`applied` y
	// `duplicate`); coincide con AcceptedDrops solo si nadie más juega.
	BotsApplied    int       `json:"bots_applied"`
	DropsPerSecond float64   `json:"drops_per_second"`
	LatencyMS      LatencyMS `json:"latency_ms"`
	// LatencyWithinLimit es la fracción de las acciones enviadas con
	// resultado en menos de RNF02MaxLatency; las sin resultado cuentan como
	// lentas.
	LatencyWithinLimit float64 `json:"latency_within_limit_ratio"`
	// HumidityContributed y AcceptedDrops son las diferencias entre el final
	// y el inicio de la prueba.
	HumidityContributed uint64        `json:"humidity_contributed"`
	AcceptedDrops       uint64        `json:"accepted_drops"`
	InvariantOK         bool          `json:"invariant_ok"`
	RNF01OK             bool          `json:"rnf01_ok"`
	RNF02OK             bool          `json:"rnf02_ok"`
	Ranking             *RankingStats `json:"ranking,omitempty"`
	Notes               []string      `json:"notes,omitempty"`
}

// Passed dice si se cumplen las tres comprobaciones de CP-11 (y RNF-03 si hubo ranking).
func (r Report) Passed() bool {
	base := r.InvariantOK && r.RNF01OK && r.RNF02OK
	if r.Ranking != nil {
		return base && r.Ranking.RNF03OK
	}
	return base
}

// Percentile devuelve el percentil p (0 a 1) por el método del rango más
// cercano; sorted debe venir ordenado. Una muestra vacía da 0.
func Percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	rank := int(math.Ceil(p * float64(len(sorted))))
	return sorted[min(max(rank, 1), len(sorted))-1]
}

func toMS(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }

// delta es after - before; ok es falso si el contador retrocedió.
func delta(before, after uint64) (uint64, bool) {
	if after < before {
		return 0, false
	}
	return after - before, true
}

// BuildReport interpreta una medición contra los umbrales del documento.
func BuildReport(m Measurement) Report {
	r := Report{
		Bots: m.Bots, DurationS: m.Window.Seconds(), Sent: m.Sent, Results: m.Results,
		Unresolved: m.Unresolved, SendErrors: m.SendErrors,
		BotsApplied: m.Results["applied"] + m.Results["duplicate"],
	}
	if r.Results == nil {
		r.Results = map[string]int{}
	}

	if m.Window > 0 {
		r.DropsPerSecond = float64(r.BotsApplied) / m.Window.Seconds()
	}
	r.RNF01OK = r.DropsPerSecond >= RNF01MinDropsPerSecond

	lat := append([]time.Duration(nil), m.Latencies...)
	sort.Slice(lat, func(i, j int) bool { return lat[i] < lat[j] })
	r.LatencyMS = LatencyMS{P50: toMS(Percentile(lat, 0.50)), P95: toMS(Percentile(lat, 0.95))}
	if len(lat) > 0 {
		r.LatencyMS.Max = toMS(lat[len(lat)-1])
	}
	within := 0
	for _, d := range lat {
		if d < RNF02MaxLatency {
			within++
		}
	}
	if m.Sent > 0 {
		r.LatencyWithinLimit = float64(within) / float64(m.Sent)
		r.RNF02OK = within*100 >= m.Sent*RNF02Percent
	}

	hum, okH := delta(m.Baseline.Humidity, m.Final.Humidity)
	acc, okA := delta(m.Baseline.Accepted, m.Final.Accepted)
	r.HumidityContributed, r.AcceptedDrops = hum, acc
	switch {
	case !okH || !okA:
		r.Notes = append(r.Notes, "Un contador retrocedió durante la prueba (¿alguien reinició el valle?); no se puede comprobar el invariante.")
	case r.BotsApplied > 0 && acc == 0:
		// Un campo en cero se omite en el JSON: un hub sin los contadores
		// se ve igual que uno que no aceptó nada.
		r.Notes = append(r.Notes, "Los bots tuvieron gotas aplicadas pero el hub informa 0 gotas aceptadas; ¿el hub informa los contadores en `state`?")
	default:
		r.InvariantOK = hum == acc
	}
	if m.SendErrors > 0 {
		r.Notes = append(r.Notes, fmt.Sprintf("%d turnos sin conexión para enviar.", m.SendErrors))
	}
	if m.Ranking != nil {
		rlat := append([]time.Duration(nil), m.Ranking.Latencies...)
		sort.Slice(rlat, func(i, j int) bool { return rlat[i] < rlat[j] })
		rlatMS := LatencyMS{P50: toMS(Percentile(rlat, 0.50)), P95: toMS(Percentile(rlat, 0.95))}
		if len(rlat) > 0 {
			rlatMS.Max = toMS(rlat[len(rlat)-1])
		}
		r.Ranking = &RankingStats{
			QueriesSent: m.Ranking.Sent,
			QueriesOK:   m.Ranking.Success,
			Errors:      m.Ranking.Errors,
			LatencyMS:   rlatMS,
		}
		switch {
		case m.Ranking.Success < RNF03MinRankingQueries || m.Ranking.Errors > 0:
			// Sin consultas suficientes y sin errores, lo medido no es el
			// ranking en funcionamiento: no se evalúa ni se da por cumplido.
			r.Notes = append(r.Notes, fmt.Sprintf(
				"RNF-03 no se pudo evaluar: se exigen al menos %d consultas de ranking exitosas y ninguna con error (hubo %d exitosas y %d con error).",
				RNF03MinRankingQueries, m.Ranking.Success, m.Ranking.Errors))
		case m.BaselineP95 > 0:
			// RNF-03: con el ranking en bucle, el p95 de las acciones no pasa
			// de los 200 ms ni se aleja de la línea base más que el margen.
			p95 := Percentile(lat, 0.95)
			r.Ranking.BaselineP95MS = toMS(m.BaselineP95)
			r.Ranking.RNF03Evaluated = true
			r.Ranking.RNF03OK = p95 <= RNF02MaxLatency && float64(p95) <= float64(m.BaselineP95)*(1+RNF03Margin)
		default:
			r.Notes = append(r.Notes, "RNF-03 no se pudo evaluar: falta la línea base (p95 de la misma carga sin ranking).")
		}
	}
	return r
}

// BaselineP95FromReport lee el p95 de las acciones de un informe de una
// ejecución sin ranking (la línea base de RNF-03). Rechaza un informe con
// ranking, porque no sería una línea base, y uno sin latencias.
func BaselineP95FromReport(data []byte) (time.Duration, error) {
	var r struct {
		LatencyMS LatencyMS     `json:"latency_ms"`
		Ranking   *RankingStats `json:"ranking"`
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return 0, fmt.Errorf("baseline report: %w", err)
	}
	if r.Ranking != nil {
		return 0, errors.New("baseline report: it was run with the ranking loop, so it is not a baseline")
	}
	if r.LatencyMS.P95 <= 0 {
		return 0, errors.New("baseline report: it has no p95 latency")
	}
	return time.Duration(r.LatencyMS.P95 * float64(time.Millisecond)), nil
}

// MarshalReport codifica el informe en JSON con sangría.
func MarshalReport(r Report) ([]byte, error) {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// thousands escribe n con punto como separador de miles (6.500).
func thousands(n int) string {
	s := fmt.Sprint(n)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(c)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

// decimal escribe f con una cifra y coma decimal.
func decimal(f float64) string { return strings.Replace(fmt.Sprintf("%.1f", f), ".", ",", 1) }

func verdict(ok bool) string {
	if ok {
		return "cumple"
	}
	return "NO cumple"
}

// FormatSummary es el resumen para personas, con el formato del documento
// inicial («Los bots»).
func FormatSummary(r Report) string {
	accepted := r.BotsApplied
	rejected := 0
	for k, n := range r.Results {
		if k != "applied" && k != "duplicate" {
			rejected += n
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d bots · %s s · modo demo\n", r.Bots, decimalTrim(r.DurationS))
	fmt.Fprintf(&b, "Gotas enviadas: %s · aceptadas: %s · rechazadas: %s", thousands(r.Sent), thousands(accepted), thousands(rejected))
	if r.Unresolved > 0 {
		fmt.Fprintf(&b, " · sin resultado: %s", thousands(r.Unresolved))
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "Ritmo: %s gotas por segundo\n", decimal(r.DropsPerSecond))
	fmt.Fprintf(&b, "Latencia: mediana %s ms · p95 %s ms · máxima %s ms\n", decimal(r.LatencyMS.P50), decimal(r.LatencyMS.P95), decimal(r.LatencyMS.Max))
	match := "coincide"
	if !r.InvariantOK {
		match = "NO coincide"
	}
	fmt.Fprintf(&b, "Humedad aportada: %s · gotas aceptadas del hub: %s -> %s\n",
		thousands(int(r.HumidityContributed)), thousands(int(r.AcceptedDrops)), match)
	if r.BotsApplied != int(r.AcceptedDrops) {
		fmt.Fprintf(&b, "(Los bots vieron %s gotas aplicadas; el resto, si hay, es de otros jugadores.)\n", thousands(r.BotsApplied))
	}
	fmt.Fprintf(&b, "RNF-01 (al menos %.0f gotas aceptadas por segundo): %s\n", RNF01MinDropsPerSecond, verdict(r.RNF01OK))
	fmt.Fprintf(&b, "RNF-02 (el %d %% de las acciones en menos de %d ms): %s (%s %%)\n",
		RNF02Percent, RNF02MaxLatency.Milliseconds(), verdict(r.RNF02OK), decimal(r.LatencyWithinLimit*100))
	if r.Ranking != nil {
		fmt.Fprintf(&b, "Ranking en bucle: %s consultas (%s exitosas, %s errores)\n",
			thousands(r.Ranking.QueriesSent), thousands(r.Ranking.QueriesOK), thousands(r.Ranking.Errors))
		fmt.Fprintf(&b, "Latencia del ranking: mediana %s ms · p95 %s ms · máxima %s ms\n",
			decimal(r.Ranking.LatencyMS.P50), decimal(r.Ranking.LatencyMS.P95), decimal(r.Ranking.LatencyMS.Max))
		if r.Ranking.RNF03Evaluated {
			fmt.Fprintf(&b, "RNF-03 (p95 de las acciones con ranking <= %.0f ms y <= línea base + %.0f %%): %s (p95 %s ms con ranking · %s ms línea base)\n",
				float64(RNF02MaxLatency.Milliseconds()), RNF03Margin*100, verdict(r.Ranking.RNF03OK),
				decimal(r.LatencyMS.P95), decimal(r.Ranking.BaselineP95MS))
		} else if r.Ranking.QueriesOK < RNF03MinRankingQueries || r.Ranking.Errors > 0 {
			fmt.Fprintf(&b, "RNF-03: NO se pudo evaluar, las consultas de ranking no bastan (se exigen al menos %d exitosas y ninguna con error)\n", RNF03MinRankingQueries)
		} else {
			b.WriteString("RNF-03: NO se pudo evaluar, sin línea base (ejecuta antes la misma carga sin ranking y pásala con BOTS_LOAD_BASELINE_REPORT)\n")
		}
	}
	for _, n := range r.Notes {
		fmt.Fprintf(&b, "Nota: %s\n", n)
	}
	if r.Passed() {
		b.WriteString("Resultado: CUMPLE\n")
	} else {
		b.WriteString("Resultado: NO CUMPLE\n")
	}
	return b.String()
}

// decimalTrim escribe 60 en vez de 60,0 y conserva los decimales si los hay.
func decimalTrim(f float64) string {
	if f == math.Trunc(f) {
		return fmt.Sprintf("%.0f", f)
	}
	return decimal(f)
}
