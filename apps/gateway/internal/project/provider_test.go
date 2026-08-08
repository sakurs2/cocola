package project

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGitHubProviderTreatsBlockedMergeabilityAsChecksPending(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer task-token" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		switch r.URL.Path {
		case "/repos/octocat/example/pulls/7":
			_, _ = fmt.Fprint(w, `{"number":7,"state":"open","mergeable":true,"mergeable_state":"blocked","head":{"sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}`)
		case "/repos/octocat/example/commits/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/status":
			_, _ = fmt.Fprint(w, `{"state":"pending","statuses":[]}`)
		case "/repos/octocat/example/commits/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/check-runs":
			_, _ = fmt.Fprint(w, `{"total_count":0,"check_runs":[]}`)
		default:
			t.Fatalf("request path = %s", r.URL.String())
		}
	}))
	defer server.Close()

	provider := githubRepositoryProvider{client: &githubClient{
		http: server.Client(), apiBase: server.URL, userAgent: "test",
	}}
	result, err := provider.GetChangeRequestStatus(context.Background(), "task-token", Project{
		RepositoryOwner: "octocat", RepositoryName: "example",
	}, 7)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "checks_pending" || result.HeadSHA != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("status = %+v", result)
	}
}

func TestProviderMergeRejectionsBecomeNotReady(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		provider RepositoryProvider
	}{
		{
			name: "github", path: "/repos/octocat/example/pulls/7/merge",
		},
		{
			name: "forgejo", path: "/api/v1/repos/octocat/example/pulls/7/merge",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != test.path {
					t.Fatalf("request path = %s", r.URL.Path)
				}
				w.WriteHeader(http.StatusMethodNotAllowed)
				_, _ = fmt.Fprint(w, `{"message":"merge blocked"}`)
			}))
			defer server.Close()
			if test.name == "github" {
				test.provider = githubRepositoryProvider{client: &githubClient{
					http: server.Client(), apiBase: server.URL, userAgent: "test",
				}}
			} else {
				test.provider = forgejoRepositoryProvider{client: &forgejoClient{
					http: server.Client(), apiBase: server.URL,
				}}
			}
			_, err := test.provider.SquashMerge(context.Background(), "task-token", Project{
				RepositoryOwner: "octocat", RepositoryName: "example",
			}, 7, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "Task")
			if !errors.Is(err, ErrChangeRequestNotReady) {
				t.Fatalf("merge error = %v", err)
			}
		})
	}
}
