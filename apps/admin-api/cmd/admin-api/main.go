// admin-api is cocola's control plane. It exposes the operator surface that the
// employee-facing data plane (llm-gateway) cannot: minting and revoking the
// HS256 identity tokens employees carry, overriding per-subject token quotas,
// curating the Skill-Market catalog, and reading the audit trail.
//
// Configuration is env-driven, mirroring the gateway so a single secret set
// powers both sides of the identity handshake:
//
//	COCOLA_ADMIN_ADDR          listen address (default ":8090")
//	COCOLA_ADMIN_KEY           static admin bearer key. Empty => auth DISABLED
//	                           (dev/test), exactly like the gateway's auth.
//	COCOLA_AUTH_SECRET         HS256 signing secret SHARED with the gateway.
//	                           Empty => token minting disabled (token endpoints
//	                           400); the rest of the admin surface still works.
//	COCOLA_AUTH_ISSUER         `iss` stamped on minted tokens (default "cocola").
//	COCOLA_AUTH_TOKEN_TTL_SECS default token lifetime in seconds (default 30d).
//	COCOLA_REDIS_ADDR          host:port of the shared Redis. When set, revokes
//	                           and quota overrides are published to the keys the
//	                           gateway reads, so they take effect fleet-wide.
//	                           Empty => single-process (publish disabled).
//	COCOLA_REDIS_PASSWORD / COCOLA_REDIS_DB / COCOLA_REDIS_POOL_SIZE tune it.
//	COCOLA_METRICS_ADDR        observability listen address; empty => disabled
//	                           (default ":9093", serving /metrics and /healthz).
//
// Persistence is in-memory for M5 (process-local); the PostgreSQL backend
// lands in M7 behind the same store.Store interface — no handler change. The
// shared-Redis publish above is the propagation seam that makes the two
// gateway-read resources (revocations, quota overrides) fleet-wide today.
package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/cocola-project/cocola/apps/admin-api/internal/httpapi"
	"github.com/cocola-project/cocola/apps/admin-api/internal/redispub"
	"github.com/cocola-project/cocola/apps/admin-api/internal/service"
	"github.com/cocola-project/cocola/apps/admin-api/internal/store"
	"github.com/cocola-project/cocola/packages/go-common/config"
	"github.com/cocola-project/cocola/packages/go-common/logger"
	"github.com/cocola-project/cocola/packages/go-common/metrics"
	"github.com/cocola-project/cocola/packages/go-common/token"
	"github.com/cocola-project/cocola/packages/go-common/tracing"
)

func main() {
	log := logger.Must()
	defer func() { _ = log.Sync() }()

	// Tracing: OFF unless COCOLA_OTEL_ENABLED; otherwise only the W3C propagator
	// is installed and stop is a no-op (zero overhead, behaviour as pre-M8).
	stop, terr := tracing.Init(context.Background(), tracing.ConfigFromEnv("admin-api"))
	if terr != nil {
		log.Warn("tracing init failed: " + terr.Error())
	} else {
		defer func() { _ = stop(context.Background()) }()
	}

	addr := getenv("COCOLA_ADMIN_ADDR", ":8090")
	adminKey := config.SecretFromEnv("COCOLA_ADMIN_KEY")
	secret := config.SecretFromEnv("COCOLA_AUTH_SECRET")
	issuerName := getenv("COCOLA_AUTH_ISSUER", "cocola")
	ttl := time.Duration(getenvInt("COCOLA_AUTH_TOKEN_TTL_SECS", 30*24*3600)) * time.Second

	// Token minting is optional: without a shared secret the admin can still
	// manage quotas/skills/audit, but token endpoints return 400.
	var iss *token.Issuer
	if secret != "" {
		iss = token.NewIssuer(secret, issuerName, ttl)
		log.Sugar().Infow("token issuance enabled", "issuer", issuerName, "default_ttl", ttl)
	} else {
		log.Warn("token issuance DISABLED (no COCOLA_AUTH_SECRET) — token endpoints will 400")
	}
	if adminKey == "" {
		log.Warn("admin auth DISABLED (no COCOLA_ADMIN_KEY) — all callers are dev-admin")
	}

	// Persistence backend (M7): when COCOLA_PG_DSN is set we run the embedded
	// goose migrations and use the Postgres store; otherwise we fall back to the
	// in-memory store so a bare dev boot stays zero-dependency. The choice is
	// invisible to the service/handlers -- both satisfy store.Store.
	var st store.Store
	if dsn := os.Getenv("COCOLA_PG_DSN"); dsn != "" {
		mctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		if err := store.Migrate(mctx, dsn); err != nil {
			cancel()
			log.Sugar().Fatalf("apply migrations: %v", err)
		}
		pg, err := store.NewPostgres(mctx, dsn)
		cancel()
		if err != nil {
			log.Sugar().Fatalf("connect postgres: %v", err)
		}
		defer pg.Close()
		st = pg
		log.Sugar().Infow("persistence backend: postgres")
	} else {
		st = store.NewMemory()
		log.Warn("persistence backend: in-memory (no COCOLA_PG_DSN) — data is process-local and lost on restart")
	}

	// Optional shared-Redis publishing: when COCOLA_REDIS_ADDR is set, mirror
	// revokes + quota overrides to the keys the gateway reads so they apply
	// fleet-wide. Best-effort — a publish failure is logged, never fatal, since
	// the authoritative write already landed in the store.
	redisAddr := os.Getenv("COCOLA_REDIS_ADDR")
	if redisAddr != "" {
		cfg := redispub.Config{
			Addr:     redisAddr,
			Password: os.Getenv("COCOLA_REDIS_PASSWORD"),
			DB:       getenvInt("COCOLA_REDIS_DB", 0),
			PoolSize: getenvInt("COCOLA_REDIS_POOL_SIZE", 10),
		}
		dctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		pub, err := redispub.New(dctx, cfg)
		cancel()
		if err != nil {
			log.Sugar().Fatalf("connect shared Redis at %s: %v", redisAddr, err)
		}
		defer func() { _ = pub.Close() }()
		mirror := store.NewMirror(st, pub)
		if m, ok := mirror.(*store.Mirror); ok {
			m.OnPublishError = func(op string, e error) {
				log.Sugar().Errorw("shared-redis publish failed", "op", op, "err", e)
			}
		}
		st = mirror
		log.Sugar().Infow("shared-redis publishing enabled", "addr", redisAddr)
	} else {
		log.Warn("shared-redis publishing DISABLED (no COCOLA_REDIS_ADDR) — revokes/overrides are process-local")
	}

	svc := service.New(st, iss, time.Now)

	// Observability: a shared registry instruments every route and is exposed on
	// a dedicated port so scrapes never compete with operator traffic.
	reg := metrics.New("admin-api")
	api := httpapi.New(svc, adminKey).WithMetrics(reg)
	if metricsAddr := getenv("COCOLA_METRICS_ADDR", ":9093"); metricsAddr != "" {
		go func() {
			log.Sugar().Infow("admin-api metrics", "addr", metricsAddr)
			msrv := &http.Server{Addr: metricsAddr, Handler: reg.Mux(), ReadHeaderTimeout: 10 * time.Second}
			if err := msrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Sugar().Warnw("admin-api metrics server error", "err", err)
			}
		}()
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           api.Router(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	log.Sugar().Infow("cocola admin-api listening", "milestone", "M5", "addr", addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Sugar().Fatalf("serve: %v", err)
	}
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func getenvInt(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
