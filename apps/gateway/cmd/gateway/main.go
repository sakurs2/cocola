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
//	COCOLA_AUTH_ALLOW_ANON  blank tokens => dev-user; default ON, set "0" to
//	                        reject anonymous callers (dev only)
//	COCOLA_METRICS_ADDR     observability listen address; empty => disabled
//	                        (default :9091, serving /metrics and /healthz)
//	COCOLA_PG_DSN           Postgres DSN; enables conversation persistence
//	                        (sidebar list + history). Unset => persistence dark.
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
	"strconv"
	"time"

	"github.com/cocola-project/cocola/apps/gateway/internal/agent"
	"github.com/cocola-project/cocola/apps/gateway/internal/auth"
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

	client, err := agent.Dial(agentAddr)
	if err != nil {
		log.Fatal("cannot dial agent-runtime: " + err.Error())
	}
	defer func() { _ = client.Close() }()

	verifier := auth.NewVerifier(auth.Config{
		Secret: config.SecretFromEnv("COCOLA_AUTH_SECRET"),
		Issuer: env("COCOLA_AUTH_ISSUER", "cocola"),
		// Auth identity is a LATER concern: today the product UI sends no token
		// and every caller should resolve to the shared dev-user. So anonymous
		// access defaults ON and is only disabled by an explicit
		// COCOLA_AUTH_ALLOW_ANON=0. (It still requires a secret to matter at all;
		// with no secret auth is off entirely.) This keeps a blank-token read
		// working no matter how the gateway is launched (make up, GoLand, bare
		// go run) rather than only when run-stack.sh exports the flag.
		AllowAnonymous: os.Getenv("COCOLA_AUTH_ALLOW_ANON") != "0",
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
		ttl := 60 * time.Minute
		if v := os.Getenv("COCOLA_SANDBOX_TOKEN_TTL_SECONDS"); v != "" {
			if n, perr := strconv.Atoi(v); perr == nil && n > 0 {
				ttl = time.Duration(n) * time.Second
			} else {
				log.Warn("ignoring invalid COCOLA_SANDBOX_TOKEN_TTL_SECONDS=" + v)
			}
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
	// Conversation persistence (route A UI-message mirror). Wired only when
	// COCOLA_PG_DSN is set; otherwise persistence stays dark (chat still streams,
	// the list/history endpoints return empty). We run migrations here too so a
	// gateway+PG boot is self-sufficient; goose is idempotent and coexists with
	// admin-api applying the same embedded schema.
	if dsn := os.Getenv("COCOLA_PG_DSN"); dsn != "" {
		if err := convo.Migrate(context.Background(), dsn); err != nil {
			log.Warn("conversation persistence disabled (migrate failed): " + err.Error())
		} else if cs, cerr := convo.NewPostgres(context.Background(), dsn); cerr != nil {
			log.Warn("conversation persistence disabled (connect failed): " + cerr.Error())
		} else {
			api = api.WithConvoStore(cs)
			defer cs.Close()
			log.Info("conversation persistence enabled (postgres)")
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
	}

	if metricsAddr := env("COCOLA_METRICS_ADDR", ":9091"); metricsAddr != "" {
		go func() {
			log.Info("cocola gateway metrics on " + metricsAddr + " (/metrics, /healthz)")
			msrv := &http.Server{Addr: metricsAddr, Handler: reg.Mux()}
			if err := msrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Warn("gateway metrics server error: " + err.Error())
			}
		}()
	}

	log.Info("cocola gateway listening on " + addr + " (agent-runtime: " + agentAddr + ")")
	srv := &http.Server{Addr: addr, Handler: api.Handler()}
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal("gateway server error: " + err.Error())
	}
}
