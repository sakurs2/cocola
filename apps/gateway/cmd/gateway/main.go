// gateway is the public-facing BFF. Responsibilities:
//   - HTTP termination from the web client
//   - Auth: verifies cocola-signed HS256 tokens (shared go-common/token codec)
//   - Routing prompts to agent-runtime over gRPC
//   - Streaming agent events back to the browser over SSE
//
// Configuration is env-driven (M3 will move this behind go-common/config):
//
//	COCOLA_GATEWAY_ADDR     listen address           (default :8080)
//	COCOLA_AGENT_ADDR       agent-runtime gRPC addr  (default 127.0.0.1:50061)
//	COCOLA_AUTH_SECRET      HS256 secret; empty => auth disabled (dev only)
//	COCOLA_AUTH_ISSUER      expected token issuer    (default cocola)
//	COCOLA_AUTH_ALLOW_ANON  blank tokens => dev-user; explicit dev-only opt-in
//	COCOLA_METRICS_ADDR     observability listen address; empty => disabled
//	                        (default :9091, serving /metrics and /healthz)
//	COCOLA_PG_DSN           required Postgres DSN for conversations and chat runs
//	COCOLA_AGENT_RUN_TIMEOUT_SECS maximum wall time for one Agent run (default 3600)
//
// Attachment object storage (ADR-0017 P1a); unset endpoint/bucket => inline-only:
//
//	COCOLA_MINIO_ENDPOINT              S3 host:port (no scheme)
//	COCOLA_MINIO_ACCESS_KEY            access key id
//	COCOLA_MINIO_SECRET_KEY[_FILE]     secret key (or _FILE indirection)
//	COCOLA_MINIO_BUCKET                bucket for attachments
//	COCOLA_MINIO_USE_SSL               "1" to use HTTPS
//	COCOLA_ATTACHMENT_INLINE_MAX_BYTES small/large split (default 16MiB)
package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/cocola-project/cocola/apps/gateway/internal/agent"
	"github.com/cocola-project/cocola/apps/gateway/internal/auth"
	"github.com/cocola-project/cocola/apps/gateway/internal/chatrun"
	"github.com/cocola-project/cocola/apps/gateway/internal/convo"
	"github.com/cocola-project/cocola/apps/gateway/internal/httpapi"
	"github.com/cocola-project/cocola/apps/gateway/internal/objstore"
	traceevents "github.com/cocola-project/cocola/apps/gateway/internal/traceevent"
	"github.com/cocola-project/cocola/packages/go-common/config"
	"github.com/cocola-project/cocola/packages/go-common/logger"
	"github.com/cocola-project/cocola/packages/go-common/metrics"
	"github.com/cocola-project/cocola/packages/go-common/token"
	"github.com/cocola-project/cocola/packages/go-common/tracing"
)

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	log := logger.WithService(logger.Must(), "gateway", "gateway")
	defer func() { _ = log.Sync() }()

	// Tracing: OFF unless COCOLA_OTEL_ENABLED. When off, only the W3C propagator
	// is installed (inbound traceparent still honoured for log correlation) and
	// stop is a no-op — zero overhead, behaviour identical to pre-M8.
	stop, terr := tracing.Init(context.Background(), tracing.ConfigFromEnv("gateway"))
	if terr != nil {
		log.Warn("tracing init failed: " + terr.Error())
	} else {
		defer func() { _ = stop(context.Background()) }()
	}

	addr := env("COCOLA_GATEWAY_ADDR", ":8080")
	agentAddr := env("COCOLA_AGENT_ADDR", "127.0.0.1:50061")
	runTimeout := 60 * time.Minute
	if value := os.Getenv("COCOLA_AGENT_RUN_TIMEOUT_SECS"); value != "" {
		seconds, parseErr := strconv.Atoi(value)
		if parseErr != nil || seconds <= 0 {
			log.Fatal("invalid COCOLA_AGENT_RUN_TIMEOUT_SECS=" + value)
		}
		runTimeout = time.Duration(seconds) * time.Second
	}

	client, err := agent.Dial(agentAddr)
	if err != nil {
		log.Fatal("cannot dial agent-runtime: " + err.Error())
	}
	defer func() { _ = client.Close() }()

	verifier := auth.NewVerifier(auth.Config{
		Secret: config.SecretFromEnv("COCOLA_AUTH_SECRET"),
		Issuer: env("COCOLA_AUTH_ISSUER", "cocola"),
		// A configured secret requires credentials unless development explicitly
		// opts into anonymous access.
		AllowAnonymous: os.Getenv("COCOLA_AUTH_ALLOW_ANON") == "1" ||
			strings.EqualFold(os.Getenv("COCOLA_AUTH_ALLOW_ANON"), "true"),
	})
	// Observability: a shared metrics registry instruments the public routes and
	// is also exposed on a dedicated port so a scrape never competes with user
	// traffic. COCOLA_METRICS_ADDR="" disables both the port and instrumentation.
	reg := metrics.New("gateway")
	api := httpapi.New(client, verifier, log).WithMetrics(reg)

	// Per-user sandbox token issuer (P0 identity fix). The gateway mints a fresh
	// cocola token per chat turn from the VERIFIED identity (sub=user, ten=tenant)
	// and forwards it to agent-runtime, which injects it into the sandbox as
	// ANTHROPIC_AUTH_TOKEN -- so the in-sandbox brain calls the llm-gateway AS THE
	// USER and per-user quota / usage / revocation actually bind, instead of the
	// static cluster-wide COCOLA_SANDBOX_LLM_TOKEN. Same secret+issuer the
	// llm-gateway verifies with (COCOLA_AUTH_SECRET / COCOLA_AUTH_ISSUER), so the
	// minted token verifies offline downstream. When the secret is empty (dev,
	// auth off) the issuer stays nil and the runtime keeps its baked token.
	if secret := config.SecretFromEnv("COCOLA_AUTH_SECRET"); secret != "" {
		minimumTTL := runTimeout + 15*time.Minute
		ttl := minimumTTL
		if v := os.Getenv("COCOLA_SANDBOX_TOKEN_TTL_SECONDS"); v != "" {
			if n, perr := strconv.Atoi(v); perr == nil && n > 0 {
				ttl = time.Duration(n) * time.Second
			} else {
				log.Fatal("invalid COCOLA_SANDBOX_TOKEN_TTL_SECONDS=" + v)
			}
		}
		if ttl < minimumTTL {
			log.Fatal("COCOLA_SANDBOX_TOKEN_TTL_SECONDS must be at least run timeout + 900 seconds")
		}
		issuer := token.NewIssuer(secret, env("COCOLA_AUTH_ISSUER", "cocola"), ttl)
		api = api.WithSandboxTokenIssuer(issuer, ttl)
		log.Info("per-user sandbox token issuer enabled (ttl " + ttl.String() + ")")
	}

	// Attachment source-of-truth object store (ADR-0017 P1a). Wired only when
	// COCOLA_MINIO_ENDPOINT+BUCKET are set; otherwise the gateway stays on the
	// P0 inline-only path so the feature is dark until MinIO is provisioned.
	if oc := objstore.ConfigFromEnv(); oc.Enabled() {
		store, oerr := objstore.New(oc)
		if oerr != nil {
			log.Warn("attachment object store disabled: " + oerr.Error())
		} else {
			threshold := int64(httpapi.DefaultInlineMaxBytes)
			if v := os.Getenv("COCOLA_ATTACHMENT_INLINE_MAX_BYTES"); v != "" {
				if n, perr := strconv.ParseInt(v, 10, 64); perr == nil && n > 0 {
					threshold = n
				} else {
					log.Warn("ignoring invalid COCOLA_ATTACHMENT_INLINE_MAX_BYTES=" + v)
				}
			}
			api = api.WithObjStore(store, threshold)
			log.Info("attachment object store enabled (bucket " + oc.Bucket +
				", inline<=" + strconv.FormatInt(threshold, 10) + "B)")
		}
	}
	// Durable chat has one production storage path. Starting without PostgreSQL
	// would make restart semantics depend on process memory.
	dsn := strings.TrimSpace(os.Getenv("COCOLA_PG_DSN"))
	if dsn == "" {
		log.Fatal("COCOLA_PG_DSN is required")
	}
	if err := convo.Migrate(context.Background(), dsn); err != nil {
		log.Fatal("conversation migration failed: " + err.Error())
	} else if cs, cerr := convo.NewPostgres(context.Background(), dsn); cerr != nil {
		log.Fatal("conversation store connect failed: " + cerr.Error())
	} else {
		api = api.WithConvoStore(cs)
		defer cs.Close()
		runStore, runErr := chatrun.NewPostgres(context.Background(), dsn)
		if runErr != nil {
			log.Fatal("chat run store connect failed: " + runErr.Error())
		}
		defer runStore.Close()
		api = api.WithChatRuns(runStore, httpapi.RunConfig{RunTimeout: runTimeout})
		if err := api.InterruptStaleRuns(context.Background()); err != nil {
			log.Fatal("stale chat run recovery failed: " + err.Error())
		}
		log.Info("single-gateway durable chat enabled (postgres)")
		if traceStore, terr := traceevents.NewPostgres(context.Background(), dsn); terr != nil {
			log.Warn("conversation traces disabled (connect failed): " + terr.Error())
		} else {
			defer traceStore.Close()
			asyncTraceStore := traceevents.NewAsyncStore(traceStore, 2048, func(err error) {
				log.Warn("conversation trace flush failed: " + err.Error())
			})
			defer asyncTraceStore.Close()
			api = api.WithTraceStore(asyncTraceStore)
			log.Info("conversation audit and traces enabled (postgres)")
		}
	}

	var metricsServer *http.Server
	if metricsAddr := env("COCOLA_METRICS_ADDR", ":9091"); metricsAddr != "" {
		metricsServer = &http.Server{Addr: metricsAddr, Handler: reg.Mux()}
		go func() {
			log.Info("cocola gateway metrics on " + metricsAddr + " (/metrics, /healthz)")
			if err := metricsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Warn("gateway metrics server error: " + err.Error())
			}
		}()
	}

	log.Info("cocola gateway listening on " + addr + " (agent-runtime: " + agentAddr + ")")
	srv := &http.Server{Addr: addr, Handler: api.Handler()}
	rootCtx, stopSignals := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stopSignals()
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.ListenAndServe() }()
	select {
	case err := <-serveErr:
		if err != nil && err != http.ErrServerClosed {
			log.Fatal("gateway server error: " + err.Error())
		}
		return
	case <-rootCtx.Done():
	}
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelShutdown()
	if err := api.ShutdownRuns(shutdownCtx); err != nil {
		log.Warn("chat run shutdown failed: " + err.Error())
	}
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Warn("gateway HTTP shutdown failed: " + err.Error())
	}
	if metricsServer != nil {
		_ = metricsServer.Shutdown(shutdownCtx)
	}
}
