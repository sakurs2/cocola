package metrics

import "net/http"

// Mux returns an http.ServeMux exposing /metrics (Prometheus exposition) and
// /healthz (liveness). Services mount it on a dedicated observability port in
// real deployments, e.g.:
//
//	mux := reg.Mux()
//	go func() { _ = http.ListenAndServe(":9090", mux) }()
//
// Per <network_security>, the ListenAndServe call lives in service main and runs
// only in real containers/clusters; this package never binds a port itself, and
// unit tests exercise the mux via httptest.
func (r *Registry) Mux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("/metrics", r.Handler())
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	return mux
}
