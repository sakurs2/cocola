package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cocola-project/cocola/apps/admin-api/internal/store"
)

func TestLLMProviderRequiresSecretForAPIKeyAndMasks(t *testing.T) {
	ctx := context.Background()
	svc := New(store.NewMemory(), nil, authTestClock)
	key := "sk-test-secret"
	if _, err := svc.CreateLLMProvider(ctx, LLMProviderInput{
		ID:      "anthropic",
		Name:    "Anthropic",
		Type:    ProviderAnthropic,
		BaseURL: "https://api.anthropic.com",
		APIKey:  &key,
		Enabled: boolPtr(true),
	}); !errors.Is(err, ErrInvalidArg) {
		t.Fatalf("create without secret want ErrInvalidArg, got %v", err)
	}

	svc.WithModelSecretKey("test-model-secret")
	p, err := svc.CreateLLMProvider(ctx, LLMProviderInput{
		ID:      "anthropic",
		Name:    "Anthropic",
		Type:    ProviderAnthropic,
		BaseURL: "https://api.anthropic.com",
		APIKey:  &key,
		Enabled: boolPtr(true),
	})
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	if p.APIKeyHint != "****cret" {
		t.Fatalf("api key hint = %q", p.APIKeyHint)
	}
	if p.APIKeyCiphertext == "" || strings.Contains(p.APIKeyCiphertext, key) {
		t.Fatalf("api key ciphertext not protected: %q", p.APIKeyCiphertext)
	}
}

func TestLLMModelsDefaultAndPublicList(t *testing.T) {
	ctx := context.Background()
	st := store.NewMemory()
	svc := New(st, nil, func() time.Time { return time.Unix(10, 0).UTC() }).WithModelSecretKey("secret")
	key := "sk-test-secret"
	if _, err := svc.CreateLLMProvider(ctx, LLMProviderInput{
		ID:      "anthropic",
		Name:    "Anthropic",
		Type:    ProviderAnthropic,
		BaseURL: "https://api.anthropic.com",
		APIKey:  &key,
		Enabled: boolPtr(true),
	}); err != nil {
		t.Fatalf("create provider: %v", err)
	}
	if _, err := svc.CreateLLMModel(ctx, LLMModelInput{
		Alias:      "sonnet",
		ProviderID: "anthropic",
		RealModel:  "claude-sonnet",
		Label:      "Claude Sonnet",
		IconType:   IconSimpleIcons,
		IconSlug:   "anthropic",
		IsDefault:  true,
	}); err != nil {
		t.Fatalf("create model: %v", err)
	}
	if _, err := svc.CreateLLMModel(ctx, LLMModelInput{
		Alias:      "hidden",
		ProviderID: "anthropic",
		RealModel:  "hidden-model",
		Label:      "Hidden",
		IconType:   IconSimpleIcons,
		IconSlug:   "anthropic",
		Visible:    boolPtr(false),
	}); err != nil {
		t.Fatalf("create hidden model: %v", err)
	}

	public, err := svc.ListPublicLLMModels(ctx)
	if err != nil {
		t.Fatalf("public models: %v", err)
	}
	if len(public) != 1 || public[0].Alias != "sonnet" {
		t.Fatalf("public models = %+v", public)
	}
	if public[0].Provider != "anthropic" || public[0].Family != "claude" || public[0].IconSlug != "anthropic" {
		t.Fatalf("public model identity = %+v", public[0])
	}

	if _, err := svc.SetDefaultLLMModel(ctx, "hidden", "admin"); !errors.Is(err, ErrInvalidArg) {
		t.Fatalf("hidden default want ErrInvalidArg, got %v", err)
	}
}

func TestDeleteReferencedLLMProviderConflicts(t *testing.T) {
	ctx := context.Background()
	st := store.NewMemory()
	svc := New(st, nil, authTestClock).WithModelSecretKey("secret")
	key := "sk-test-secret"
	if _, err := svc.CreateLLMProvider(ctx, LLMProviderInput{
		ID:      "anthropic",
		Name:    "Anthropic",
		Type:    ProviderAnthropic,
		BaseURL: "https://api.anthropic.com",
		APIKey:  &key,
		Enabled: boolPtr(true),
	}); err != nil {
		t.Fatalf("create provider: %v", err)
	}
	if _, err := svc.CreateLLMModel(ctx, LLMModelInput{
		Alias:      "sonnet",
		ProviderID: "anthropic",
		RealModel:  "claude-sonnet",
		Label:      "Claude Sonnet",
		IconType:   IconSimpleIcons,
		IconSlug:   "anthropic",
	}); err != nil {
		t.Fatalf("create model: %v", err)
	}
	if err := svc.DeleteLLMProvider(ctx, "anthropic", "admin"); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("delete referenced provider want conflict, got %v", err)
	}
}
