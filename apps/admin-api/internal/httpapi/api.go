// Package httpapi exposes the admin service over a chi router. It owns three
// cross-cutting concerns and nothing else: a JSON error envelope aligned with
// go-common/errors codes, an admin-auth middleware (a static bearer admin key),
// and request decoding/encoding. All business logic lives in internal/service.
//
// Why a static admin key rather than reusing the employee JWTs? The admin
// surface is operated by a small set of operators, not employees; a single
// shared admin key kept in the deployment secret is the simplest thing that is
// correct for an internal tool. Per-operator admin identities + RBAC are a
// follow-up (see ADR-0006). When no admin key is configured, auth is disabled
// (dev/test convenience), mirroring the gateway's COCOLA_AUTH_SECRET behavior.
package httpapi

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/cocola-project/cocola/apps/admin-api/internal/service"
	"github.com/cocola-project/cocola/apps/admin-api/internal/store"
	"github.com/cocola-project/cocola/packages/go-common/metrics"
	"github.com/cocola-project/cocola/packages/go-common/tracing"
)

// API holds the dependencies the handlers need.
type API struct {
	svc      *service.Admin
	adminKey string            // when "", admin auth is disabled
	metrics  *metrics.Registry // optional; nil => no instrumentation (tests)
}

// New builds the API. adminKey "" disables auth (dev/test).
func New(svc *service.Admin, adminKey string) *API {
	return &API{svc: svc, adminKey: adminKey}
}

// WithMetrics enables RED instrumentation on every route. The label is chi's
// matched route pattern (e.g. "/admin/tokens/{id}"), resolved post-routing, so
// path params never inflate label cardinality. nil leaves the API
// uninstrumented (the default in unit tests).
func (a *API) WithMetrics(reg *metrics.Registry) *API { a.metrics = reg; return a }

// Router returns the fully-wired chi router.
func (a *API) Router() http.Handler {
	r := chi.NewRouter()

	// RED instrumentation first so it spans auth + handler. The route label is
	// chi's matched pattern, available after routing via RouteContext.
	if a.metrics != nil {
		r.Use(func(next http.Handler) http.Handler {
			return a.metrics.HTTPHandler(func(req *http.Request) string {
				if rc := chi.RouteContext(req.Context()); rc != nil {
					return req.Method + " " + rc.RoutePattern()
				}
				return ""
			}, next)
		})
	}

	r.Get("/healthz", a.health)
	r.Post("/auth/login", a.login)

	r.Route("/admin", func(r chi.Router) {
		r.Use(a.requireAdmin)

		r.Route("/users", func(r chi.Router) {
			r.Post("/", a.createAuthUser)
			r.Get("/", a.listAuthUsers)
			r.Get("/lookup", a.lookupAuthUser)
			r.Patch("/{id}", a.updateAuthUser)
			r.Post("/{id}/password", a.resetAuthUserPassword)
			r.Delete("/{id}", a.deleteAuthUser)
		})

		r.Post("/runtime-token", a.issueRuntimeToken)

		r.Route("/tokens", func(r chi.Router) {
			r.Post("/", a.issueToken)
			r.Get("/", a.listTokens)
			r.Get("/revoked", a.listRevoked)
			r.Delete("/{id}", a.revokeToken)
		})

		r.Route("/quotas", func(r chi.Router) {
			r.Get("/", a.listQuotas)
			r.Put("/", a.setQuota)
			r.Delete("/{scope}/{subject}", a.deleteQuota)
		})

		r.Route("/skills", func(r chi.Router) {
			r.Post("/", a.createSkill)
			r.Get("/", a.listSkills)
			r.Get("/{id}", a.getSkill)
			r.Post("/{id}/enable", a.enableSkill)
			r.Post("/{id}/disable", a.disableSkill)
			r.Delete("/{id}", a.deleteSkill)
		})

		r.Route("/sandbox-nodes", func(r chi.Router) {
			r.Get("/", a.listSandboxNodes)
			r.Get("/join-command", a.sandboxNodeJoinCommand)
			r.Post("/{name}/disable", a.disableSandboxNode)
			r.Post("/{name}/restore", a.restoreSandboxNode)
			r.Post("/{name}/offline", a.offlineSandboxNode)
		})

		r.Get("/audit", a.listAudit)
	})

	// Tracing: wrap the whole router so an inbound W3C traceparent is extracted
	// and a server span is started before auth/handlers run. No-op when tracing
	// is disabled.
	return tracing.HTTPHandler("admin-api.http", r)
}

