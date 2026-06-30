package obs

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/cocola-project/cocola/apps/sandbox-manager/internal/orchestrator"
)

// fakeSnap returns a fixed snapshot, proving the collector reads via the
// snapshotter seam without depending on the concrete binder sink internals.
type fakeSnap struct{ s orchestrator.Snapshot }

func (f fakeSnap) Snapshot() orchestrator.Snapshot { return f.s }

func TestBinderCollector_EmitsSnapshot(t *testing.T) {
	src := fakeSnap{s: orchestrator.Snapshot{
		Hits:        7,
		Misses:      3,
		HitRate:     0.7,
		ActiveCount: 5,
		CreateP50Ms: 12.5,
		CreateP99Ms: 99.0,
	}}
	c := NewBinderCollector(src, prometheus.Labels{"service": "sandbox-manager"})

	reg := prometheus.NewRegistry()
	reg.MustRegister(c)

	want := `
# HELP cocola_sandbox_active_count Live (non-paused) sandboxes, sampled by the reaper.
# TYPE cocola_sandbox_active_count gauge
cocola_sandbox_active_count{service="sandbox-manager"} 5
# HELP cocola_sandbox_pool_hit_rate Session->sandbox reuse rate: hits/(hits+misses).
# TYPE cocola_sandbox_pool_hit_rate gauge
cocola_sandbox_pool_hit_rate{service="sandbox-manager"} 0.7
# HELP cocola_sandbox_pool_hits_total Sessions that reused their existing sandbox.
# TYPE cocola_sandbox_pool_hits_total counter
cocola_sandbox_pool_hits_total{service="sandbox-manager"} 7
# HELP cocola_sandbox_pool_misses_total Sessions that cold-created a sandbox.
# TYPE cocola_sandbox_pool_misses_total counter
cocola_sandbox_pool_misses_total{service="sandbox-manager"} 3
`
	if err := testutil.GatherAndCompare(reg, strings.NewReader(want),
		"cocola_sandbox_active_count",
		"cocola_sandbox_pool_hit_rate",
		"cocola_sandbox_pool_hits_total",
		"cocola_sandbox_pool_misses_total",
	); err != nil {
		t.Fatalf("collector output mismatch: %v", err)
	}
}

// TestBinderCollector_OverLiveSink proves the collector reads the real sink via
// Snapshot() at scrape time and emits exactly one series per gauge.
func TestBinderCollector_OverLiveSink(t *testing.T) {
	m := orchestrator.NewMetrics()
	c := NewBinderCollector(m, nil)
	reg := prometheus.NewRegistry()
	reg.MustRegister(c)

	// A fresh sink reports a single hit_rate series (value 0, total==0).
	if n := testutil.CollectAndCount(reg, "cocola_sandbox_pool_hit_rate"); n != 1 {
		t.Fatalf("want 1 hit_rate series, got %d", n)
	}
}
