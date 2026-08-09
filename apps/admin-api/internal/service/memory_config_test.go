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

func TestMemoryConfigEnableDisableAndEmbeddingRouteLock(t *testing.T) {
	ctx := context.Background()
	var st *store.Memory
	ready := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		config, err := st.GetMemoryConfig(ctx)
		if err != nil || config.ExtractionModelRouteID == "" || config.EmbeddingModelRouteID == "" {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ready.Close()
	extractionCalled := false
	embedding := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/messages" {
			extractionCalled = true
			if r.Header.Get("x-api-key") != "test-only-key" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"content": []any{map[string]any{"type": "text", "text": "OK"}},
			})
			return
		}
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

	st = store.NewMemory()
	svc := New(st, nil, authTestClock).
		WithModelSecretKey("secret").
		WithMemoryEmbeddingDimension(1024).
		WithMemoryOpenVikingURL(ready.URL).
		WithMemoryOpenVikingRootAPIKey("root-key")
	key := "test-only-key"
	if _, err := svc.CreateLLMProvider(ctx, LLMProviderInput{
		ID: "extract", Name: "Extract", Type: ProviderAnthropic,
		BaseURL: embedding.URL, APIKey: &key,
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
	if saved.Enabled || saved.Version != 1 || !saved.CanEnable || saved.Status != "disabled" ||
		saved.OpenVikingVersion != "0.4.12" {
		t.Fatalf("unexpected disabled config: %+v", saved)
	}

	enabled, err := svc.UpdateMemoryConfig(ctx, MemoryConfigInput{
		Enabled: true, ExtractionModelRouteID: extraction.ID,
		EmbeddingModelRouteID: embeddingRoute.ID, ExpectedVersion: 1, Actor: "admin",
	})
	if err != nil {
		t.Fatalf("enable memory: %v", err)
	}
	if !enabled.Enabled || enabled.Version != 2 || enabled.Status != "ready" ||
		enabled.ExtractionStatus != "ready" || enabled.EmbeddingStatus != "ready" {
		t.Fatalf("unexpected enabled config: %+v", enabled)
	}
	if !extractionCalled {
		t.Fatal("enable did not test the extraction route")
	}
	if _, err := svc.UpdateEmbeddingModel(ctx, embeddingRoute.ID, EmbeddingModelInput{
		Model: "embed-replacement", BaseURL: embedding.URL + "/v1/embeddings",
	}); !errors.Is(err, ErrInvalidArg) {
		t.Fatalf("same route model replacement must require a Memory reset, got %v", err)
	}
	providerDisabled := *embeddingProvider
	providerDisabled.Enabled = false
	if err := st.UpdateLLMProvider(ctx, providerDisabled); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateMemoryConfig(ctx, MemoryConfigInput{
		Enabled: false, ExpectedVersion: 1, Actor: "stale-admin",
	}); !errors.Is(err, store.ErrVersionConflict) {
		t.Fatalf("stale update want version conflict, got %v", err)
	}

	disabled, err := svc.UpdateMemoryConfig(ctx, MemoryConfigInput{
		Enabled: false, ExtractionModelRouteID: extraction.ID,
		EmbeddingModelRouteID: embeddingRoute.ID, ExpectedVersion: 2, Actor: "admin",
	})
	if err != nil || disabled.Enabled {
		t.Fatalf("disable memory: %+v, %v", disabled, err)
	}
	otherEmbedding, err := svc.CreateEmbeddingModel(ctx, EmbeddingModelInput{
		Model: "embed-other", BaseURL: embedding.URL + "/v1/embeddings", APIKey: &key,
	})
	if err != nil {
		t.Fatal(err)
	}
	changed, err := svc.UpdateMemoryConfig(ctx, MemoryConfigInput{
		ExtractionModelRouteID: extraction.ID, EmbeddingModelRouteID: otherEmbedding.ID,
		ExpectedVersion: 3, Actor: "admin",
	})
	if err != nil || changed.Version != 4 {
		t.Fatalf("save alternate embedding while disabled: %+v, %v", changed, err)
	}
	if _, err := svc.UpdateMemoryConfig(ctx, MemoryConfigInput{
		Enabled: true, ExtractionModelRouteID: extraction.ID,
		EmbeddingModelRouteID: otherEmbedding.ID, ExpectedVersion: 4, Actor: "admin",
	}); !errors.Is(err, ErrInvalidArg) {
		t.Fatalf("embedding route switch want ErrInvalidArg, got %v", err)
	}

	reset, err := svc.ResetMemory(ctx, "admin")
	if err != nil || reset.Enabled || reset.ExtractionModelRouteID != "" ||
		reset.EmbeddingModelRouteID != "" || reset.Version != 5 {
		t.Fatalf("reset memory: %+v, %v", reset, err)
	}
	resaved, err := svc.UpdateMemoryConfig(ctx, MemoryConfigInput{
		ExtractionModelRouteID: extraction.ID, EmbeddingModelRouteID: otherEmbedding.ID,
		ExpectedVersion: 5, Actor: "admin",
	})
	if err != nil || resaved.Enabled || resaved.Version != 6 {
		t.Fatalf("save alternate embedding after reset: %+v, %v", resaved, err)
	}
	reenabled, err := svc.UpdateMemoryConfig(ctx, MemoryConfigInput{
		Enabled: true, ExtractionModelRouteID: extraction.ID,
		EmbeddingModelRouteID: otherEmbedding.ID, ExpectedVersion: 6, Actor: "admin",
	})
	if err != nil || !reenabled.Enabled || reenabled.Version != 7 {
		t.Fatalf("enable alternate embedding after reset: %+v, %v", reenabled, err)
	}
	ready.Close()
	downstreamCalled := false
	svc.memoryHTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		downstreamCalled = true
		return nil, fmt.Errorf("downstream unavailable")
	})}
	disabled, err = svc.UpdateMemoryConfig(ctx, MemoryConfigInput{
		Enabled: false, ExtractionModelRouteID: extraction.ID,
		EmbeddingModelRouteID: otherEmbedding.ID, ExpectedVersion: 7, Actor: "admin",
	})
	if err != nil || disabled.Enabled {
		t.Fatalf("disable must succeed with OpenViking down: %+v, %v", disabled, err)
	}
	if downstreamCalled {
		t.Fatal("disable path called downstream readiness")
	}
	if _, err := svc.GetMemoryConfig(ctx); err != nil {
		t.Fatalf("passive config read failed: %v", err)
	}
	if downstreamCalled {
		t.Fatal("config read called downstream readiness")
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

func TestMemoryConfigRejectsInvalidRoutesWhileDisabled(t *testing.T) {
	svc := New(store.NewMemory(), nil, authTestClock).
		WithMemoryEmbeddingDimension(1024)
	_, err := svc.UpdateMemoryConfig(context.Background(), MemoryConfigInput{
		ExtractionModelRouteID: "missing", EmbeddingModelRouteID: "missing",
		ExpectedVersion: 0, Actor: "admin",
	})
	if !errors.Is(err, ErrInvalidArg) {
		t.Fatalf("invalid disabled routes want ErrInvalidArg, got %v", err)
	}
}

func TestDefaultDisabledMemoryConfigDoesNotCallOpenViking(t *testing.T) {
	called := false
	svc := New(store.NewMemory(), nil, authTestClock).
		WithMemoryEmbeddingDimension(1024).
		WithMemoryOpenVikingURL("http://memory.invalid")
	svc.memoryHTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		called = true
		return nil, errors.New("must not be called")
	})}
	view, err := svc.GetMemoryConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if view.Enabled || view.Status != "disabled" || called {
		t.Fatalf("default config = %+v, downstream called=%t", view, called)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}
