package httpapi

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/cocola-project/cocola/apps/gateway/internal/auth"
	"github.com/cocola-project/cocola/apps/gateway/internal/sandboxmgr"
)

// previewProxy reverse-proxies a request to a user-launched dev server running
// on an in-sandbox port. This is cocola's Preview Proxy (inspired by AIO
// Sandbox's /proxy/{port}/): a session's sandbox can run e.g. a Vite/Next dev
// server on port 3000, and the browser reaches it through the gateway at
//
//	/v1/preview/{session_id}/{port}/{rest...}
//
// without ever exposing the sandbox network. The gateway resolves the port to a
// reachable URL via sandbox-manager (which reuses the OpenSandbox lifecycle
// endpoints API), then streams the proxied response.
//
// NOTE (base-path caveat, same as AIO's /proxy vs /absproxy): the app being
// previewed must serve assets relative to this subpath, or be configured with a
// matching public base path. Apps that hard-code root-absolute asset URLs
// (e.g. /static/app.js) will not resolve through the subpath; those need the
// dev server's base/publicPath set to the preview prefix.
func (a *API) previewProxy(w http.ResponseWriter, r *http.Request) {
	id, ok := auth.IdentityOf(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "UNAUTHENTICATED", "missing identity")
		return
	}
	if a.sandboxResolver == nil {
		writeErr(w, http.StatusNotImplemented, "UNIMPLEMENTED", "preview proxy is not configured")
		return
	}

	sessionID := r.PathValue("session_id")
	if sessionID == "" {
		writeErr(w, http.StatusBadRequest, "INVALID_ARGUMENT", "session id is required")
		return
	}
	portStr := r.PathValue("port")
	port, perr := strconv.Atoi(portStr)
	if perr != nil || port <= 0 || port > 65535 {
		writeErr(w, http.StatusBadRequest, "INVALID_ARGUMENT", "port must be in 1..65535")
		return
	}

	// Ownership is enforced server-side: the resolver keys off the VERIFIED
	// user, so a caller can only preview ports of a sandbox bound to their own
	// session — a mismatched session id resolves to nothing.
	ep, err := a.sandboxResolver.ResolveEndpoint(r.Context(), id.UserID, sessionID, port)
	if err != nil {
		a.log.Warn("preview resolve failed: " + err.Error())
		writeErr(w, http.StatusBadGateway, "UNAVAILABLE",
			"could not resolve preview target (is the sandbox running and the port listening?)")
		return
	}

	base, perr2 := url.Parse(ep.URL)
	if perr2 != nil || base.Host == "" {
		a.log.Warn("preview resolve returned malformed url: " + ep.URL)
		writeErr(w, http.StatusBadGateway, "UNAVAILABLE", "preview target is unreachable")
		return
	}

	rest := r.PathValue("rest") // path after the port segment (no leading slash)
	basePath := strings.TrimRight(base.Path, "/")

	proxy := &httputil.ReverseProxy{
		// FlushInterval keeps SSE / streamed dev-server responses responsive
		// instead of buffering the whole body.
		FlushInterval: 200 * time.Millisecond,
		Director: func(req *http.Request) {
			req.URL.Scheme = base.Scheme
			req.URL.Host = base.Host
			req.Host = base.Host
			joined := basePath
			if rest != "" {
				joined = basePath + "/" + strings.TrimLeft(rest, "/")
			} else {
				joined = basePath + "/"
			}
			req.URL.Path = joined
			req.URL.RawQuery = r.URL.RawQuery
			// Replay per-sandbox auth/routing headers on every proxied request.
			for k, v := range ep.Headers {
				req.Header.Set(k, v)
			}
			// Strip the caller's cocola auth so it never leaks into the sandbox.
			req.Header.Del("Authorization")
		},
		ErrorHandler: func(rw http.ResponseWriter, _ *http.Request, e error) {
			a.log.Warn("preview proxy error: " + e.Error())
			writeErr(rw, http.StatusBadGateway, "UNAVAILABLE", "preview target request failed")
		},
	}
	proxy.ServeHTTP(w, r)
}

// compile-time assertion: the gRPC client satisfies the resolver seam.
var _ sandboxmgr.EndpointResolver = (*sandboxmgr.Client)(nil)
