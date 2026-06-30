// Package obs bridges sandbox-manager's existing, dependency-free
// orchestrator.Metrics sink into Prometheus without rewriting it.
//
// Design (ADR-0011, M8): orchestrator.Metrics stays a tiny in-memory sink (it
// is all the M2 acceptance bench needs). This collector is a thin adapter that,
// on each scrape, reads Metrics.Snapshot() and emits the four binder signals as
// Prometheus metrics. There is no background goroutine and no duplicated state:
// the snapshot is the single source of truth, computed lazily at scrape time.
package obs

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/cocola-project/cocola/apps/sandbox-manager/internal/orchestrator"
)

// snapshotter is the read side of orchestrator.Metrics. Narrowing to an
// interface keeps this package testable with a fake and avoids a hard coupling
// to the concrete sink.
type snapshotter interface {
	Snapshot() orchestrator.Snapshot
}

// BinderCollector adapts a binder metrics snapshot to prometheus.Collector.
type BinderCollector struct {
	src snapshotter

	hitRate     *prometheus.Desc
	hits        *prometheus.Desc
	misses      *prometheus.Desc
	active      *prometheus.Desc
	createP50Ms *prometheus.Desc
	createP99Ms *prometheus.Desc
}

// NewBinderCollector builds a collector reading from the given metrics sink.
// constLabels (e.g. service="sandbox-manager") are stamped on every series so a
// shared Prometheus can attribute them.
func NewBinderCollector(src snapshotter, constLabels prometheus.Labels) *BinderCollector {
	d := func(name, help string) *prometheus.Desc {
		return prometheus.NewDesc(name, help, nil, constLabels)
	}
	return &BinderCollector{
		src:         src,
		hitRate:     d("cocola_sandbox_pool_hit_rate", "Session->sandbox reuse rate: hits/(hits+misses)."),
		hits:        d("cocola_sandbox_pool_hits_total", "Sessions that reused their existing sandbox."),
		misses:      d("cocola_sandbox_pool_misses_total", "Sessions that cold-created a sandbox."),
		active:      d("cocola_sandbox_active_count", "Live (non-paused) sandboxes, sampled by the reaper."),
		createP50Ms: d("cocola_sandbox_create_p50_milliseconds", "p50 of provider create latency on the miss path."),
		createP99Ms: d("cocola_sandbox_create_p99_milliseconds", "p99 of provider create latency on the miss path."),
	}
}

// Describe implements prometheus.Collector.
func (c *BinderCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.hitRate
	ch <- c.hits
	ch <- c.misses
	ch <- c.active
	ch <- c.createP50Ms
	ch <- c.createP99Ms
}

// Collect implements prometheus.Collector. It snapshots the sink once per scrape
// and emits the derived gauges/counters.
func (c *BinderCollector) Collect(ch chan<- prometheus.Metric) {
	s := c.src.Snapshot()
	ch <- prometheus.MustNewConstMetric(c.hitRate, prometheus.GaugeValue, s.HitRate)
	ch <- prometheus.MustNewConstMetric(c.hits, prometheus.CounterValue, float64(s.Hits))
	ch <- prometheus.MustNewConstMetric(c.misses, prometheus.CounterValue, float64(s.Misses))
	ch <- prometheus.MustNewConstMetric(c.active, prometheus.GaugeValue, float64(s.ActiveCount))
	ch <- prometheus.MustNewConstMetric(c.createP50Ms, prometheus.GaugeValue, s.CreateP50Ms)
	ch <- prometheus.MustNewConstMetric(c.createP99Ms, prometheus.GaugeValue, s.CreateP99Ms)
}
