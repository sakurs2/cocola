// sandbox-manager owns the SandboxProvider abstraction. It receives Create/Exec/
// Destroy gRPC calls from agent-runtime and dispatches to a concrete provider:
//   - DockerProvider (M1)
//   - K8sGVisorProvider (later)
//   - E2BProvider / CubeSandboxProvider (community)
//
// The provider is chosen at startup from COCOLA_SANDBOX_PROVIDER (default: docker).
// Nothing below the provider factory knows which backend is in use.
package main

import (
	"context"
	"net"
	"net/http"
	"os"

	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	"github.com/cocola-project/cocola/apps/sandbox-manager/internal/obs"
	"github.com/cocola-project/cocola/apps/sandbox-manager/internal/orchestrator"
	"github.com/cocola-project/cocola/apps/sandbox-manager/internal/provider"
	"github.com/cocola-project/cocola/apps/sandbox-manager/internal/provider/docker"
	"github.com/cocola-project/cocola/apps/sandbox-manager/internal/provider/k8s"
	"github.com/cocola-project/cocola/apps/sandbox-manager/internal/server"
	"github.com/cocola-project/cocola/packages/go-common/logger"
	"github.com/cocola-project/cocola/packages/go-common/metrics"
	rds "github.com/cocola-project/cocola/packages/go-common/redis"
	sandboxv1 "github.com/cocola-project/cocola/packages/proto/gen/go/cocola/sandbox/v1"
)

func main() {
	log := logger.Must()
	defer func() { _ = log.Sync() }()

	addr := getenv("COCOLA_SANDBOX_ADDR", ":50051")
	backend := getenv("COCOLA_SANDBOX_PROVIDER", docker.ProviderName)

	// Observability registry: shared by the gRPC interceptors below and the
	// binder collector bridge. Exposed on a dedicated port at the end of main.
	reg := metrics.New("sandbox-manager")

	p, err := newProvider(backend)
	if err != nil {
		log.Sugar().Fatalf("init provider %q: %v", backend, err)
	}

	// Wire the session<->sandbox binder over Redis. If Redis is unreachable we
	// degrade gracefully: the raw provider RPCs still work, only the binding
	// RPCs (Acquire/Heartbeat/Release) return Unimplemented. This keeps local
	// single-process debugging possible without standing up Redis.
	ctx := context.Background()
	var binder *orchestrator.Binder
	kv, rerr := rds.New(ctx, rds.ConfigFromEnv())
	if rerr != nil {
		log.Sugar().Warnw("redis unavailable; session-binding RPCs disabled",
			"err", rerr)
	} else {
		defer func() { _ = kv.Close() }()
		bm := orchestrator.NewMetrics()
		// Bridge the in-memory binder sink into Prometheus (no rewrite; the
		// collector reads Snapshot() lazily at scrape time).
		reg.MustRegister(obs.NewBinderCollector(bm, prometheus.Labels{"service": "sandbox-manager"}))
		cfg := orchestrator.ConfigFromEnv()
		binder = orchestrator.NewBinder(kv, p, cfg).WithMetrics(bm)
		go binder.RunReaper(ctx) // background two-stage Pause-then-Destroy GC
		eff := binder.EffectiveConfig()
		log.Sugar().Infow("session<->sandbox binder enabled",
			"lease_ttl", eff.LeaseTTL,
			"heartbeat_every", eff.HeartbeatEvery,
			"destroy_grace", eff.DestroyGrace,
			"reaper_every", eff.ReaperEvery)
	}

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Sugar().Fatalf("listen %s: %v", addr, err)
	}

	gs := grpc.NewServer(
		grpc.UnaryInterceptor(reg.UnaryServerInterceptor()),
		grpc.StreamInterceptor(reg.StreamServerInterceptor()),
	)
	sandboxv1.RegisterSandboxServiceServer(gs, server.New(p, binder))
	reflection.Register(gs) // enables grpcurl describe/list for local debugging

	if metricsAddr := getenv("COCOLA_METRICS_ADDR", ":9092"); metricsAddr != "" {
		go func() {
			log.Sugar().Infow("sandbox-manager metrics", "addr", metricsAddr)
			msrv := &http.Server{Addr: metricsAddr, Handler: reg.Mux()}
			if err := msrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Sugar().Warnw("sandbox-manager metrics server error", "err", err)
			}
		}()
	}

	log.Sugar().Infow("cocola sandbox-manager listening",
		"milestone", "M2", "addr", addr, "provider", backend)
	if err := gs.Serve(lis); err != nil {
		log.Sugar().Fatalf("serve: %v", err)
	}
}

// newProvider is the single place that maps a backend name to a concrete
// implementation. Adding a new backend = one case here + a package under
// internal/provider/. No other file changes.
func newProvider(name string) (provider.SandboxProvider, error) {
	switch name {
	case docker.ProviderName:
		return docker.New()
	case k8s.ProviderName:
		return k8s.New()
	default:
		// Allow providers that self-registered via Register() in their init().
		if p := provider.Get(name); p != nil {
			return p, nil
		}
		return docker.New()
	}
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
