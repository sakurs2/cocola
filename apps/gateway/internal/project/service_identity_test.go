package project

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

type taskBaseStore struct {
	Store
	project   Project
	workspace *Workspace
}

type provisionClaimStore struct {
	Store
	current     Project
	claimed     Project
	claimErr    error
	attemptID   string
	staleBefore time.Time
}

func (s *provisionClaimStore) ClaimProjectProvisionAttempt(
	_ context.Context,
	_ Identity,
	_ string,
	attemptID string,
	_ time.Time,
	staleBefore time.Time,
) (Project, error) {
	s.attemptID = attemptID
	s.staleBefore = staleBefore
	return s.claimed, s.claimErr
}

func (s *provisionClaimStore) GetProject(
	_ context.Context,
	_ Identity,
	_ string,
) (Project, error) {
	return s.current, nil
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

func (s *taskBaseStore) GetChangeRequest(
	context.Context,
	Identity,
	string,
) (ChangeRequest, error) {
	return ChangeRequest{}, ErrNotFound
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

func TestClaimProvisionAttemptUsesCASAndRejectsAnActiveAttempt(t *testing.T) {
	now := time.Date(2026, 8, 8, 10, 0, 0, 0, time.UTC)
	claimed := Project{ID: "project-1", Status: ProjectProvisioning, ProvisionAttemptID: "claimed"}
	store := &provisionClaimStore{claimed: claimed}
	service := &Service{store: store, now: func() time.Time { return now }}

	result, err := service.claimProvisionAttempt(
		context.Background(), Identity{UserID: "user-1"}, Project{ID: "project-1"},
	)
	if err != nil || result.ProvisionAttemptID != "claimed" || store.attemptID == "" {
		t.Fatalf("claim result = %+v, attempt = %q, err = %v", result, store.attemptID, err)
	}
	if !store.staleBefore.Equal(now.Add(-projectOperationStaleAfter)) {
		t.Fatalf("stale before = %v", store.staleBefore)
	}

	store.claimErr = ErrNotFound
	store.current = Project{ID: "project-1", Status: ProjectProvisioning}
	if _, err := service.claimProvisionAttempt(
		context.Background(), Identity{UserID: "user-1"}, store.current,
	); !errors.Is(err, ErrConflict) {
		t.Fatalf("active attempt error = %v", err)
	}
}

func TestPrepareTaskBaseResolvesLocalProjectMainFromInternalSCM(t *testing.T) {
	projectID := "11111111-1111-1111-1111-111111111111"
	identity := Identity{TenantID: "tenant-a", UserID: "user-a"}
	taskBranchExists := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "token project-token" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Path != "/api/v1/repos/cocola/p-project/branches/main" &&
			!(taskBranchExists && strings.HasSuffix(r.URL.Path, "/branches/cocola/task-login-page")) {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"commit": map[string]any{"id": strings.Repeat("a", 40)},
		})
	}))
	defer server.Close()
	box, err := newSecretBox(base64.StdEncoding.EncodeToString(
		[]byte("0123456789abcdef0123456789abcdef"),
	))
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := box.encrypt("project-token", projectTokenAAD(identity, projectID))
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{
		store: &taskBaseStore{project: Project{
			ID: projectID, Status: ProjectReady, RepositoryProvider: ProviderLocal,
			DefaultBranch: "main", RepositoryOwner: "cocola", RepositoryName: "p-project",
			RepositoryTokenCipher: ciphertext,
		}},
		box: box, localProjectsEnabled: true,
		forgejo: &forgejoClient{http: server.Client(), apiBase: server.URL},
	}
	result, err := service.PrepareTaskBase(
		context.Background(), identity, projectID, "new-task", "main", "cocola/task-login-page",
	)
	if err != nil || result.Ref != "main" || result.SHA != strings.Repeat("a", 40) ||
		result.BranchName != "cocola/task-login-page" {
		t.Fatalf("PrepareTaskBase() = %+v, %v", result, err)
	}
	if _, err := service.PrepareTaskBase(
		context.Background(), identity, projectID, "new-task", "feature/login", "cocola/task-login-page",
	); !errors.Is(err, ErrBaseRefNotFound) {
		t.Fatalf("non-main local base error = %v", err)
	}
	taskBranchExists = true
	if _, err := service.PrepareTaskBase(
		context.Background(), identity, projectID, "new-task", "main", "cocola/task-login-page",
	); !errors.Is(err, ErrTaskBranchExists) {
		t.Fatalf("existing task branch error = %v", err)
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
			BaseRef: "main", BaseSHA: strings.Repeat("a", 40), BranchName: "cocola/task-login-page",
		},
	}
	service := &Service{store: store, localProjectsEnabled: true}
	result, err := service.PrepareTaskBase(
		context.Background(), Identity{UserID: "user-a"}, projectID, "task-1", "", "",
	)
	if err != nil || result.Ref != "main" || result.SHA != strings.Repeat("a", 40) ||
		result.BranchName != "cocola/task-login-page" {
		t.Fatalf("PrepareTaskBase() = %+v, %v", result, err)
	}
	if _, err := service.PrepareTaskBase(
		context.Background(), Identity{UserID: "user-a"}, projectID, "task-1", "feature/login", "",
	); !errors.Is(err, ErrBaseRefMismatch) {
		t.Fatalf("changed task base error = %v", err)
	}
	if _, err := service.PrepareTaskBase(
		context.Background(), Identity{UserID: "user-a"}, projectID, "task-1", "main", "cocola/task-other",
	); !errors.Is(err, ErrTaskBranchMismatch) {
		t.Fatalf("changed task branch error = %v", err)
	}
}

