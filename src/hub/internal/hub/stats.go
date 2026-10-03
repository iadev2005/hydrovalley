package hub

import (
	"time"

	"github.com/iadev2005/hydrovalley/src/hub/internal/metrics"
)

const (
	// StatsWindowSeconds es la ventana móvil de las tasas y de la latencia
	// de /stats.
	StatsWindowSeconds = 10
	// latencySamples acota la memoria de la latencia: las últimas 4096
	// dentro de la ventana.
	latencySamples = 4096
	// stepSamples: el p95 de Step se calcula con los últimos 600 Step (60 s
	// de ticks), sin ventana de tiempo.
	stepSamples = 600
)

// StepStats resume la duración de WorldService.Step medida por el hub.
type StepStats struct {
	// Last y P95 (últimos 600 Step, fallidos incluidos).
	Last, P95 time.Duration
	// FailuresTotal cuenta los Step fallidos desde que arrancó el hub.
	FailuresTotal uint64
}

// Stats es la foto de métricas de /stats. Es cuenta propia del hub: no
// incluye goroutines (eso es del proceso y lo añade el transporte).
type Stats struct {
	Tick     uint64
	Paused   bool
	Breaker  string
	QueueLen int
	Sessions int
	// Acciones por segundo promediadas en StatsWindowSeconds: admitidas por
	// el hub y aplicadas (APPLIED, incluido el DUPLICATE tratado como
	// APPLIED tras un Step fallido).
	AdmittedPerSecond, AppliedPerSecond float64
	// Latencia desde la admisión hasta el resultado final (applied,
	// duplicate, rejected o expired) en la misma ventana.
	Latency metrics.Summary
	Step    StepStats

	HumidityTotal       uint64
	HumidityContributed uint64
	AcceptedDrops       uint64
	ActiveFires         int
	BootID              string
	At                  time.Time
}

// Stats devuelve una foto de las métricas. Toma el candado una sola vez y
// solo para copiar contadores y recorrer el último estado del valle (unas
// 1.024 casillas); los percentiles se calculan fuera de él.
func (h *Hub) Stats() Stats {
	h.mu.Lock()
	now := h.clock.Now()
	st := Stats{
		Tick: h.tick, Paused: h.paused, Breaker: h.breaker.State().String(), QueueLen: len(h.queue),
		AcceptedDrops: h.acceptedDrops, BootID: h.bootID, At: now,
		HumidityContributed: h.latestWorld.GetHumidityContributed(),
	}
	for _, p := range h.players {
		st.Sessions += len(p.sessions)
	}
	for _, tile := range h.latestWorld.GetTiles() {
		st.HumidityTotal += tile.GetHumidity()
		if tile.GetFire() != nil {
			st.ActiveFires++
		}
	}
	h.mu.Unlock()

	st.AdmittedPerSecond = h.admittedRate.PerSecond(now)
	st.AppliedPerSecond = h.appliedRate.PerSecond(now)
	st.Latency = h.latency.Summary(now)
	step := h.stepTimes.Summary(now)
	st.Step = StepStats{Last: step.Last, P95: step.P95, FailuresTotal: h.stepFailures.Load()}
	return st
}
