package observability

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRuntimeMetricsHandlerWritesPrometheusLines(t *testing.T) {
	metrics := NewRuntimeMetrics()
	metrics.ActiveConnections.Set(3)
	metrics.ActiveRooms.Inc()
	metrics.JoinsTotal.Inc()
	metrics.LeavesTotal.Inc()
	metrics.EventsTotal.Inc()
	metrics.PresenceUpdatesTotal.Inc()
	metrics.QueueOverflowsTotal.Inc()

	recorder := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	if contentType := recorder.Header().Get("Content-Type"); contentType != "text/plain; version=0.0.4" {
		t.Fatalf("unexpected content type: %s", contentType)
	}
	body := recorder.Body.String()
	expected := []string{
		"openrtc_runtime_active_connections 3",
		"openrtc_runtime_active_rooms 1",
		"openrtc_runtime_joins_total 1",
		"openrtc_runtime_leaves_total 1",
		"openrtc_runtime_events_total 1",
		"openrtc_runtime_presence_updates_total 1",
		"openrtc_runtime_queue_overflows_total 1",
	}
	for _, line := range expected {
		if !strings.Contains(body, line+"\n") {
			t.Fatalf("metrics body missing %q: %s", line, body)
		}
	}
}

func TestAdminMetricsHandlerWritesPrometheusLines(t *testing.T) {
	metrics := NewAdminMetrics()
	metrics.AdminPublishesTotal.Inc()

	recorder := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	if body := recorder.Body.String(); body != "openrtc_admin_publishes_total 1\n" {
		t.Fatalf("unexpected admin metrics body: %q", body)
	}
}

func TestGaugeAndCounterHelpers(t *testing.T) {
	var gauge Gauge
	gauge.Set(2.9)
	gauge.Inc()
	gauge.Dec()
	if got := gauge.Load(); got != 2 {
		t.Fatalf("unexpected gauge value: %d", got)
	}

	var counter Counter
	counter.Inc()
	counter.Inc()
	if got := counter.Load(); got != 2 {
		t.Fatalf("unexpected counter value: %d", got)
	}
}
