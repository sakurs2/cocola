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
//	COCOLA_AUTH_ALLOW_ANON  "1" to accept blank tokens as dev-user (dev only)
//	COCOLA_METRICS_ADDR     observability listen address; empty => disabled
//	                        (default :9091, serving /metrics and /healthz)
package main

import (
	"context"
	"net/http"
	"os"

	"github.com/cocola-project/cocola/apps/gateway/internal/agent"
	"github.com/cocola-project/cocola/apps/gateway/internal/auth"
	"github.com/cocola-project/cocola/apps/gateway/internal/httpapi"
	"github.com/cocola-project/cocola/packages/go-common/config"
	"github.com/cocola-project/cocola/packages/go-common/logger"
	"github.com/cocola-project/cocola/packages/go-common/metrics"
	"github.com/cocola-project/cocola/packages/go-common/tracing"
)

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	log := logger.Must()
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
		Secret:         config.SecretFromEnv("COCOLA_AUTH_SECRET"),
		Issuer:         env("COCOLA_AUTH_ISSUER", "cocola"),
		AllowAnonymous: os.Getenv("COCOLA_AUTH_ALLOW_ANON") == "1",
	})
	// Observability: a shared metrics registry instruments the public routes and
	// is also exposed on a dedicated port so a scrape never competes with user
	// traffic. COCOLA_METRICS_ADDR="" disables both the port and instrumentation.
	reg := metrics.New("gateway")
	api := httpapi.New(client, verifier, log).WithMetrics(reg)
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
