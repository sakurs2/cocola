package httpapi

import (
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/cocola-project/cocola/apps/admin-api/internal/store"
	"github.com/cocola-project/cocola/packages/go-common/tracing"
)

type auditStatusWriter struct {
	http.ResponseWriter
	status int
}

func (w *auditStatusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *auditStatusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

func (w *auditStatusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (a *API) auditHTTP(actorType string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			sw := &auditStatusWriter{ResponseWriter: w}
			next.ServeHTTP(sw, r)
			status := sw.status
			if status == 0 {
				status = http.StatusOK
			}
			a.appendHTTPAudit(r, actorType, auditAction(r), auditResourceType(r), auditResourceID(r), status, "", start)
		})
	}
}

func (a *API) appendHTTPAudit(r *http.Request, actorType, action, resourceType, resourceID string, status int, errorCode string, start time.Time) {
	traceID := tracing.TraceID(r.Context())
	actor := actorOf(r)
	if actor == "unknown" {
		actor = actorFromHeaders(r)
	}
	meta := map[string]any{"duration_ms": time.Since(start).Milliseconds()}
	if pattern := routePattern(r); pattern != "" {
		meta["route_pattern"] = pattern
	}
	event := store.AuditEvent{
		At:           time.Now().UTC(),
		ActorType:    actorType,
		ActorUserID:  actor,
		ActorEmail:   actor,
		Action:       action,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		Result:       auditResult(status),
		HTTPMethod:   r.Method,
		Route:        routePattern(r),
		StatusCode:   status,
		RequestID:    requestID(r),
		TraceID:      traceID,
		ClientIP:     clientIP(r),
		UserAgent:    r.UserAgent(),
		Metadata:     meta,
		ErrorCode:    errorCode,
	}
	if err := a.svc.AppendAuditEvent(r.Context(), event); err != nil {
		if a.metrics != nil {
			a.metrics.IncAuditWriteError()
		}
		log.Printf("admin audit write failed: %v", err)
	}
}

func auditResult(status int) string {
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return "denied"
	}
	if status >= 400 {
		return "failure"
	}
	return "success"
}

func auditAction(r *http.Request) string {
	scope, resource, op := auditRouteParts(r)
	if scope == "" {
		scope = "http"
	}
	if resource == "" {
		resource = "request"
	}
	return scope + "." + resource + "." + op
}

func auditResourceType(r *http.Request) string {
	_, resource, _ := auditRouteParts(r)
	return resource
}

func auditResourceID(r *http.Request) string {
	for _, name := range []string{"id", "key", "alias", "name", "subject", "artifact_id"} {
		if v := chi.URLParam(r, name); v != "" {
			return v
		}
	}
	return ""
}

func auditRouteParts(r *http.Request) (scope, resource, op string) {
	pattern := strings.Trim(routePattern(r), "/")
	if pattern == "" {
		pattern = strings.Trim(r.URL.Path, "/")
	}
	parts := strings.Split(pattern, "/")
	if len(parts) > 0 {
		scope = sanitizeAuditPart(parts[0])
	}
	if len(parts) > 1 {
		resource = sanitizeAuditPart(parts[1])
	}
	switch r.Method {
	case http.MethodGet:
		op = "list"
		if strings.Contains(pattern, "{") {
			op = "get"
		}
	case http.MethodPost:
		op = "create"
		if len(parts) > 2 && !strings.HasPrefix(parts[len(parts)-1], "{") {
			op = sanitizeAuditPart(parts[len(parts)-1])
		}
	case http.MethodPatch:
		op = "update"
	case http.MethodPut:
		op = "set"
	case http.MethodDelete:
		op = "delete"
	default:
		op = strings.ToLower(r.Method)
	}
	if op == "" {
		op = "request"
	}
	return scope, resource, op
}

func sanitizeAuditPart(v string) string {
	v = strings.Trim(v, "{}")
	v = strings.ReplaceAll(v, "-", "_")
	if v == "" {
		return "unknown"
	}
	return v
}

func routePattern(r *http.Request) string {
	if rc := chi.RouteContext(r.Context()); rc != nil {
		return rc.RoutePattern()
	}
	return ""
}

func requestID(r *http.Request) string {
	if v := strings.TrimSpace(r.Header.Get("x-request-id")); v != "" {
		return v
	}
	return strings.TrimSpace(r.Header.Get("x-cocola-request-id"))
}

func clientIP(r *http.Request) string {
	if v := strings.TrimSpace(r.Header.Get("x-forwarded-for")); v != "" {
		if i := strings.IndexByte(v, ','); i >= 0 {
			return strings.TrimSpace(v[:i])
		}
		return v
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func actorFromHeaders(r *http.Request) string {
	if v := strings.TrimSpace(r.Header.Get("x-cocola-admin")); v != "" {
		return v
	}
	return "anonymous"
}