// ---- middleware ----

type ctxKey int

const actorKey ctxKey = 0

func (a *API) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Disabled mode: no key configured => everyone is the dev admin.
		if a.adminKey == "" {
			next.ServeHTTP(w, r.WithContext(withActor(r, "dev-admin")))
			return
		}
		presented := bearer(r)
		if presented == "" || subtle.ConstantTimeCompare([]byte(presented), []byte(a.adminKey)) != 1 {
			writeErr(w, http.StatusUnauthorized, "PERMISSION_DENIED", "admin authentication required")
			return
		}
		// Actor label: an optional header lets ops attribute the audit trail to a
		// named operator; falls back to a generic label.
		actor := strings.TrimSpace(r.Header.Get("x-cocola-admin"))
		if actor == "" {
			actor = "admin"
		}
		next.ServeHTTP(w, r.WithContext(withActor(r, actor)))
	})
}

func bearer(r *http.Request) string {
	if h := r.Header.Get("authorization"); h != "" {
		if strings.HasPrefix(strings.ToLower(h), "bearer ") {
			return strings.TrimSpace(h[7:])
		}
		return strings.TrimSpace(h)
	}
	return strings.TrimSpace(r.Header.Get("x-admin-key"))
}

// ---- helpers ----

func actorOf(r *http.Request) string {
	if v, ok := r.Context().Value(actorKey).(string); ok {
		return v
	}
	return "unknown"
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

type errBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func writeErr(w http.ResponseWriter, status int, code, msg string) {
	var b errBody
	b.Error.Code = code
	b.Error.Message = msg
	writeJSON(w, status, b)
}

// mapErr translates a service/store sentinel into an HTTP response.
func mapErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrUnauthenticated):
		writeErr(w, http.StatusUnauthorized, "UNAUTHENTICATED", "invalid credentials")
	case errors.Is(err, service.ErrAccountDisabled):
		writeErr(w, http.StatusForbidden, "ACCOUNT_DISABLED", "account disabled")
	case errors.Is(err, service.ErrProtectedAdmin):
		writeErr(w, http.StatusForbidden, "PROTECTED_ADMIN", "bootstrap admin cannot be changed")
	case errors.Is(err, service.ErrSelfPermission):
		writeErr(w, http.StatusForbidden, "SELF_PERMISSION_CHANGE", "admin cannot change own permissions")
	case errors.Is(err, service.ErrPermissionDenied):
		writeErr(w, http.StatusForbidden, "PERMISSION_DENIED", "permission denied")
	case errors.Is(err, service.ErrInvalidArg):
		writeErr(w, http.StatusBadRequest, "INVALID_ARGUMENT", err.Error())
	case errors.Is(err, store.ErrNotFound):
		writeErr(w, http.StatusNotFound, "NOT_FOUND", "resource not found")
	case errors.Is(err, store.ErrConflict):
		writeErr(w, http.StatusConflict, "CONFLICT", "resource already exists")
	case errors.Is(err, service.ErrNotConfigured):
		writeErr(w, http.StatusNotImplemented, "NOT_CONFIGURED", err.Error())
	default:
		writeErr(w, http.StatusInternalServerError, "INTERNAL", "internal error")
	}
}

func decode(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return service.ErrInvalidArg
	}
	return nil
}

func qInt(r *http.Request, key string, def int) int {
	if s := r.URL.Query().Get(key); s != "" {
		if n, err := strconv.Atoi(s); err == nil {
			return n
		}
	}
	return def
}
