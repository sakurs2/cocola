package httpapi

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cocola-project/cocola/packages/go-common/metrics"
)

// TestMetricsRoutePattern proves WithMetrics records chi's matched route
// PATTERN (not the raw path) so a request to /admin/tokens/{id} is counted under
// the bounded template rather than a high-cardinality concrete id.
func TestMetricsRoutePattern(t *testing.T) {
	api := newTestAPI("") // auth disabled so requests reach the handler
	reg := metrics.New("admin-test")
	r := api.WithMetrics(reg).Router()

	// Hit a parametrised route with two distinct ids; both must collapse to the
	// same pattern label.
	do(t, r, http.MethodDelete, "/admin/tokens/id-aaa", "", nil)
	do(t, r, http.MethodDelete, "/admin/tokens/id-bbb", "", nil)

	srv := httptest.NewServer(reg.Mux())
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/metrics")
	if err != nil {
		t.Fatalf("scrape: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	out := string(b)

	for _, want := range []string{
		`service="admin-test"`,
		`transport="http"`,
		`method="DELETE /admin/tokens/{id}"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("metrics missing %q in:\n%s", want, out)
		}
	}
	// The concrete ids must NOT appear as labels.
	if strings.Contains(out, "id-aaa") || strings.Contains(out, "id-bbb") {
		t.Fatalf("raw path id leaked into labels:\n%s", out)
	}
}
