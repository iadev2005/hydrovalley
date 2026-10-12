// Paquete cheater: el bot tramposo. Comprueba en tres fases lo que el hub
// promete frente a un cliente que no juega limpio: el límite de gotas por
// segundo (RF-05, CP-03), las acciones repetidas (RF-09) y los mensajes
// inválidos (RF-06).
package cheater

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/iadev2005/hydrovalley/src/bots/internal/client"
)

// Códigos de error del hub (proto/hydrovalley/play/v1/play.proto).
const (
	codeInvalidMessage = "invalid_message"
	codeUnknownType    = "unknown_type"
)

// FloodSample es una gota de la inundación con su resultado final.
type FloodSample struct {
	// Offset va desde el inicio del envío hasta esta gota (reloj del bot).
	Offset time.Duration
	// Status y Reason son los del resultado final; Resolved es falso si no
	// llegó ninguno (o lo decidió el cliente al reconectarse).
	Status, Reason string
	Resolved       bool
}

// FloodMeasurement es lo medido en la fase 1, sin interpretar.
type FloodMeasurement struct {
	Window  time.Duration
	Samples []FloodSample
	// Limit es el límite del hub (15 por segundo) y Tolerance lo que se
	// admite de más por segundo medido (ver Run).
	Limit, Tolerance int
}

// Flood es el informe de la fase 1 (CP-03).
type Flood struct {
	DurationS float64        `json:"duration_s"`
	Sent      int            `json:"sent"`
	Results   map[string]int `json:"results"`
	// Unresolved son las enviadas sin resultado final: no se pueden clasificar.
	Unresolved int `json:"unresolved"`
	// Admitted son las que el límite dejó pasar: applied, duplicate, rejected
	// (también por falta de agua, que es economía y no límite) y expired.
	Admitted    int `json:"admitted"`
	RateLimited int `json:"rate_limited"`
	// AcceptedPerSecond son las admitidas entre la ventana de envío.
	AcceptedPerSecond float64 `json:"accepted_per_second"`
	// MaxPerBucket es el máximo de admitidas en un segundo de la ventana
	// (cubos de 1 s desde el primer envío, con el reloj del bot).
	MaxPerBucket int  `json:"max_per_bucket"`
	Limit        int  `json:"limit"`
	Tolerance    int  `json:"tolerance"`
	OK           bool `json:"ok"`
}

func isAdmittedStatus(s string) bool {
	switch s {
	case "applied", "duplicate", "rejected", "expired":
		return true
	}
	return false
}

// BuildFlood clasifica los resultados y aplica el criterio de CP-03: ningún
// segundo con más de Limit+Tolerance admitidas, alguna `rate_limited` y
// ninguna acción sin resultado.
func BuildFlood(m FloodMeasurement) Flood {
	f := Flood{
		DurationS: m.Window.Seconds(), Sent: len(m.Samples), Results: map[string]int{},
		Limit: m.Limit, Tolerance: m.Tolerance,
	}
	buckets := map[int]int{}
	for _, s := range m.Samples {
		if !s.Resolved {
			f.Unresolved++
			continue
		}
		key := s.Status
		if s.Status == "rejected" || s.Status == "error" {
			key += ":" + s.Reason
		}
		f.Results[key]++
		switch {
		case s.Status == "rate_limited":
			f.RateLimited++
		case isAdmittedStatus(s.Status):
			f.Admitted++
			buckets[int(s.Offset/time.Second)]++
		}
	}
	for _, n := range buckets {
		f.MaxPerBucket = max(f.MaxPerBucket, n)
	}
	if m.Window > 0 {
		f.AcceptedPerSecond = float64(f.Admitted) / m.Window.Seconds()
	}
	f.OK = f.Sent > 0 && f.Unresolved == 0 && f.RateLimited > 0 && f.MaxPerBucket <= m.Limit+m.Tolerance
	return f
}

// Duplicates es el informe de la fase 2 (RF-09).
type Duplicates struct {
	// Applied cuenta los `applied` de esa acción, sumando el resultado
	// original y cualquier marco posterior: debe ser exactamente 1.
	Applied         int      `json:"applied"`
	FirstStatuses   []string `json:"first_statuses"`
	DuplicateFrames int      `json:"duplicate_frames"`
	OK              bool     `json:"ok"`
}

