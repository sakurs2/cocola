package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cocola-project/cocola/apps/admin-api/internal/store"
)

func TestMemoryConfigEnableDisableAndOptimisticLock(t *testing.T) {
	ctx := context.Background()
	ready := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ready.Close()
	embedding := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("authorization") != "Bearer test-only-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode embedding request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if _, exists := payload["dimensions"]; exists {
			t.Error("OpenAI-compatible request must not force dimensions")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []any{map[string]any{"embedding": make([]float64, 1024)}},
		})
	}))
	defer embedding.Close()

	st := store.NewMemory()
	svc := New(st, nil, authTestClock).
		WithModelSecretKey("secret").
		WithMemoryEmbeddingDimension(1024).
		WithMemoryOpenVikingURL(ready.URL)
	key := "test-only-key"
	if _, err := svc.CreateLLMProvider(ctx, LLMProviderInput{
		ID: "extract", Name: "Extract", Type: ProviderAnthropic,
		BaseURL: "https://extract.invalid", APIKey: &key,
	}); err != nil {
		t.Fatal(err)
	}
	extraction, err := svc.CreateLLMModel(ctx, LLMModelInput{
		Alias: "extract", ProviderID: "extract", RealModel: "claude-real",
		Label: "Extract", IconType: IconSimpleIcons, IconSlug: "anthropic",
	})
	if err != nil {
		t.Fatal(err)
	}
	embeddingRoute, err := svc.CreateEmbeddingModel(ctx, EmbeddingModelInput{
		Model: "embed-real", BaseURL: embedding.URL + "/v1/embeddings", APIKey: &key,
	})
	if err != nil {
		t.Fatal(err)
	}
	if embeddingRoute.Protocol != "openai-embeddings" || embeddingRoute.Visible ||
		embeddingRoute.IsDefault || embeddingRoute.EmbeddingDimension != 1024 {
		t.Fatalf("unexpected embedding route: %+v", embeddingRoute)
	}
	providers, err := st.ListLLMProviders(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var embeddingProvider *store.LLMProvider
	for index := range providers {
		if providers[index].ID == embeddingRoute.ProviderID {
			embeddingProvider = &providers[index]
			break
		}
	}
	if embeddingProvider == nil || embeddingProvider.Type != ProviderOpenAIEmbeddings ||
		embeddingProvider.APIKeyCiphertext == "" ||
		strings.Contains(embeddingProvider.APIKeyCiphertext, key) {
		t.Fatalf("embedding provider was not stored safely: %+v", embeddingProvider)
	}

	saved, err := svc.UpdateMemoryConfig(ctx, MemoryConfigInput{
		ExtractionModelRouteID: extraction.ID, EmbeddingModelRouteID: embeddingRoute.ID,
		ExpectedVersion: 0, Actor: "admin",
	})
	if err != nil {
		t.Fatalf("save disabled selections: %v", err)
	}
	if saved.Enabled || saved.Version != 1 || !saved.CanEnable || saved.Status != "disabled" {
		t.Fatalf("unexpected disabled config: %+v", saved)
	}

	enabled, err := svc.UpdateMemoryConfig(ctx, MemoryConfigInput{
		Enabled: true, ExtractionModelRouteID: extraction.ID,
		EmbeddingModelRouteID: embeddingRoute.ID, ExpectedVersion: 1, Actor: "admin",
	})
	if err != nil {
		t.Fatalf("enable memory: %v", err)
	}
	if !enabled.Enabled || enabled.Version != 2 || enabled.Status != "ready" {
		t.Fatalf("unexpected enabled config: %+v", enabled)
	}
	if _, err := svc.UpdateMemoryConfig(ctx, MemoryConfigInput{
		Enabled: false, ExpectedVersion: 1, Actor: "stale-admin",
	}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("stale update want conflict, got %v", err)
	}

	ready.Close()
	downstreamCalled := false
	svc.memoryHTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		downstreamCalled = true
		return nil, fmt.Errorf("downstream unavailable")
	})}
	disabled, err := svc.UpdateMemoryConfig(ctx, MemoryConfigInput{
		Enabled: false, ExtractionModelRouteID: extraction.ID,
		EmbeddingModelRouteID: embeddingRoute.ID, ExpectedVersion: 2, Actor: "admin",
	})
	if err != nil {
		t.Fatalf("disable must succeed with OpenViking down: %v", err)
	}
	if disabled.Enabled {
		t.Fatalf("memory remained enabled: %+v", disabled)
	}
	if downstreamCalled {
		t.Fatal("disable path called downstream readiness")
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
				WithModelSecretKey("secret").
				WithMemoryEmbeddingDimension(1024)
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

func TestMemoryConfigCannotEnableIncompleteSelection(t *testing.T) {
	svc := New(store.NewMemory(), nil, authTestClock).
		WithMemoryEmbeddingDimension(1024).
		WithMemoryOpenVikingURL("http://127.0.0.1:1")
	_, err := svc.UpdateMemoryConfig(context.Background(), MemoryConfigInput{
		Enabled: true, ExpectedVersion: 0, Actor: "admin",
	})
	if !errors.Is(err, ErrInvalidArg) {
		t.Fatalf("incomplete enable want ErrInvalidArg, got %v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}
