// sandbox-manager owns the SandboxProvider abstraction. It receives Create/Exec/
// Destroy gRPC calls from agent-runtime to OpenSandbox (ADR-0014). The provider
// interface remains the internal test seam; there is only one production backend.
package main

import (
	"context"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	"github.com/cocola-project/cocola/apps/sandbox-manager/internal/obs"
	"github.com/cocola-project/cocola/apps/sandbox-manager/internal/orchestrator"
	"github.com/cocola-project/cocola/apps/sandbox-manager/internal/provider"
	"github.com/cocola-project/cocola/apps/sandbox-manager/internal/provider/opensandbox"
	"github.com/cocola-project/cocola/apps/sandbox-manager/internal/server"
	"github.com/cocola-project/cocola/packages/go-common/logger"
	"github.com/cocola-project/cocola/packages/go-common/metrics"
	rds "github.com/cocola-project/cocola/packages/go-common/redis"
	"github.com/cocola-project/cocola/packages/go-common/tracing"
	sandboxv1 "github.com/cocola-project/cocola/packages/proto/gen/go/cocola/sandbox/v1"
)

func main() {
	log := logger.WithService(logger.Must(), "sandbox-manager", "sandbox-manager")
	defer func() { _ = log.Sync() }()

	stopTracing, terr := tracing.Init(context.Background(), tracing.ConfigFromEnv("sandbox-manager"))
	if terr != nil {
		log.Sugar().Warnw("tracing init failed", "err", terr)
	} else {
		defer func() { _ = stopTracing(context.Background()) }()
	}

	addr := getenv("COCOLA_SANDBOX_ADDR", ":50051")

	// Observability registry: shared by the gRPC interceptors below and the
	// binder collector bridge. Exposed on a dedicated port at the end of main.
	reg := metrics.New("sandbox-manager")

	base, err := opensandbox.New()
	if err != nil {
		log.Sugar().Fatalf("init OpenSandbox provider: %v", err)
	}
	var p provider.SandboxProvider = base

	// Redis is required by the session<->sandbox binder. Starting without it
	// would expose a healthy server whose core binding RPCs are unusable.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Retry the initial dial instead of a single shot: Compose orders us after
	// redis is healthy, but transient DNS/network races (or a slow-to-DNS bridge)
	// would otherwise permanently disable binding RPCs until a manual restart.
	// We wait up to COCOLA_REDIS_CONNECT_TIMEOUT (default 30s) before failing.
	kv, rerr := dialRedisWithRetry(ctx, log, redisConnectTimeout())
	if rerr != nil {
		log.Sugar().Fatalf("redis unavailable after retries: %v", rerr)
	}
	defer func() { _ = kv.Close() }()
	bm := orchestrator.NewMetrics()
	// Bridge the in-memory binder sink into Prometheus (no rewrite; the
	// collector reads Snapshot() lazily at scrape time).
	reg.MustRegister(obs.NewBinderCollector(bm, prometheus.Labels{"service": "sandbox-manager"}))
	cfg := orchestrator.ConfigFromEnv()
	networking := orchestrator.NetworkingFromEnv()
	binder := orchestrator.NewBinder(kv, p, cfg).
		WithMetrics(bm).
		WithNetworking(networking)
	capGuard, capErr := orchestrator.NewCapacityGuardFromEnv()
	if capErr != nil {
		log.Sugar().Fatalf("init sandbox capacity guard: %v", capErr)
	} else if capGuard != nil {
		binder.WithCapacityGuard(capGuard)
		log.Info("sandbox capacity guard enabled (Kubernetes REST)")
	}
	storage, storageErr := orchestrator.NewSessionStorageManagerFromEnv(ctx)
	if storageErr != nil {
		log.Sugar().Fatalf("init session storage: %v", storageErr)
	}
	if storage != nil {
		if capGuard == nil {
			log.Fatal("session storage requires Kubernetes capacity guard")
		}
		defer storage.Close()
		binder.WithSessionStorage(storage)
		log.Info("node-local session storage enabled")
	}

	go binder.RunReaper(ctx)
	eff := binder.EffectiveConfig()
	log.Sugar().Infow("session<->sandbox binder enabled",
		"lease_ttl", eff.LeaseTTL,
		"reaper_every", eff.ReaperEvery)

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Sugar().Fatalf("listen %s: %v", addr, err)
	}

	// Raise the single-message ceiling above gRPC's 4 MiB default: WriteFile
	// carries the full attachment bytes into the sandbox, which can exceed
	// 4 MiB (COCOLA_GRPC_MAX_MESSAGE_BYTES, default 64 MiB).
	maxMsg := maxMessageBytes()
	gs := grpc.NewServer(
		grpc.UnaryInterceptor(reg.UnaryServerInterceptor()),
		grpc.StreamInterceptor(reg.StreamServerInterceptor()),
		tracing.GRPCServerStatsHandler(),
		grpc.MaxRecvMsgSize(maxMsg),
		grpc.MaxSendMsgSize(maxMsg),
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
		"milestone", "M2", "addr", addr, "provider", "opensandbox")

	// Serve on a goroutine so main can stop accepting work and let in-flight RPCs
	// finish. Persistent state already lives on Session PVCs; shutdown performs
	// no archive or object-store sweep.
	serveErr := make(chan error, 1)
	go func() { serveErr <- gs.Serve(lis) }()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serveErr:
		if err != nil {
			log.Sugar().Fatalf("serve: %v", err)
		}
	case s := <-sig:
		log.Sugar().Infow("signal received; draining before exit", "signal", s.String())
		gs.GracefulStop()
		cancel()
	}
}

// defaultMaxMessageBytes is 64 MiB -- above the 32 MiB frontend upload cap,
// with headroom for base64/proto framing overhead.
const defaultMaxMessageBytes = 64 * 1024 * 1024

// maxMessageBytes resolves the configured gRPC single-message ceiling. A
// non-positive/invalid COCOLA_GRPC_MAX_MESSAGE_BYTES falls back to the default.
func maxMessageBytes() int {
	if v := os.Getenv("COCOLA_GRPC_MAX_MESSAGE_BYTES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return defaultMaxMessageBytes
}

// redisConnectTimeout is the total budget for the initial Redis dial, retries
// included. A non-positive/invalid COCOLA_REDIS_CONNECT_TIMEOUT falls back to
// 30s. Set it to 0s-equivalent (e.g. "1ms") to effectively disable retrying.
func redisConnectTimeout() time.Duration {
	if v := os.Getenv("COCOLA_REDIS_CONNECT_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	return 30 * time.Second
}

// dialRedisWithRetry dials Redis, retrying on failure until the total budget is
// exhausted. It returns the first successful client, or the last error if the
// deadline passes. Each individual dial keeps rds.New's own 5s ping timeout.
func dialRedisWithRetry(ctx context.Context, log logger.Logger, budget time.Duration) (*rds.Client, error) {
	const interval = 2 * time.Second
	deadline := time.Now().Add(budget)
	attempt := 0
	for {
		attempt++
		kv, err := rds.New(ctx, rds.ConfigFromEnv())
		if err == nil {
			if attempt > 1 {
				log.Sugar().Infow("redis reachable", "attempt", attempt)
			}
			return kv, nil
		}
		if time.Now().Add(interval).After(deadline) {
			return nil, err
		}
		log.Sugar().Infow("redis not ready; retrying",
			"attempt", attempt, "err", err,
			"retry_in", interval.String(),
			"deadline_in", time.Until(deadline).Round(time.Second).String())
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(interval):
		}
	}
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
