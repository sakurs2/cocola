package metrics

import "github.com/prometheus/client_golang/prometheus"

// dummyCollector is a minimal Collector used only to prove MustRegister wires a
// service-specific collector onto the shared registry (the orchestrator bridge
// pattern in sandbox-manager).
type dummyCollector struct{ desc *prometheus.Desc }

func newDummyCollector() *dummyCollector {
	return &dummyCollector{desc: prometheus.NewDesc("cocola_dummy_metric", "test", nil, nil)}
}

func (c *dummyCollector) Describe(ch chan<- *prometheus.Desc) { ch <- c.desc }
func (c *dummyCollector) Collect(ch chan<- prometheus.Metric) {
	ch <- prometheus.MustNewConstMetric(c.desc, prometheus.GaugeValue, 42)
}
