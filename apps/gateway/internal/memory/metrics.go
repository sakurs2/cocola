package memory

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type serviceMetrics struct {
	recalls       *prometheus.CounterVec
	recallLatency prometheus.Histogram
	captures      *prometheus.CounterVec
}

func newServiceMetrics(registerer prometheus.Registerer) serviceMetrics {
	if registerer == nil {
		return serviceMetrics{}
	}
	metrics := serviceMetrics{
		recalls: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "cocola", Subsystem: "memory", Name: "recall_total",
			Help: "Memory recall attempts by outcome.",
		}, []string{"outcome"}),
		captures: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "cocola", Subsystem: "memory", Name: "capture_total",
			Help: "Memory capture jobs and skips by outcome.",
		}, []string{"outcome"}),
		recallLatency: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: "cocola", Subsystem: "memory", Name: "recall_duration_seconds",
			Help:    "End-to-end Memory recall latency in seconds.",
			Buckets: []float64{0.025, 0.05, 0.1, 0.25, 0.5, 1, 2},
		}),
	}
	registerer.MustRegister(metrics.recalls, metrics.captures, metrics.recallLatency)
	return metrics
}

func (m serviceMetrics) observeRecall(duration time.Duration) {
	if m.recallLatency != nil {
		m.recallLatency.Observe(duration.Seconds())
	}
}

func (m serviceMetrics) recall(outcome string) {
	if m.recalls != nil {
		m.recalls.WithLabelValues(outcome).Inc()
	}
}

func (m serviceMetrics) capture(outcome string) {
	if m.captures != nil {
		m.captures.WithLabelValues(outcome).Inc()
	}
}
