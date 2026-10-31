package transport

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestMetricsExposesPrometheusText(t *testing.T) {
	e := newEnv(t)
	conn := e.dial(e.as(ana, "map=false"))
	e.waitSessions(1)
	send(t, conn, `{"type":"drop","action_id":"`+actionID+`","x":0,"y":0}`)
	e.waitQueue(1)
	e.tick()
	next(t, conn, "result")

	resp := e.get("/metrics")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/metrics = %d, want 200", resp.StatusCode)
	}
	contentType := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "text/plain") {
		t.Errorf("Content-Type = %q, want text/plain", contentType)
	}
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	body := string(bodyBytes)

	expectedMetrics := []string{
		"hydrovalley_hub_sessions 1",
		"hydrovalley_hub_goroutines",
		"hydrovalley_hub_actions_admitted_total 1",
		"hydrovalley_hub_actions_applied_total 1",
		"hydrovalley_hub_actions_admitted_rate",
		"hydrovalley_hub_actions_applied_rate",
		"hydrovalley_hub_latency_ms{quantile=\"0.5\"}",
		"hydrovalley_hub_latency_ms{quantile=\"0.95\"}",
		"# TYPE hydrovalley_hub_tick gauge",
		"hydrovalley_hub_tick 1",
		"hydrovalley_hub_step_last_duration_ms",
		"hydrovalley_hub_step_duration_ms{quantile=\"0.95\"}",
		"hydrovalley_hub_step_failures_total 0",
		"hydrovalley_hub_queue_length 0",
		"hydrovalley_hub_paused 0",
		"hydrovalley_hub_breaker_code 0",
		"hydrovalley_hub_breaker_state{state=\"closed\"} 1",
		"hydrovalley_valley_humidity_total",
		"hydrovalley_valley_humidity_contributed",
		"hydrovalley_valley_accepted_drops_total 1",
		"hydrovalley_valley_active_fires 0",
		"hydrovalley_hub_info{boot_id=\"boot\",restored_tick=\"0\"} 1",
	}

	for _, metric := range expectedMetrics {
		if !strings.Contains(body, metric) {
			t.Errorf("missing metric %q in /metrics output:\n%s", metric, body)
		}
	}
}
