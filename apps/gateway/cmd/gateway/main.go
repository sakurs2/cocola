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
//	COCOLA_AUTH_SECRET      required HS256 secret
//	COCOLA_AUTH_ISSUER      expected token issuer    (default cocola)
//	COCOLA_METRICS_ADDR     observability listen address; empty => disabled
//	                        (default :9091, serving /metrics and /healthz)
//	COCOLA_PG_DSN           required Postgres DSN for conversations and chat runs
//	COCOLA_AGENT_MAX_TURNS maximum model turns in one Agent run (default 200)
//	COCOLA_AGENT_TOOL_STEP_TIMEOUT_SECS maximum wall time for one tool step (default 600)
//
// Required attachment/session object storage (ADR-0017 P1a):
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
	"fmt"
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
	"github.com/cocola-project/cocola/apps/gateway/internal/memory"
	"github.com/cocola-project/cocola/apps/gateway/internal/objstore"
	"github.com/cocola-project/cocola/apps/gateway/internal/project"
	"github.com/cocola-project/cocola/apps/gateway/internal/sandboxmgr"
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

func anyEnvConfigured(keys ...string) bool {
	for _, key := range keys {
		if strings.TrimSpace(os.Getenv(key)) != "" {
			return true
		}
	}
	return false
}

func boundedEnvInt(key string, fallback, minValue, maxValue int) (int, error) {
	value := env(key, strconv.Itoa(fallback))
	parsed, parseErr := strconv.Atoi(value)
	if parseErr != nil || parsed < minValue || parsed > maxValue {
		return 0, fmt.Errorf("invalid %s=%s", key, value)
	}
	return parsed, nil
}

