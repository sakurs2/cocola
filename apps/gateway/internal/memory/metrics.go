package memory

import "github.com/prometheus/client_golang/prometheus"

type serviceMetrics struct {
	recalls  *prometheus.CounterVec
	captures *prometheus.CounterVec
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
	}
	registerer.MustRegister(metrics.recalls, metrics.captures)
	return metrics
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
