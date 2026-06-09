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
//
// Persistence is in-memory for M5 (process-local); the PostgreSQL backend
// lands in M7 behind the same store.Store interface — no handler change.
package main

import (
	"errors"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/cocola-project/cocola/apps/admin-api/internal/httpapi"
	"github.com/cocola-project/cocola/apps/admin-api/internal/service"
	"github.com/cocola-project/cocola/apps/admin-api/internal/store"
	"github.com/cocola-project/cocola/apps/admin-api/internal/token"
	"github.com/cocola-project/cocola/packages/go-common/logger"
)

func main() {
	log := logger.Must()
	defer func() { _ = log.Sync() }()

	addr := getenv("COCOLA_ADMIN_ADDR", ":8090")
	adminKey := os.Getenv("COCOLA_ADMIN_KEY")
	secret := os.Getenv("COCOLA_AUTH_SECRET")
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

	mem := store.NewMemory()
	svc := service.New(mem, iss, time.Now)
	api := httpapi.New(svc, adminKey)

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
