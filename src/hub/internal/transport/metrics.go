package transport

import (
	"fmt"
	"io"
	"runtime"
	"strconv"

	"github.com/iadev2005/hydrovalley/src/hub/internal/hub"
)

// writePrometheusMetrics escribe las métricas en formato texto estándar de Prometheus (OpenMetrics).
func writePrometheusMetrics(w io.Writer, st hub.Stats) {
	// Breaker state code: 0 = closed, 1 = open, 2 = half_open
	breakerCode := 0
	switch st.Breaker {
	case "open":
		breakerCode = 1
	case "half_open":
		breakerCode = 2
	}

	pausedVal := 0
	if st.Paused {
		pausedVal = 1
	}

	fmt.Fprintf(w, "# HELP hydrovalley_hub_sessions Current active player sessions\n")
	fmt.Fprintf(w, "# TYPE hydrovalley_hub_sessions gauge\n")
	fmt.Fprintf(w, "hydrovalley_hub_sessions %d\n", st.Sessions)

	fmt.Fprintf(w, "# HELP hydrovalley_hub_goroutines Current running goroutines\n")
	fmt.Fprintf(w, "# TYPE hydrovalley_hub_goroutines gauge\n")
	fmt.Fprintf(w, "hydrovalley_hub_goroutines %d\n", runtime.NumGoroutine())

	fmt.Fprintf(w, "# HELP hydrovalley_hub_actions_admitted_total Cumulative admitted player actions\n")
	fmt.Fprintf(w, "# TYPE hydrovalley_hub_actions_admitted_total counter\n")
	fmt.Fprintf(w, "hydrovalley_hub_actions_admitted_total %d\n", st.AdmittedTotal)

	fmt.Fprintf(w, "# HELP hydrovalley_hub_actions_applied_total Cumulative applied player actions\n")
	fmt.Fprintf(w, "# TYPE hydrovalley_hub_actions_applied_total counter\n")
	fmt.Fprintf(w, "hydrovalley_hub_actions_applied_total %d\n", st.AppliedTotal)

	fmt.Fprintf(w, "# HELP hydrovalley_hub_actions_admitted_rate Admitted actions per second in rolling window\n")
	fmt.Fprintf(w, "# TYPE hydrovalley_hub_actions_admitted_rate gauge\n")
	fmt.Fprintf(w, "hydrovalley_hub_actions_admitted_rate %s\n", strconv.FormatFloat(round1(st.AdmittedPerSecond), 'f', -1, 64))

	fmt.Fprintf(w, "# HELP hydrovalley_hub_actions_applied_rate Applied actions per second in rolling window\n")
	fmt.Fprintf(w, "# TYPE hydrovalley_hub_actions_applied_rate gauge\n")
	fmt.Fprintf(w, "hydrovalley_hub_actions_applied_rate %s\n", strconv.FormatFloat(round1(st.AppliedPerSecond), 'f', -1, 64))

	fmt.Fprintf(w, "# HELP hydrovalley_hub_latency_ms Action admission to final result latency in milliseconds\n")
	fmt.Fprintf(w, "# TYPE hydrovalley_hub_latency_ms gauge\n")
	fmt.Fprintf(w, "hydrovalley_hub_latency_ms{quantile=\"0.5\"} %s\n", strconv.FormatFloat(ms(st.Latency.P50), 'f', -1, 64))
	fmt.Fprintf(w, "hydrovalley_hub_latency_ms{quantile=\"0.95\"} %s\n", strconv.FormatFloat(ms(st.Latency.P95), 'f', -1, 64))

	fmt.Fprintf(w, "# HELP hydrovalley_hub_tick Current logical world tick\n")
	fmt.Fprintf(w, "# TYPE hydrovalley_hub_tick counter\n")
	fmt.Fprintf(w, "hydrovalley_hub_tick %d\n", st.Tick)

	fmt.Fprintf(w, "# HELP hydrovalley_hub_step_last_duration_ms Last WorldService.Step duration in milliseconds\n")
	fmt.Fprintf(w, "# TYPE hydrovalley_hub_step_last_duration_ms gauge\n")
	fmt.Fprintf(w, "hydrovalley_hub_step_last_duration_ms %s\n", strconv.FormatFloat(ms(st.Step.Last), 'f', -1, 64))

	fmt.Fprintf(w, "# HELP hydrovalley_hub_step_duration_ms WorldService.Step duration in milliseconds\n")
	fmt.Fprintf(w, "# TYPE hydrovalley_hub_step_duration_ms gauge\n")
	fmt.Fprintf(w, "hydrovalley_hub_step_duration_ms{quantile=\"0.95\"} %s\n", strconv.FormatFloat(ms(st.Step.P95), 'f', -1, 64))

	fmt.Fprintf(w, "# HELP hydrovalley_hub_step_failures_total Total failed WorldService.Step calls\n")
	fmt.Fprintf(w, "# TYPE hydrovalley_hub_step_failures_total counter\n")
	fmt.Fprintf(w, "hydrovalley_hub_step_failures_total %d\n", st.Step.FailuresTotal)

	fmt.Fprintf(w, "# HELP hydrovalley_hub_queue_length Current queued action commands count\n")
	fmt.Fprintf(w, "# TYPE hydrovalley_hub_queue_length gauge\n")
	fmt.Fprintf(w, "hydrovalley_hub_queue_length %d\n", st.QueueLen)

	fmt.Fprintf(w, "# HELP hydrovalley_hub_paused Whether the valley is paused (1=true, 0=false)\n")
	fmt.Fprintf(w, "# TYPE hydrovalley_hub_paused gauge\n")
	fmt.Fprintf(w, "hydrovalley_hub_paused %d\n", pausedVal)

	fmt.Fprintf(w, "# HELP hydrovalley_hub_breaker_code Circuit breaker state code (0=closed, 1=open, 2=half_open)\n")
	fmt.Fprintf(w, "# TYPE hydrovalley_hub_breaker_code gauge\n")
	fmt.Fprintf(w, "hydrovalley_hub_breaker_code %d\n", breakerCode)

	fmt.Fprintf(w, "# HELP hydrovalley_hub_breaker_state Circuit breaker state label\n")
	fmt.Fprintf(w, "# TYPE hydrovalley_hub_breaker_state gauge\n")
	fmt.Fprintf(w, "hydrovalley_hub_breaker_state{state=\"%s\"} 1\n", st.Breaker)

	fmt.Fprintf(w, "# HELP hydrovalley_valley_humidity_total Total humidity across all valley tiles\n")
	fmt.Fprintf(w, "# TYPE hydrovalley_valley_humidity_total gauge\n")
	fmt.Fprintf(w, "hydrovalley_valley_humidity_total %d\n", st.HumidityTotal)

	fmt.Fprintf(w, "# HELP hydrovalley_valley_humidity_contributed Cumulative humidity contributed by player drops\n")
	fmt.Fprintf(w, "# TYPE hydrovalley_valley_humidity_contributed gauge\n")
	fmt.Fprintf(w, "hydrovalley_valley_humidity_contributed %d\n", st.HumidityContributed)

	fmt.Fprintf(w, "# HELP hydrovalley_valley_accepted_drops_total Total drops accepted and applied since hub boot\n")
	fmt.Fprintf(w, "# TYPE hydrovalley_valley_accepted_drops_total counter\n")
	fmt.Fprintf(w, "hydrovalley_valley_accepted_drops_total %d\n", st.AcceptedDrops)

	fmt.Fprintf(w, "# HELP hydrovalley_valley_active_fires Current active fires in the valley\n")
	fmt.Fprintf(w, "# TYPE hydrovalley_valley_active_fires gauge\n")
	fmt.Fprintf(w, "hydrovalley_valley_active_fires %d\n", st.ActiveFires)

	fmt.Fprintf(w, "# HELP hydrovalley_hub_info Hub and simulator boot metadata\n")
	fmt.Fprintf(w, "# TYPE hydrovalley_hub_info gauge\n")
	fmt.Fprintf(w, "hydrovalley_hub_info{boot_id=\"%s\",restored_tick=\"%d\"} 1\n", st.BootID, st.RestoredTick)
}
