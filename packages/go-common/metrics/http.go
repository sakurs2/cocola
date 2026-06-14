package metrics

import (
	"net/http"
	"strconv"
	"time"
)

// HTTPMiddleware wraps an http.Handler so every request is recorded into the RED
// vectors under transport="http". The "method" label is the route template the
// caller supplies (NOT the raw path) to avoid unbounded label cardinality from
// path params. Pass a stable label such as "POST /v1/messages".
func (r *Registry) HTTPMiddleware(route string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.inflight.WithLabelValues("http").Inc()
		defer r.inflight.WithLabelValues("http").Dec()

		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()
		next.ServeHTTP(sw, req)
		r.observeRequest("http", route, strconv.Itoa(sw.status), time.Since(start).Seconds())
	})
}

// statusWriter captures the response status code for the RED "code" label while
// transparently forwarding writes. It implements http.Flusher so SSE streams
// (gateway BFF) keep flushing.
type statusWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (w *statusWriter) WriteHeader(code int) {
	if !w.wroteHeader {
		w.status = code
		w.wroteHeader = true
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.wroteHeader = true
	}
	return w.ResponseWriter.Write(b)
}

// Flush forwards to the underlying ResponseWriter if it supports flushing, so
// streaming responses (SSE) are not buffered by the middleware.
func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