func TestNormalizeTaskBranch(t *testing.T) {
	tests := map[string]struct {
		requested string
		want      string
		wantErr   error
	}{
		"valid":        {requested: " cocola/task-login-page ", want: "cocola/task-login-page"},
		"default":      {requested: "", want: "cocola/task-9ad7d7672f20"},
		"wrong prefix": {requested: "feature/login-page", wantErr: ErrTaskBranchInvalid},
		"uppercase":    {requested: "cocola/task/Login", wantErr: ErrTaskBranchInvalid},
		"slash":        {requested: "cocola/task-login/page", wantErr: ErrTaskBranchInvalid},
		"edge dash":    {requested: "cocola/task--login", wantErr: ErrTaskBranchInvalid},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := normalizeTaskBranch(test.requested, "9ad7d767-2f20-4d67-b8ff-b604d10dd03e")
			if !errors.Is(err, test.wantErr) || got != test.want {
				t.Fatalf("normalizeTaskBranch() = %q, %v; want %q, %v", got, err, test.want, test.wantErr)
			}
		})
	}
}

func TestGitHubManifestIncludesOAuthCallbackAndSupportedPermissions(t *testing.T) {
	manifest := githubManifest("http://localhost:3000", "Cocola Alice")
	if got := manifest["callback_urls"]; !reflect.DeepEqual(got, []string{
		"http://localhost:3000/connectors/github/oauth/callback",
	}) {
		t.Fatalf("callback_urls = %#v", got)
	}
	if _, ok := manifest["hook_attributes"]; ok {
		t.Fatalf("hook_attributes must be omitted when Cocola does not consume GitHub App webhooks")
	}
	permissions, ok := manifest["default_permissions"].(map[string]string)
	if !ok || permissions["actions_variables"] != "write" {
		t.Fatalf("default_permissions = %#v", manifest["default_permissions"])
	}
	if _, ok := permissions["variables"]; ok {
		t.Fatalf("default_permissions contains unsupported variables permission")
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

	service := &Service{now: func() time.Time { return now }}
	github := &githubClient{http: server.Client(), apiBase: server.URL, userAgent: "test"}
	repo, createdInRetry, err := service.retryCreateRepository(
		context.Background(), Project{
			ID: "11111111-1111-1111-1111-111111111111", RepositoryName: "example",
			Description: "description", Visibility: "private",
		}, "user-token", "alice", github,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !created || !createdInRetry || repo.ID != 42 {
		t.Fatalf("retry result = repo:%+v created:%v", repo, createdInRetry)
	}
}