// BuildDuplicates evalúa la fase 2. first son los resultados finales de la
// acción original y frames los marcos sueltos con su action_id. afterFinal
// son los reenvíos hechos tras el resultado final (cada uno debe recibir
// `duplicate`) e immediate los hechos mientras seguía pendiente (el hub los
// ignora; solo recibirían `duplicate` si llegaran tarde).
func BuildDuplicates(first []string, frames []client.Frame, afterFinal, immediate int) Duplicates {
	d := Duplicates{FirstStatuses: first}
	for _, s := range first {
		if s == "applied" {
			d.Applied++
		}
	}
	for _, f := range frames {
		switch f.Status {
		case "applied":
			d.Applied++
		case "duplicate":
			d.DuplicateFrames++
		}
	}
	d.OK = d.Applied == 1 && len(first) == 1 && first[0] == "applied" &&
		d.DuplicateFrames >= afterFinal && d.DuplicateFrames <= afterFinal+immediate
	return d
}

// Invalid es el informe de la fase 3 (RF-06).
type Invalid struct {
	Codes         []string `json:"codes"`
	MalformedOK   bool     `json:"malformed_ok"`
	UnknownTypeOK bool     `json:"unknown_type_ok"`
	// ConnectionOK: tras los mensajes inválidos, una gota recibió su
	// resultado final por la misma conexión.
	ConnectionOK bool `json:"connection_ok"`
	OK           bool `json:"ok"`
}

// BuildInvalid evalúa la fase 3 con los marcos `error` recibidos.
func BuildInvalid(frames []client.Frame, connectionOK bool) Invalid {
	v := Invalid{ConnectionOK: connectionOK}
	for _, f := range frames {
		if f.Type != "error" {
			continue
		}
		v.Codes = append(v.Codes, f.Reason)
		switch f.Reason {
		case codeInvalidMessage:
			v.MalformedOK = true
		case codeUnknownType:
			v.UnknownTypeOK = true
		}
	}
	v.OK = v.MalformedOK && v.UnknownTypeOK && connectionOK
	return v
}

// Report es el informe del escenario, tal como se escribe en JSON.
type Report struct {
	Flood      Flood      `json:"flood"`
	Duplicates Duplicates `json:"duplicates"`
	Invalid    Invalid    `json:"invalid"`
	Notes      []string   `json:"notes,omitempty"`
}

// Passed dice si las tres fases cumplen.
func (r Report) Passed() bool { return r.Flood.OK && r.Duplicates.OK && r.Invalid.OK }

// MarshalReport codifica el informe en JSON con sangría.
func MarshalReport(r Report) ([]byte, error) {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func decimal(f float64) string { return strings.Replace(fmt.Sprintf("%.1f", f), ".", ",", 1) }

func verdict(ok bool) string {
	if ok {
		return "cumple"
	}
	return "NO cumple"
}

// FormatSummary es el resumen para personas.
func FormatSummary(r Report) string {
	var b strings.Builder
	f := r.Flood
	fmt.Fprintf(&b, "CP-03 · límite de gotas (RF-05): %s\n", verdict(f.OK))
	fmt.Fprintf(&b, "  Enviadas: %d en %s s · admitidas por el hub: %d · rechazadas por límite (rate_limited): %d",
		f.Sent, decimal(f.DurationS), f.Admitted, f.RateLimited)
	if f.Unresolved > 0 {
		fmt.Fprintf(&b, " · sin resultado: %d", f.Unresolved)
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "  Admitidas por segundo: %s de media · máximo en un segundo: %d (límite %d, tolerancia %d en los bordes de la ventana)\n",
		decimal(f.AcceptedPerSecond), f.MaxPerBucket, f.Limit, f.Tolerance)
	keys := make([]string, 0, len(f.Results))
	for k := range f.Results {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", k, f.Results[k]))
	}
	fmt.Fprintf(&b, "  Resultados: %s\n", strings.Join(parts, " · "))
	d := r.Duplicates
	fmt.Fprintf(&b, "RF-09 · acción repetida: %s (aplicada %d vez, `duplicate` recibidos: %d)\n", verdict(d.OK), d.Applied, d.DuplicateFrames)
	v := r.Invalid
	fmt.Fprintf(&b, "RF-06 · mensajes inválidos: %s (códigos: %s; la conexión siguió funcionando: %t)\n",
		verdict(v.OK), strings.Join(v.Codes, ", "), v.ConnectionOK)
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
