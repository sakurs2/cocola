package orchestrator

import (
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// Metrics is a tiny, dependency-free sink for the three M2 acceptance signals:
//   - pool_hit_rate:        hits / (hits+misses) — how often a session reused a
//     sandbox instead of cold-creating.
//   - create_p99:           p99 of provider create latency on the miss path.
//   - active_sandbox_count: live (non-paused) sandboxes, sampled by the reaper.
//
// It is intentionally not Prometheus: M2 only needs to *prove* the binding
// behaviour in a bench. A Prometheus/OTel exporter can wrap Snapshot() later
// without touching the binder.
type Metrics struct {
	hits   atomic.Int64
	misses atomic.Int64
	active atomic.Int64

	mu        sync.Mutex
	createObs []float64 // create latencies in milliseconds (bounded reservoir)
}

// NewMetrics returns a ready sink.
func NewMetrics() *Metrics { return &Metrics{} }

const maxCreateObs = 4096

func (m *Metrics) recordHit() { m.hits.Add(1) }

func (m *Metrics) recordMiss(d time.Duration) {
	m.misses.Add(1)
	m.mu.Lock()
	if len(m.createObs) < maxCreateObs {
		m.createObs = append(m.createObs, float64(d.Microseconds())/1000.0)
	}
	m.mu.Unlock()
}

// setActive records the current live-sandbox gauge (called by the reaper sweep).
func (m *Metrics) setActive(n int64) { m.active.Store(n) }

// Snapshot is an immutable view of the metrics at a point in time.
type Snapshot struct {
	Hits        int64
	Misses      int64
	HitRate     float64
	ActiveCount int64
	CreateP99Ms float64
	CreateP50Ms float64
}

// Snapshot computes the current values. Safe for concurrent use.
func (m *Metrics) Snapshot() Snapshot {
	hits := m.hits.Load()
	misses := m.misses.Load()
	total := hits + misses
	var rate float64
	if total > 0 {
		rate = float64(hits) / float64(total)
	}

	m.mu.Lock()
	obs := make([]float64, len(m.createObs))
	copy(obs, m.createObs)
	m.mu.Unlock()
	sort.Float64s(obs)

	return Snapshot{
		Hits:        hits,
		Misses:      misses,
		HitRate:     rate,
		ActiveCount: m.active.Load(),
		CreateP99Ms: pct(obs, 0.99),
		CreateP50Ms: pct(obs, 0.50),
	}
}

// pct returns the q-quantile of a pre-sorted slice (0 if empty).
func pct(sorted []float64, q float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(q * float64(len(sorted)-1))
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}
