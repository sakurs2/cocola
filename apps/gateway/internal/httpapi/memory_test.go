package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cocola-project/cocola/apps/gateway/internal/auth"
	"github.com/cocola-project/cocola/apps/gateway/internal/memory"
	"github.com/cocola-project/cocola/packages/go-common/logger"
)

func TestMemorySettingsAreDisabledWithoutService(t *testing.T) {
	handler := New(&fakeStreamer{}, auth.NewVerifier(auth.Config{}), logger.Must()).Handler()
	request := httptest.NewRequest(http.MethodGet, "/v1/memory/settings", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	var settings memory.Settings
	if err := json.NewDecoder(response.Body).Decode(&settings); err != nil {
		t.Fatal(err)
	}
	if settings.GlobalEnabled || settings.UseEnabled || settings.LearnEnabled {
		t.Fatalf("memory unexpectedly enabled: %+v", settings)
	}
}
