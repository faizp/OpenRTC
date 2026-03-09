package observability

import (
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
)

type Counter struct {
	value int64
}

func (c *Counter) Inc() {
	atomic.AddInt64(&c.value, 1)
}

func (c *Counter) Load() int64 {
	return atomic.LoadInt64(&c.value)
}

type Gauge struct {
	value int64
}

func (g *Gauge) Inc() {
	atomic.AddInt64(&g.value, 1)
}

func (g *Gauge) Dec() {
	atomic.AddInt64(&g.value, -1)
}

func (g *Gauge) Set(value float64) {
	atomic.StoreInt64(&g.value, int64(value))
}

func (g *Gauge) Load() int64 {
	return atomic.LoadInt64(&g.value)
}

type RuntimeMetrics struct {
	ActiveConnections    *Gauge
	ActiveRooms          *Gauge
	JoinsTotal           *Counter
	LeavesTotal          *Counter
	EventsTotal          *Counter
	PresenceUpdatesTotal *Counter
	QueueOverflowsTotal  *Counter
}

type AdminMetrics struct {
	AdminPublishesTotal *Counter
}

func NewRuntimeMetrics() *RuntimeMetrics {
	return &RuntimeMetrics{
		ActiveConnections:    &Gauge{},
		ActiveRooms:          &Gauge{},
		JoinsTotal:           &Counter{},
		LeavesTotal:          &Counter{},
		EventsTotal:          &Counter{},
		PresenceUpdatesTotal: &Counter{},
		QueueOverflowsTotal:  &Counter{},
	}
}

func NewAdminMetrics() *AdminMetrics {
	return &AdminMetrics{
		AdminPublishesTotal: &Counter{},
	}
}

func (m *RuntimeMetrics) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeMetrics(w, []metricLine{
			{name: "openrtc_runtime_active_connections", value: m.ActiveConnections.Load()},
			{name: "openrtc_runtime_active_rooms", value: m.ActiveRooms.Load()},
			{name: "openrtc_runtime_joins_total", value: m.JoinsTotal.Load()},
			{name: "openrtc_runtime_leaves_total", value: m.LeavesTotal.Load()},
			{name: "openrtc_runtime_events_total", value: m.EventsTotal.Load()},
			{name: "openrtc_runtime_presence_updates_total", value: m.PresenceUpdatesTotal.Load()},
			{name: "openrtc_runtime_queue_overflows_total", value: m.QueueOverflowsTotal.Load()},
		})
	})
}

func (m *AdminMetrics) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeMetrics(w, []metricLine{
			{name: "openrtc_admin_publishes_total", value: m.AdminPublishesTotal.Load()},
		})
	})
}

type metricLine struct {
	name  string
	value int64
}

func writeMetrics(w http.ResponseWriter, metrics []metricLine) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	lines := make([]string, 0, len(metrics))
	for _, metric := range metrics {
		lines = append(lines, fmt.Sprintf("%s %d", metric.name, metric.value))
	}
	_, _ = w.Write([]byte(strings.Join(lines, "\n") + "\n"))
}
