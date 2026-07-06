// Package metrics is cocola's shared Prometheus instrumentation layer. Every Go
// service (gateway, sandbox-manager, admin-api) builds its observability surface
// from here so that metric names, the /metrics handler, and the RED middleware
// are identical across the fleet.
//
// Design (ADR-0011, M8):
//   - One *prometheus.Registry per service (not the global default registry) so
//     tests are hermetic and two registries never collide.
//   - Go runtime + process collectors are registered by default (GC, goroutines,
//     fds, rss) — free baseline signals.
//   - Handler() returns a promhttp handler the caller mounts on a dedicated
//     observability port. Per <network_security>, services bind that port only
//     in real deployments; unit tests use httptest and never bind a real port.
package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Registry wraps a prometheus.Registry plus the cocola-standard metric vectors
// that every service shares (RED: rate, errors, duration). Service-specific
// collectors register onto the same registry via Register / MustRegister.
type Registry struct {
	reg     *prometheus.Registry
	service string

	// RED request metrics, labelled by (transport, method, code). "transport"
	// is "grpc" or "http"; "method" is the gRPC full method or HTTP route;
	// "code" is the status code/string.
	reqTotal    *prometheus.CounterVec
	reqDuration *prometheus.HistogramVec
	inflight    *prometheus.GaugeVec
	auditErrors prometheus.Counter
}

// Option configures a Registry at construction time.
type Option func(*options)

type options struct {
	durationBuckets []float64
	constLabels     prometheus.Labels
}

// WithDurationBuckets overrides the default latency histogram buckets (seconds).
func WithDurationBuckets(b []float64) Option {
	return func(o *options) { o.durationBuckets = b }
}

// WithConstLabels attaches constant labels (e.g. version) to every metric.
func WithConstLabels(l prometheus.Labels) Option {
	return func(o *options) { o.constLabels = l }
}

// defaultBuckets covers sub-millisecond RPCs up to multi-second cold starts
// (sandbox create can take seconds), so a single histogram serves both.
var defaultBuckets = []float64{
	0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30,
}

// New builds a Registry for a service. The service name becomes the "service"
// const label on every metric, so a shared Prometheus can tell fleets apart.
func New(service string, opts ...Option) *Registry {
	o := options{durationBuckets: defaultBuckets, constLabels: prometheus.Labels{}}
	for _, fn := range opts {
		fn(&o)
	}
	o.constLabels["service"] = service

	reg := prometheus.NewRegistry()
	// Free baseline: Go runtime + process stats.
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)

	r := &Registry{
		reg:     reg,
		service: service,
		reqTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name:        "cocola_requests_total",
			Help:        "Total requests handled, by transport, method and status code.",
			ConstLabels: o.constLabels,
		}, []string{"transport", "method", "code"}),
		reqDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:        "cocola_request_duration_seconds",
			Help:        "Request handling latency in seconds, by transport and method.",
			Buckets:     o.durationBuckets,
			ConstLabels: o.constLabels,
		}, []string{"transport", "method"}),
		inflight: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name:        "cocola_requests_in_flight",
			Help:        "In-flight requests, by transport.",
			ConstLabels: o.constLabels,
		}, []string{"transport"}),
		auditErrors: prometheus.NewCounter(prometheus.CounterOpts{
			Name:        "cocola_audit_write_errors_total",
			Help:        "Total audit-event write failures.",
			ConstLabels: o.constLabels,
		}),
	}
	reg.MustRegister(r.reqTotal, r.reqDuration, r.inflight, r.auditErrors)
	return r
}

// Registerer exposes the underlying registry so service-specific collectors
// (e.g. sandbox-manager's orchestrator bridge) can attach onto it.
func (r *Registry) Registerer() prometheus.Registerer { return r.reg }

// MustRegister attaches custom collectors, panicking on duplicate registration
// (same semantics as prometheus.MustRegister).
func (r *Registry) MustRegister(cs ...prometheus.Collector) { r.reg.MustRegister(cs...) }

// Handler returns the HTTP handler that serves the Prometheus exposition format.
// Mount it at /metrics on the service's observability port.
func (r *Registry) Handler() http.Handler {
	return promhttp.HandlerFor(r.reg, promhttp.HandlerOpts{Registry: r.reg})
}

// observeRequest records one finished request into the RED vectors. Internal;
// the transport middlewares (HTTP, gRPC) call it.
func (r *Registry) observeRequest(transport, method, code string, seconds float64) {
	r.reqTotal.WithLabelValues(transport, method, code).Inc()
	r.reqDuration.WithLabelValues(transport, method).Observe(seconds)
}

// IncAuditWriteError records one best-effort audit write failure.
func (r *Registry) IncAuditWriteError() {
	if r != nil {
		r.auditErrors.Inc()
	}
}
