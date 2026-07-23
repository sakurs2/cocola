package project

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

type retryCreateStore struct {
	Store
	refreshedAt time.Time
}

type taskBaseStore struct {
	Store
	project   Project
	workspace *Workspace
}

func (s *taskBaseStore) GetProject(_ context.Context, _ Identity, projectID string) (Project, error) {
	if s.project.ID != projectID {
		return Project{}, ErrNotFound
	}
	return s.project, nil
}

func (s *taskBaseStore) GetWorkspace(
	_ context.Context,
	_ Identity,
	conversationID string,
) (Workspace, Project, error) {
	if s.workspace == nil || s.workspace.ConversationID != conversationID {
		return Workspace{}, Project{}, ErrNotFound
	}
	return *s.workspace, s.project, nil
}

func (s *retryCreateStore) RefreshProjectProvisionAttempt(
	_ context.Context,
	_ Identity,
	_ string,
	now time.Time,
) (Project, error) {
	s.refreshedAt = now
	return Project{
		ID: "11111111-1111-1111-1111-111111111111", RepositoryName: "example",
		Description: "description", Visibility: "private", ProvisionStartedAt: now,
	}, nil
}

func TestGitAuthorIdentityUsesCocolaEmail(t *testing.T) {
	name, email := gitAuthorIdentity(Identity{
		UserID: "user-1", Name: "Alice Example", Username: "alice", Email: "alice@example.com",
	})
	if name != "Alice Example" || email != "alice@example.com" {
		t.Fatalf("gitAuthorIdentity() = %q, %q", name, email)
	}
}

func TestGitAuthorIdentityFallsBackToCocolaUsername(t *testing.T) {
	name, email := gitAuthorIdentity(Identity{UserID: "user-1", Username: "alice"})
	if name != "alice" || email != "alice@localhost" {
		t.Fatalf("gitAuthorIdentity() = %q, %q", name, email)
	}
}

func TestPrepareTaskBaseKeepsLocalProjectOnMain(t *testing.T) {
	projectID := "11111111-1111-1111-1111-111111111111"
	service := &Service{
		store: &taskBaseStore{project: Project{
			ID: projectID, Status: ProjectReady, RepositoryProvider: ProviderLocal,
			DefaultBranch: "main",
		}},
		localProjectsEnabled: true,
	}
	result, err := service.PrepareTaskBase(
		context.Background(), Identity{UserID: "user-a"}, projectID, "new-task", "main",
	)
	if err != nil || result.Ref != "main" || result.SHA != "" {
		t.Fatalf("PrepareTaskBase() = %+v, %v", result, err)
	}
	if _, err := service.PrepareTaskBase(
		context.Background(), Identity{UserID: "user-a"}, projectID, "new-task", "feature/login",
	); !errors.Is(err, ErrBaseRefNotFound) {
		t.Fatalf("non-main local base error = %v", err)
	}
}

func TestPrepareTaskBaseReusesImmutableWorkspaceBase(t *testing.T) {
	projectID := "11111111-1111-1111-1111-111111111111"
	store := &taskBaseStore{
		project: Project{
			ID: projectID, Status: ProjectReady, RepositoryProvider: ProviderLocal,
			DefaultBranch: "main",
		},
		workspace: &Workspace{
			ConversationID: "task-1", ProjectID: projectID,
			BaseRef: "main", BaseSHA: strings.Repeat("a", 40),
		},
	}
	service := &Service{store: store, localProjectsEnabled: true}
	result, err := service.PrepareTaskBase(
		context.Background(), Identity{UserID: "user-a"}, projectID, "task-1", "",
	)
	if err != nil || result.Ref != "main" || result.SHA != strings.Repeat("a", 40) {
		t.Fatalf("PrepareTaskBase() = %+v, %v", result, err)
	}
	if _, err := service.PrepareTaskBase(
		context.Background(), Identity{UserID: "user-a"}, projectID, "task-1", "feature/login",
	); !errors.Is(err, ErrBaseRefMismatch) {
		t.Fatalf("changed task base error = %v", err)
	}
}

func TestGitHubManifestIncludesOAuthCallbackAndDisabledWebhookURL(t *testing.T) {
	manifest := githubManifest("https://cocola.example", "Cocola Alice")
	if got := manifest["callback_urls"]; !reflect.DeepEqual(got, []string{
		"https://cocola.example/connectors/github/oauth/callback",
	}) {
		t.Fatalf("callback_urls = %#v", got)
	}
	hook, ok := manifest["hook_attributes"].(map[string]any)
	if !ok || hook["active"] != false || hook["url"] !=
		"https://cocola.example/connectors/github/webhooks/disabled" {
		t.Fatalf("hook_attributes = %#v", manifest["hook_attributes"])
	}
}

func TestRepositoryCreatedNearPublishIntent(t *testing.T) {
	started := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	if !repositoryCreatedNear(Repository{CreatedAt: started.Add(time.Minute)}, started) {
		t.Fatal("repository created during publish intent was rejected")
	}
	if repositoryCreatedNear(Repository{CreatedAt: started.Add(-3 * time.Minute)}, started) {
		t.Fatal("pre-existing repository was accepted as a publish retry")
	}
	if repositoryCreatedNear(Repository{}, started) {
		t.Fatal("repository without creation time was accepted as a publish retry")
	}
}

func TestRetryCreateRepositoryRecreatesConfirmedMissingRepository(t *testing.T) {
	now := time.Date(2026, 7, 22, 1, 30, 0, 0, time.UTC)
	var created bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer user-token" {
			t.Fatalf("Authorization = %q", r.Header.Get("Authorization"))
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/alice/example":
			http.NotFound(w, r)
		case r.Method == http.MethodPost && r.URL.Path == "/user/repos":
			var input map[string]any
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				t.Fatal(err)
			}
			if input["name"] != "example" || input["private"] != true || input["auto_init"] != true {
				t.Fatalf("create input = %#v", input)
			}
			created = true
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": 42, "name": "example", "full_name": "alice/example",
				"owner":   map[string]any{"id": 7, "login": "alice"},
				"private": true, "visibility": "private", "default_branch": "main",
				"created_at": now,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	store := &retryCreateStore{}
	service := &Service{store: store, now: func() time.Time { return now }}
	github := &githubClient{http: server.Client(), apiBase: server.URL, userAgent: "test"}
	value, repo, createdInRetry, err := service.retryCreateRepository(
		context.Background(), Identity{UserID: "user-a"}, Project{
			ID: "11111111-1111-1111-1111-111111111111", RepositoryName: "example",
			Description: "description", Visibility: "private",
		}, "user-token", "alice", github,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !created || !createdInRetry || repo.ID != 42 || !value.ProvisionStartedAt.Equal(now) ||
		!store.refreshedAt.Equal(now) {
		t.Fatalf("retry result = project:%+v repo:%+v created:%v refreshed:%v",
			value, repo, createdInRetry, store.refreshedAt)
	}
}
