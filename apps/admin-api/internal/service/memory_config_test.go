package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cocola-project/cocola/apps/admin-api/internal/store"
)

func TestMemoryConfigEnableDisableAndOptimisticLock(t *testing.T) {
	ctx := context.Background()
	ready := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ready.Close()

	st := store.NewMemory()
	svc := New(st, nil, authTestClock).
		WithModelSecretKey("secret").
		WithMemoryEmbeddingDimension(1024).
		WithMemoryOpenVikingURL(ready.URL)
	key := "test-only-key"
	for _, provider := range []LLMProviderInput{
		{ID: "extract", Name: "Extract", Type: ProviderAnthropic, BaseURL: "https://extract.invalid", APIKey: &key},
		{ID: "embed", Name: "Embed", Type: ProviderOpenAIEmbeddings, BaseURL: "https://embed.invalid/v1", APIKey: &key},
	} {
		if _, err := svc.CreateLLMProvider(ctx, provider); err != nil {
			t.Fatal(err)
		}
	}
	extraction, err := svc.CreateLLMModel(ctx, LLMModelInput{
		Alias: "extract", ProviderID: "extract", RealModel: "claude-real",
		Label: "Extract", IconType: IconSimpleIcons, IconSlug: "anthropic",
	})
	if err != nil {
		t.Fatal(err)
	}
	embedding, err := svc.CreateLLMModel(ctx, LLMModelInput{
		Alias: "embed", ProviderID: "embed", RealModel: "embed-real",
		Label: "Embed", IconType: IconSimpleIcons, IconSlug: "openai", EmbeddingDimension: 1024,
	})
	if err != nil {
		t.Fatal(err)
	}

	saved, err := svc.UpdateMemoryConfig(ctx, MemoryConfigInput{
		ExtractionModelRouteID: extraction.ID, EmbeddingModelRouteID: embedding.ID,
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
		EmbeddingModelRouteID: embedding.ID, ExpectedVersion: 1, Actor: "admin",
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
		EmbeddingModelRouteID: embedding.ID, ExpectedVersion: 2, Actor: "admin",
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
