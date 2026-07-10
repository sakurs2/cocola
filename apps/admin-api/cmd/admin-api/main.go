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
//	COCOLA_BOOTSTRAP_ADMIN_EMAIL
//	COCOLA_BOOTSTRAP_ADMIN_USERNAME
//	COCOLA_BOOTSTRAP_ADMIN_PASSWORD or COCOLA_BOOTSTRAP_ADMIN_PASSWORD_HASH
//	                           seed the first Auth.js web admin user. Existing
//	                           users are left unchanged unless
//	                           COCOLA_BOOTSTRAP_ADMIN_RESET=true.
//	COCOLA_BOOTSTRAP_ADMIN_PRINT
//	                           true => print dev bootstrap credentials. Use
//	                           only for local dev; never in production.
//	COCOLA_SCHEDULER_ENABLED   run admin-created system scheduled tasks
//	                           (default true).
//	COCOLA_AGENT_ADDR          agent-runtime gRPC address for scheduled tasks
//	                           (default 127.0.0.1:50061).
//	COCOLA_GATEWAY_URL         llm-gateway URL for user scheduled tasks
//	                           (default http://127.0.0.1:8080).
//	COCOLA_SCHEDULER_POLL_SECS due-task scan cadence (default 60).
//	COCOLA_SCHEDULER_RUN_TIMEOUT_SECS
//	                           max runtime per scheduled task run (default 3600).
//	COCOLA_SCHEDULER_HEARTBEAT_SECS
//	                           running task lease heartbeat cadence (default 30).
//	COCOLA_SCHEDULER_LEASE_TIMEOUT_SECS
//	                           stale running task timeout (default 300).
//	COCOLA_SCHEDULER_MIN_INTERVAL_SECS
//	                           minimum schedule interval accepted (default 3600).
//	COCOLA_CONFIG_SECRET_KEY    encrypts admin-managed runtime config secrets
//	                           (MCP URL/env/header). Empty falls back to
//	                           COCOLA_MODEL_SECRET_KEY for compatibility.
//	COCOLA_SANDBOX_ADDR         sandbox-manager gRPC address used for MCP checks.
//	COCOLA_SANDBOX_IMAGE        runtime image used by temporary MCP checks.
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
	"strings"
	"time"

	"github.com/cocola-project/cocola/apps/admin-api/internal/httpapi"
	"github.com/cocola-project/cocola/apps/admin-api/internal/objstore"
	"github.com/cocola-project/cocola/apps/admin-api/internal/redispub"
	"github.com/cocola-project/cocola/apps/admin-api/internal/service"
	"github.com/cocola-project/cocola/apps/admin-api/internal/store"
	"github.com/cocola-project/cocola/packages/go-common/config"
	"github.com/cocola-project/cocola/packages/go-common/logger"
	"github.com/cocola-project/cocola/packages/go-common/metrics"
	rds "github.com/cocola-project/cocola/packages/go-common/redis"
	"github.com/cocola-project/cocola/packages/go-common/token"
	"github.com/cocola-project/cocola/packages/go-common/tracing"
)

