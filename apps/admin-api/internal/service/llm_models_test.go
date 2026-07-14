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

func TestLLMProviderRejectsRemovedOpenAICompatType(t *testing.T) {
	ctx := context.Background()
	svc := New(store.NewMemory(), nil, authTestClock).WithModelSecretKey("secret")
	key := "test-only-key"
	if _, err := svc.CreateLLMProvider(ctx, LLMProviderInput{
		ID: "legacy", Name: "Legacy", Type: "openai_compat",
		BaseURL: "https://example.invalid/v1", APIKey: &key,
	}); !errors.Is(err, ErrInvalidArg) {
		t.Fatalf("removed provider type want ErrInvalidArg, got %v", err)
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
	sonnet, err := svc.CreateLLMModel(ctx, LLMModelInput{
		Alias:      "sonnet",
		ProviderID: "anthropic",
		RealModel:  "claude-sonnet",
		Label:      "Claude Sonnet",
		IconType:   IconSimpleIcons,
		IconSlug:   "anthropic",
		IsDefault:  true,
	})
	if err != nil {
		t.Fatalf("create model: %v", err)
	}
	hidden, err := svc.CreateLLMModel(ctx, LLMModelInput{
		Alias:      "hidden",
		ProviderID: "anthropic",
		RealModel:  "hidden-model",
		Label:      "Hidden",
		IconType:   IconSimpleIcons,
		IconSlug:   "anthropic",
		Visible:    boolPtr(false),
	})
	if err != nil {
		t.Fatalf("create hidden model: %v", err)
	}

	public, err := svc.ListPublicLLMModels(ctx)
	if err != nil {
		t.Fatalf("public models: %v", err)
	}
	if len(public) != 1 || public[0].ID != sonnet.ID || public[0].Alias != "sonnet" {
		t.Fatalf("public models = %+v", public)
	}
	if public[0].Provider != "anthropic" || public[0].Family != "claude" || public[0].IconSlug != "anthropic" {
		t.Fatalf("public model identity = %+v", public[0])
	}
	if len(public[0].Protocols) != 1 || public[0].Protocols[0] != "anthropic-messages" {
		t.Fatalf("public model protocols = %+v", public[0].Protocols)
	}

	if _, err := svc.SetDefaultLLMModel(ctx, hidden.ID, "admin"); !errors.Is(err, ErrInvalidArg) {
		t.Fatalf("hidden default want ErrInvalidArg, got %v", err)
	}
}

func TestLLMModelAliasIsScopedToProviderAndDefaultsToProtocol(t *testing.T) {
	ctx := context.Background()
	svc := New(store.NewMemory(), nil, authTestClock).WithModelSecretKey("secret")
	key := "test-only-key"
	for _, provider := range []LLMProviderInput{
		{ID: "chat-a", Name: "Chat A", Type: ProviderAnthropic, BaseURL: "https://a.invalid", APIKey: &key},
		{ID: "chat-b", Name: "Chat B", Type: ProviderAnthropic, BaseURL: "https://b.invalid", APIKey: &key},
		{ID: "responses", Name: "Responses", Type: ProviderOpenAIResponses, BaseURL: "https://r.invalid/v1", APIKey: &key},
	} {
		if _, err := svc.CreateLLMProvider(ctx, provider); err != nil {
			t.Fatalf("create provider %s: %v", provider.ID, err)
		}
	}
	create := func(providerID string, isDefault bool) store.LLMModelRoute {
		route, err := svc.CreateLLMModel(ctx, LLMModelInput{
			Alias: "shared", ProviderID: providerID, RealModel: "real-" + providerID,
			Label: "Shared", IconType: IconSimpleIcons, IconSlug: "openai", IsDefault: isDefault,
		})
		if err != nil {
			t.Fatalf("create route for %s: %v", providerID, err)
		}
		return route
	}
	chatA := create("chat-a", true)
	chatB := create("chat-b", false)
	responses := create("responses", true)
	if chatA.ID == chatB.ID || chatA.Alias != chatB.Alias {
		t.Fatalf("provider-scoped aliases = %+v %+v", chatA, chatB)
	}
	if chatA.Protocol != "anthropic-messages" || responses.Protocol != "openai-responses" {
		t.Fatalf("route protocols = %q %q", chatA.Protocol, responses.Protocol)
	}
	if !chatA.IsDefault || !responses.IsDefault {
		t.Fatalf("defaults should coexist across protocols")
	}
	if _, err := svc.CreateLLMModel(ctx, LLMModelInput{
		Alias: "shared", ProviderID: "chat-a", RealModel: "duplicate",
		Label: "Duplicate", IconType: IconSimpleIcons, IconSlug: "openai",
	}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("duplicate provider alias want conflict, got %v", err)
	}
}

func TestReferencedProviderProtocolCannotChange(t *testing.T) {
	ctx := context.Background()
	svc := New(store.NewMemory(), nil, authTestClock).WithModelSecretKey("secret")
	key := "test-only-key"
	if _, err := svc.CreateLLMProvider(ctx, LLMProviderInput{
		ID: "provider", Name: "Provider", Type: ProviderAnthropic,
		BaseURL: "https://example.invalid", APIKey: &key,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateLLMModel(ctx, LLMModelInput{
		Alias: "model", ProviderID: "provider", RealModel: "real",
		Label: "Model", IconType: IconSimpleIcons, IconSlug: "anthropic",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateLLMProvider(ctx, "provider", LLMProviderInput{
		Type: ProviderOpenAIResponses,
	}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("referenced provider type change want conflict, got %v", err)
	}
}

func TestResponsesProviderPublishesResponsesProtocol(t *testing.T) {
	ctx := context.Background()
	svc := New(store.NewMemory(), nil, authTestClock).WithModelSecretKey("secret")
	key := "test-only-key"
	if _, err := svc.CreateLLMProvider(ctx, LLMProviderInput{
		ID:      "openai-responses",
		Name:    "OpenAI Responses",
		Type:    ProviderOpenAIResponses,
		BaseURL: "https://api.openai.com/v1",
		APIKey:  &key,
		Enabled: boolPtr(true),
	}); err != nil {
		t.Fatalf("create responses provider: %v", err)
	}
	if _, err := svc.CreateLLMModel(ctx, LLMModelInput{
		Alias:      "codex-model",
		ProviderID: "openai-responses",
		RealModel:  "gpt-real",
		Label:      "Codex Model",
		IconType:   IconSimpleIcons,
		IconSlug:   "openai",
		IsDefault:  true,
	}); err != nil {
		t.Fatalf("create responses model: %v", err)
	}

	models, err := svc.ListPublicLLMModels(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || len(models[0].Protocols) != 1 ||
		models[0].Protocols[0] != "openai-responses" {
		t.Fatalf("responses model protocols = %+v", models)
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
