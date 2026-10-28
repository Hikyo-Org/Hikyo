package server

import (
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	MetricRefusalHealthy  = "hikyo_auth_refusal_accounting_healthy"
	MetricRefusalFailures = "hikyo_auth_refusal_accounting_failures_total"
	MetricRefusalPending  = "hikyo_auth_refusal_pending_window_timestamp_seconds"
	MetricRefusalInFlight = "hikyo_auth_refusal_writers_in_flight"
	MetricRefusalWaiting  = "hikyo_auth_refusal_writers_waiting"
)

type RefusalHealthSource interface{ RefusalHealth() service.RefusalHealth }
type refusalCollector struct {
	source RefusalHealthSource
	desc   [5]*prometheus.Desc
}

// SetRefusalSource is boot wiring. No source, request, principal, path or token
// is ever a label; every node exposes the same five fixed health series.
func (m *Metrics) SetRefusalSource(source RefusalHealthSource) {
	c := &refusalCollector{source: source, desc: [5]*prometheus.Desc{
		prometheus.NewDesc(MetricRefusalHealthy, "Whether the last refusal accounting attempt succeeded.", nil, nil),
		prometheus.NewDesc(MetricRefusalFailures, "Refusal updates or seals without a confirmed durable result, including queue and sink failure.", nil, nil),
		prometheus.NewDesc(MetricRefusalPending, "Datastore window start of pending refusal accounting, or zero when empty.", nil, nil),
		prometheus.NewDesc(MetricRefusalInFlight, "Refusal accounting writers in flight, bounded by eight.", nil, nil),
		prometheus.NewDesc(MetricRefusalWaiting, "Refusal accounting writers waiting, bounded by 64.", nil, nil),
	}}
	m.registry.MustRegister(c)
}
func (c *refusalCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, d := range c.desc {
		ch <- d
	}
}
func (c *refusalCollector) Collect(ch chan<- prometheus.Metric) {
	s := c.source.RefusalHealth()
	healthy := 0.0
	if s.Healthy {
		healthy = 1
	}
	values := [5]float64{healthy, float64(s.Failures), float64(s.PendingSince), float64(s.InFlight), float64(s.Waiting)}
	for i, value := range values {
		kind := prometheus.GaugeValue
		if i == 1 {
			kind = prometheus.CounterValue
		}
		ch <- prometheus.MustNewConstMetric(c.desc[i], kind, value)
	}
}