func main() {
	log := logger.WithService(logger.Must(), "admin-api", "admin-api")
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
	var runtimeKV *rds.Client
	var warmPoolWriter service.WarmPoolConfigWriter
	var userEventBroker service.UserEventBroker = service.NewMemoryUserEventBroker()
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
		userEventBroker = pub
		warmPoolWriter = pub
		mirror := store.NewMirror(st, pub)
		if m, ok := mirror.(*store.Mirror); ok {
			m.OnPublishError = func(op string, e error) {
				log.Sugar().Errorw("shared-redis publish failed", "op", op, "err", e)
			}
		}
		st = mirror
		log.Sugar().Infow("shared-redis publishing enabled", "addr", redisAddr)

		kvctx, kvcancel := context.WithTimeout(context.Background(), 5*time.Second)
		runtimeKV, err = rds.New(kvctx, rds.Config{
			Addr:     redisAddr,
			Password: os.Getenv("COCOLA_REDIS_PASSWORD"),
			DB:       getenvInt("COCOLA_REDIS_DB", 0),
			PoolSize: getenvInt("COCOLA_REDIS_POOL_SIZE", 10),
		})
		kvcancel()
		if err != nil {
			log.Sugar().Warnw("sandbox runtime monitor disabled: shared Redis unavailable", "err", err)
		} else {
			defer func() { _ = runtimeKV.Close() }()
		}
	} else {
		log.Warn("shared-redis publishing DISABLED (no COCOLA_REDIS_ADDR) — revokes/overrides are process-local")
		log.Warn("user event bus using in-memory broker (no COCOLA_REDIS_ADDR) — realtime events are single-process")
	}

	svc := service.New(st, iss, time.Now).
		WithUserEventBroker(userEventBroker).
		WithWarmPoolConfigWriter(warmPoolWriter).
		WithModelSecretKey(config.SecretFromEnv("COCOLA_MODEL_SECRET_KEY")).
		WithConfigSecretKey(config.SecretFromEnv("COCOLA_CONFIG_SECRET_KEY")).
		WithMinScheduleInterval(time.Duration(getenvInt("COCOLA_SCHEDULER_MIN_INTERVAL_SECS", 3600)) * time.Second)
	migrationCtx, migrationCancel := context.WithTimeout(context.Background(), 30*time.Second)
	if err := svc.MigrateMCPRemoteURLs(migrationCtx); err != nil {
		migrationCancel()
		log.Sugar().Fatalf("secure legacy MCP URLs: %v", err)
	}
	migrationCancel()
	if oc := objstore.ConfigFromEnv(); oc.Enabled() {
		skillStore, err := objstore.New(oc)
		if err != nil {
			log.Sugar().Warnw("skill bundle store disabled", "err", err)
		} else {
			hctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			herr := skillStore.Health(hctx)
			cancel()
			if herr != nil {
				log.Sugar().Warnw("skill bundle store health failed; disabling bundle uploads", "err", herr)
			} else {
				svc.WithSkillBundleStore(skillStore)
				log.Sugar().Infow("skill bundle store enabled", "endpoint", oc.Endpoint, "bucket", oc.Bucket)
			}
		}
	} else {
		log.Warn("skill bundle store disabled (no COCOLA_MINIO_ENDPOINT/BUCKET) — imported skills keep metadata only")
	}
	if runtimeKV != nil {
		runtimeMgr, err := service.NewSandboxRuntimeManagerFromEnv(runtimeKV)
		if err != nil {
			log.Sugar().Warnw("sandbox runtime monitor disabled", "err", err)
		} else if runtimeMgr != nil {
			if rm, ok := runtimeMgr.(*service.RedisSandboxRuntimeManager); ok {
				runtimeMgr = svc.AttachSandboxRuntimeUsernames(rm)
			}
			svc.WithSandboxRuntimeManager(runtimeMgr)
			log.Info("sandbox runtime monitor enabled (Redis metadata)")
		}
	}
	if warmPoolWriter != nil {
		// Reconcile the shared warm-pool config key with the current DB/env state
		// at boot so sandbox-manager sees the admin-configured sizing even if no
		// update has happened since the last restart.
		bctx, bcancel := context.WithTimeout(context.Background(), 5*time.Second)
		svc.PublishWarmPoolConfig(bctx)
		bcancel()
	}
	if email := os.Getenv("COCOLA_BOOTSTRAP_ADMIN_EMAIL"); email != "" {
		username := os.Getenv("COCOLA_BOOTSTRAP_ADMIN_USERNAME")
		password := config.SecretFromEnv("COCOLA_BOOTSTRAP_ADMIN_PASSWORD")
		passwordHash := config.SecretFromEnv("COCOLA_BOOTSTRAP_ADMIN_PASSWORD_HASH")
		if err := svc.BootstrapAdmin(context.Background(), service.BootstrapAdminInput{
			Username:     username,
			Email:        email,
			Password:     password,
			PasswordHash: passwordHash,
			Reset:        getenvBool("COCOLA_BOOTSTRAP_ADMIN_RESET", false),
			Actor:        "bootstrap",
		}); err != nil {
			log.Sugar().Fatalf("bootstrap admin user: %v", err)
		}
		log.Sugar().Infow("bootstrap admin user ensured", "email", email)
		if getenvBool("COCOLA_BOOTSTRAP_ADMIN_PRINT", false) && password != "" {
			if username == "" {
				username = email
				if at := strings.IndexByte(username, '@'); at > 0 {
					username = username[:at]
				}
			}
			log.Sugar().Warnw("dev bootstrap admin credentials", "username", username, "email", email, "password", password)
		}
	}
	nodeMgr, err := service.NewSandboxNodeManagerFromEnv()
	if err != nil {
		log.Sugar().Warnw("sandbox node manager disabled", "err", err)
	} else if nodeMgr != nil {
		svc.WithSandboxNodeManager(nodeMgr)
		log.Info("sandbox node manager enabled (Kubernetes REST)")
	} else {
		log.Warn("sandbox node manager DISABLED (no Kubernetes config)")
	}
	if !envBoolFalse(os.Getenv("COCOLA_SCHEDULER_ENABLED")) {
		if err := svc.StartScheduler(context.Background(), service.SchedulerConfig{
			Enabled:    true,
			AgentAddr:  getenv("COCOLA_AGENT_ADDR", "127.0.0.1:50061"),
			GatewayURL: getenv("COCOLA_GATEWAY_URL", "http://127.0.0.1:8080"),
			WorkerID:   getenv("COCOLA_SCHEDULER_WORKER_ID", "admin-api"),
			PollEvery:  time.Duration(getenvInt("COCOLA_SCHEDULER_POLL_SECS", 60)) * time.Second,
			RunTimeout: time.Duration(getenvInt("COCOLA_SCHEDULER_RUN_TIMEOUT_SECS", 3600)) * time.Second,
			HeartbeatEvery: time.Duration(
				getenvInt("COCOLA_SCHEDULER_HEARTBEAT_SECS", 30),
			) * time.Second,
			LeaseTimeout: time.Duration(
				getenvInt("COCOLA_SCHEDULER_LEASE_TIMEOUT_SECS", 300),
			) * time.Second,
		}); err != nil {
			log.Sugar().Warnw("scheduled task worker disabled", "err", err)
		} else {
			log.Info("scheduled task worker enabled")
		}
	} else {
		log.Warn("scheduled task worker DISABLED")
	}

	// Observability: a shared registry instruments every route and is exposed on
	// a dedicated port so scrapes never compete with operator traffic.
	reg := metrics.New("admin-api")
	api := httpapi.New(svc, adminKey).WithRuntimeAuth(secret, issuerName).WithMetrics(reg)
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

func getenvBool(k string, def bool) bool {
	if v := os.Getenv(k); v != "" {
		switch v {
		case "1", "true", "TRUE", "True", "yes", "YES", "on", "ON":
			return true
		case "0", "false", "FALSE", "False", "no", "NO", "off", "OFF":
			return false
		}
	}
	return def
}

func envBoolFalse(v string) bool {
	switch v {
	case "0", "false", "FALSE", "False", "no", "NO", "off", "OFF":
		return true
	default:
		return false
	}
}
