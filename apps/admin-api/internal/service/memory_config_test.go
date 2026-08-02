package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cocola-project/cocola/apps/admin-api/internal/store"
)

func TestMemoryFeatureIsUnderDevelopment(t *testing.T) {
	ctx := context.Background()
	st := store.NewMemory()
	_, err := st.UpdateMemoryConfig(ctx, store.MemoryConfig{Enabled: true}, 0)
	if err != nil {
		t.Fatal(err)
	}
	svc := New(st, nil, authTestClock)
	view, err := svc.GetMemoryConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if view.Enabled || view.CanEnable || view.Status != "development" {
		t.Fatalf("unexpected development config: %+v", view)
	}
	if _, err := svc.UpdateMemoryConfig(ctx, MemoryConfigInput{Enabled: true}); !errors.Is(err, ErrMemoryUnderDevelopment) {
		t.Fatalf("enable memory error = %v, want ErrMemoryUnderDevelopment", err)
	}
	stored, err := st.GetMemoryConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !stored.Enabled {
		t.Fatal("hard-disabled service unexpectedly rewrote the stored configuration")
	}
}

func TestEmbeddingConnectionReportsDimensionAndSanitizedErrors(t *testing.T) {
	tests := []struct {
		name          string
		status        int
		dimension     int
		wantOK        bool
		wantErrorCode string
	}{
		{name: "connected", status: http.StatusOK, dimension: 1024, wantOK: true},
		{name: "different valid dimension", status: http.StatusOK, dimension: 2560, wantOK: true},
		{name: "empty vector", status: http.StatusOK, wantErrorCode: "invalid_response"},
		{name: "authentication", status: http.StatusUnauthorized, wantErrorCode: "authentication_failed"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(test.status)
				if test.status == http.StatusOK {
					_ = json.NewEncoder(w).Encode(map[string]any{
						"data": []any{map[string]any{"embedding": make([]float64, test.dimension)}},
					})
				}
			}))
			defer server.Close()
			svc := New(store.NewMemory(), nil, authTestClock).
				WithModelSecretKey("secret")
			key := "test-only-key"
			result, err := svc.TestEmbeddingModel(context.Background(), EmbeddingModelTestInput{
				Model: "embed-real", BaseURL: server.URL + "/v1", APIKey: &key,
			})
			if err != nil {
				t.Fatal(err)
			}
			if result.OK != test.wantOK || result.ErrorCode != test.wantErrorCode {
				t.Fatalf("result = %+v", result)
			}
		})
	}
}
