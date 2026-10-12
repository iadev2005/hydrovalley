package firefighter

import (
	"encoding/json"
	"fmt"
	"strings"
)

// FireRecord es lo observado de un incendio.
type FireRecord struct {
	X         uint32 `json:"x"`
	Y         uint32 `json:"y"`
	FirstTick uint64 `json:"first_tick"`
	// Rounds son las rondas en que todos los bots soltaron una gota; Drops,
	// las gotas enviadas; Applied, las que el hub resolvió como aplicadas.
	Rounds  int    `json:"rounds"`
	Drops   int    `json:"drops_sent"`
	Applied int    `json:"drops_applied"`
	Outcome string `json:"outcome"`
	// BonusBots son los bots (índices desde 0) cuyas hojas subieron al
	// menos el bono alrededor de la extinción. Indicativo.
	BonusBots []int `json:"bonus_bots"`
}

// Counters son los contadores que el hub difunde en `state`.
type Counters struct{ Humidity, Accepted uint64 }

// Measurement es lo medido, sin interpretar: la entrada de BuildReport.
type Measurement struct {
	Bots            int
	DurationS       float64
	Fires           []FireRecord
	Results         map[string]int
	Baseline, Final Counters
	Notes           []string
}

// Report es el informe del escenario, tal como se escribe en JSON.
type Report struct {
	Bots         int            `json:"bots"`
	DurationS    float64        `json:"duration_s"`
	FiresSeen    int            `json:"fires_seen"`
	Extinguished int            `json:"fires_extinguished"`
	BurnedOut    int            `json:"fires_burned_out"`
	Unresolved   int            `json:"fires_unresolved"`
	Fires        []FireRecord   `json:"fires"`
	Results      map[string]int `json:"results"`
	// SingleBonusOK es falso solo si algún incendio apagado dio señales de
	// bono en más de un bot. Es indicativo: las hojas también suben por
	// crecimiento.
	SingleBonusOK bool `json:"single_bonus_ok"`
	// HumidityContributed y AcceptedDrops son diferencias de los contadores
	// del hub, solo informativas: la igualdad exacta vale en modo demo.
	HumidityContributed uint64   `json:"humidity_contributed"`
	AcceptedDrops       uint64   `json:"accepted_drops"`
	Notes               []string `json:"notes,omitempty"`
}

// BuildReport aplica los criterios de CP-04. Falla un incendio visto con los
// bots activos que no se apagó (se consumió tras ser atacado o seguía
// ardiendo) o un bono que claramente se repartió a más de un bot. Ningún
// incendio en la ventana no es un fallo.
func BuildReport(m Measurement) Report {
	r := Report{Bots: m.Bots, DurationS: m.DurationS, Fires: m.Fires, Results: m.Results, SingleBonusOK: true, Notes: m.Notes}
	if r.Fires == nil {
		r.Fires = []FireRecord{}
	}
	for _, f := range m.Fires {
		r.FiresSeen++
		switch f.Outcome {
		case OutcomeExtinguished, OutcomeUnknown:
			r.Extinguished++
			switch {
			case len(f.BonusBots) > 1:
				r.SingleBonusOK = false
				r.Notes = append(r.Notes, fmt.Sprintf("El incendio de (%d, %d) mostró señales de bono en %d bots; debía ser uno.", f.X, f.Y, len(f.BonusBots)))
			case len(f.BonusBots) == 0:
				r.Notes = append(r.Notes, fmt.Sprintf("No se vio una subida de hojas del tamaño del bono en el incendio de (%d, %d); la comprobación es indicativa (las hojas también cambian por crecimiento y cada bot solo ve las suyas).", f.X, f.Y))
			}
		case OutcomeBurnedOut:
			r.BurnedOut++
		case OutcomeUnresolved:
			r.Unresolved++
		}
	}
	if b, f := m.Baseline, m.Final; f.Humidity < b.Humidity || f.Accepted < b.Accepted {
		r.Notes = append(r.Notes, "Un contador del hub retrocedió durante la prueba (¿alguien reinició el valle?); no se informan sus diferencias.")
	} else {
		r.HumidityContributed, r.AcceptedDrops = f.Humidity-b.Humidity, f.Accepted-b.Accepted
	}
	return r
}

// Passed dice si todos los incendios vistos se apagaron y el bono, según el
// indicio, no se repartió de más.
func (r Report) Passed() bool { return r.BurnedOut == 0 && r.Unresolved == 0 && r.SingleBonusOK }

// MarshalReport codifica el informe en JSON con sangría.
func MarshalReport(r Report) ([]byte, error) {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

var outcomeText = map[string]string{
	OutcomeExtinguished: "apagado",
	OutcomeBurnedOut:    "se consumió sin apagarse",
	OutcomeUnknown:      "desapareció (sin mapa para saber cómo)",
	OutcomeUnresolved:   "seguía ardiendo al terminar",
	OutcomePendingAtEnd: "apareció al final, sin tiempo para atacarlo",
}

// FormatSummary es el resumen para personas.
func FormatSummary(r Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d bots · %.0f s\n", r.Bots, r.DurationS)
	if r.FiresSeen == 0 {
		b.WriteString("Incendios: ninguno; no hubo incendios en la ventana (aparecen al azar en casillas secas con planta), así que CP-04 no se pudo ejercitar.\n")
	} else {
		fmt.Fprintf(&b, "Incendios vistos: %d · apagados: %d · consumidos: %d · sin apagar: %d\n", r.FiresSeen, r.Extinguished, r.BurnedOut, r.Unresolved)
	}
	for _, f := range r.Fires {
		fmt.Fprintf(&b, "  (%d, %d): %s · rondas: %d · gotas enviadas: %d · aplicadas: %d", f.X, f.Y, outcomeText[f.Outcome], f.Rounds, f.Drops, f.Applied)
		if f.Outcome == OutcomeExtinguished || f.Outcome == OutcomeUnknown {
			fmt.Fprintf(&b, " · bots con subida de hojas del tamaño del bono (indicativo): %v", f.BonusBots)
		}
		b.WriteString("\n")
	}
	if r.FiresSeen > 0 {
		fmt.Fprintf(&b, "Bono único (indicativo): %s\n", map[bool]string{true: "sin señales de repetirse", false: "NO cumple"}[r.SingleBonusOK])
	}
	fmt.Fprintf(&b, "Humedad aportada: %d · gotas aceptadas del hub: %d (solo informativo; se igualan exactamente en modo demo)\n", r.HumidityContributed, r.AcceptedDrops)
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
