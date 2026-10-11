// Package metrics defines the Prometheus metrics exposed by the service.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"net/http"
)

// Metrics bundles all collectors on a dedicated registry.
type Metrics struct {
	reg *prometheus.Registry

	SMTPConnections   prometheus.Counter
	SMTPActiveConns   prometheus.Gauge
	Messages          *prometheus.CounterVec // result: accepted|rejected
	Attachments       *prometheus.CounterVec // result: uploaded|skipped|failed
	UploadedBytes     prometheus.Counter
	UploadDuration    prometheus.Histogram
	PaperlessRequests *prometheus.CounterVec // code: HTTP status or "error"
}

// New creates and registers all metrics.
func New() *Metrics {
	const ns = "smtp2paperless"
	m := &Metrics{
		reg: prometheus.NewRegistry(),
		SMTPConnections: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: ns, Name: "smtp_connections_total", Help: "Total SMTP connections accepted."}),
		SMTPActiveConns: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: ns, Name: "smtp_active_connections", Help: "Currently open SMTP sessions."}),
		Messages: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: ns, Name: "messages_total", Help: "Received messages by result (accepted, rejected)."}, []string{"result"}),
		Attachments: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: ns, Name: "attachments_total", Help: "Attachments by result (uploaded, skipped, failed)."}, []string{"result"}),
		UploadedBytes: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: ns, Name: "uploaded_bytes_total", Help: "Bytes of attachments successfully uploaded to paperless-ngx."}),
		UploadDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: ns, Name: "upload_duration_seconds", Help: "Duration of paperless-ngx uploads, including retries.",
			Buckets: prometheus.ExponentialBuckets(0.05, 2, 10)}),
		PaperlessRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: ns, Name: "paperless_requests_total", Help: "HTTP requests to paperless-ngx by status code."}, []string{"code"}),
	}
	m.reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		m.SMTPConnections, m.SMTPActiveConns, m.Messages, m.Attachments,
		m.UploadedBytes, m.UploadDuration, m.PaperlessRequests,
	)
	// Make label series visible with zero values from the start.
	for _, r := range []string{"accepted", "rejected"} {
		m.Messages.WithLabelValues(r)
	}
	for _, r := range []string{"uploaded", "skipped", "failed"} {
		m.Attachments.WithLabelValues(r)
	}
	return m
}

// ObserveRequest records one paperless-ngx HTTP request outcome.
func (m *Metrics) ObserveRequest(code string) {
	m.PaperlessRequests.WithLabelValues(code).Inc()
}

// Handler serves the metrics in Prometheus text format.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.reg, promhttp.HandlerOpts{})
}