func mustEnvBool(log logger.Logger, key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		log.Fatal("invalid " + key + "=" + value)
	}
	return parsed
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
	agentMaxTurns, err := boundedEnvInt("COCOLA_AGENT_MAX_TURNS", 200, 1, 1000)
	if err != nil {
		log.Fatal(err.Error())
	}
	toolTimeoutSecs, err := boundedEnvInt("COCOLA_AGENT_TOOL_STEP_TIMEOUT_SECS", 600, 30, 86400)
	if err != nil {
		log.Fatal(err.Error())
	}
	toolTimeout := time.Duration(toolTimeoutSecs) * time.Second

	client, err := agent.Dial(agentAddr)
	if err != nil {
		log.Fatal("cannot dial agent-runtime: " + err.Error())
	}
	defer func() { _ = client.Close() }()
	runtimeCtx, cancelRuntimes := context.WithTimeout(context.Background(), 5*time.Second)
	runtimes, err := client.ListRuntimes(runtimeCtx)
	cancelRuntimes()
	if err != nil {
		log.Fatal("cannot load agent runtime catalog: " + err.Error())
	}

	secret := config.SecretFromEnv("COCOLA_AUTH_SECRET")
	if secret == "" {
		log.Fatal("COCOLA_AUTH_SECRET is required")
	}
	issuerName := env("COCOLA_AUTH_ISSUER", "cocola")
	verifier := auth.NewVerifier(auth.Config{Secret: secret, Issuer: issuerName})
	// Observability: a shared metrics registry instruments the public routes and
	// is also exposed on a dedicated port so a scrape never competes with user
	// traffic. COCOLA_METRICS_ADDR="" disables both the port and instrumentation.
	reg := metrics.New("gateway")
	api := httpapi.New(client, verifier, log).WithMetrics(reg).WithAgentRuntimes(runtimes)

	// Per-user sandbox token issuer (P0 identity fix). The gateway mints a fresh
	// cocola token per chat turn from the VERIFIED identity (sub=user, ten=tenant)
	// and forwards it to agent-runtime, which injects it into the sandbox as
	// ANTHROPIC_AUTH_TOKEN -- so the in-sandbox brain calls the llm-gateway AS THE
	// USER and per-user quota / usage / revocation actually bind. Same secret+issuer the
	// llm-gateway verifies with (COCOLA_AUTH_SECRET / COCOLA_AUTH_ISSUER), so the
	// minted token verifies offline downstream. There is no shared or anonymous
	// fallback identity.
	ttl := 7 * 24 * time.Hour
	if v := os.Getenv("COCOLA_SANDBOX_TOKEN_TTL_SECONDS"); v != "" {
		if n, perr := strconv.Atoi(v); perr == nil && n > 0 {
			ttl = time.Duration(n) * time.Second
		} else {
			log.Fatal("invalid COCOLA_SANDBOX_TOKEN_TTL_SECONDS=" + v)
		}
	}
	issuer := token.NewIssuer(secret, issuerName, ttl)
	api = api.WithSandboxTokenIssuer(issuer, ttl)
	log.Info("per-user sandbox token issuer enabled (ttl " + ttl.String() + ")")

	// Preview Proxy: optionally dial sandbox-manager so the gateway can resolve
	// a session's in-sandbox dev-server port to a reachable URL and reverse-proxy
	// it. Disabled (route returns 501) when COCOLA_SANDBOX_ADDR is unset, so
	// environments without a directly-reachable sandbox-manager stay dark.
	if sandboxAddr := strings.TrimSpace(os.Getenv("COCOLA_SANDBOX_ADDR")); sandboxAddr != "" {
		sbClient, sberr := sandboxmgr.Dial(sandboxAddr)
		if sberr != nil {
			log.Fatal("cannot dial sandbox-manager: " + sberr.Error())
		}
		defer func() { _ = sbClient.Close() }()
		api = api.WithSandboxResolver(sbClient)
		log.Info("preview proxy enabled (sandbox-manager: " + sandboxAddr + ")")
	} else {
		log.Info("preview proxy disabled (COCOLA_SANDBOX_ADDR unset)")
	}

	// Attachment source of truth. MinIO is required so large attachments and
	// restart behavior do not depend on an implicit inline-only mode.
	oc := objstore.ConfigFromEnv()
	objectStore, oerr := objstore.New(oc)
	if oerr != nil {
		log.Fatal("attachment object store: " + oerr.Error())
	}
	healthCtx, healthCancel := context.WithTimeout(context.Background(), 5*time.Second)
	healthErr := objectStore.Health(healthCtx)
	healthCancel()
	if healthErr != nil {
		log.Fatal("attachment object store health check failed: " + healthErr.Error())
	}
	threshold := int64(httpapi.DefaultInlineMaxBytes)
	if v := os.Getenv("COCOLA_ATTACHMENT_INLINE_MAX_BYTES"); v != "" {
		if n, perr := strconv.ParseInt(v, 10, 64); perr == nil && n > 0 {
			threshold = n
		} else {
			log.Fatal("invalid COCOLA_ATTACHMENT_INLINE_MAX_BYTES=" + v)
		}
	}
	api = api.WithObjStore(objectStore, threshold)
	log.Info("attachment object store enabled (bucket " + oc.Bucket +
		", inline<=" + strconv.FormatInt(threshold, 10) + "B)")
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
		projectStore, projectStoreErr := project.NewPostgres(context.Background(), dsn)
		if projectStoreErr != nil {
			log.Fatal("project store connect failed: " + projectStoreErr.Error())
		}
		defer projectStore.Close()
		projectService, projectErr := project.New(projectStore, project.Config{
			SecretKey:               config.SecretFromEnv("COCOLA_SCM_SECRET_KEY"),
			PublicOrigins:           strings.TrimSpace(os.Getenv("COCOLA_PUBLIC_ORIGINS")),
			MaxRepositoryMB:         int64(mustBoundedEnvInt(log, "COCOLA_PROJECT_MAX_REPOSITORY_MB", 512, 1, 8192)),
			DisableLocalProjects:    !mustEnvBool(log, "COCOLA_FEATURE_LOCAL_PROJECTS", true),
			DisableGitHubConnector:  !mustEnvBool(log, "COCOLA_FEATURE_GITHUB_MANIFEST_CONNECTOR", true),
			DisableGitHubAgentWrite: !mustEnvBool(log, "COCOLA_FEATURE_GITHUB_AGENT_WRITE", true),
		})
		if projectErr != nil {
			log.Fatal("Project configuration failed: " + projectErr.Error())
		}
		api = api.WithProjects(projectService)
		log.Info("Projects enabled (local projects=" + strconv.FormatBool(projectService.LocalProjectsEnabled()) +
			", github connector=" + strconv.FormatBool(projectService.GitHubConnectorEnabled()) +
			", github agent write=" + strconv.FormatBool(projectService.GitHubAgentWriteEnabled()) + ")")
		memoryService, memoryErr := memory.New(context.Background(), dsn, memory.Config{
			OpenVikingURL:        env("COCOLA_OPENVIKING_URL", "http://127.0.0.1:1933"),
			OpenVikingRootAPIKey: config.SecretFromEnv("COCOLA_OPENVIKING_ROOT_API_KEY"),
			EmbeddingDimension:   mustBoundedEnvInt(log, "COCOLA_MEMORY_EMBEDDING_DIMENSION", 1024, 1, 100000),
			Metrics:              reg.Registerer(),
		}, logger.WithService(log, "gateway", "memory"))
		if memoryErr != nil {
			log.Fatal("memory service connect failed: " + memoryErr.Error())
		}
		defer memoryService.Close()
		api = api.WithMemory(memoryService)
		log.Info("OpenViking memory integration loaded (globally disabled until configured)")
		runStore, runErr := chatrun.NewPostgres(context.Background(), dsn)
		if runErr != nil {
			log.Fatal("chat run store connect failed: " + runErr.Error())
		}
		defer runStore.Close()
		api = api.WithChatRuns(runStore, httpapi.RunConfig{
			AgentMaxTurns: int32(agentMaxTurns), ToolTimeout: toolTimeout,
		})
		if err := api.InterruptStaleRuns(context.Background()); err != nil {
			log.Fatal("stale chat run recovery failed: " + err.Error())
		}
		log.Info("single-gateway durable chat enabled (postgres)")
		traceStore, terr := traceevents.NewPostgres(context.Background(), dsn)
		if terr != nil {
			log.Fatal("conversation trace store connect failed: " + terr.Error())
		}
		defer traceStore.Close()
		asyncTraceStore := traceevents.NewAsyncStore(traceStore, 2048, func(err error) {
			log.Warn("conversation trace flush failed: " + err.Error())
		})
		defer asyncTraceStore.Close()
		api = api.WithTraceStore(asyncTraceStore)
		log.Info("conversation audit and traces enabled (postgres)")
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

func mustBoundedEnvInt(
	log logger.Logger,
	key string,
	fallback int,
	minValue int,
	maxValue int,
) int {
	value, err := boundedEnvInt(key, fallback, minValue, maxValue)
	if err != nil {
		log.Fatal(err.Error())
	}
	return value
}
