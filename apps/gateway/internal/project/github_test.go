package project

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRepositoryWarningsDetectsLFSAndSubmodules(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer user-token" {
			t.Fatalf("Authorization = %q", got)
		}
		switch r.URL.Path {
		case "/repos/octocat/example/contents/.gitattributes":
			_, _ = fmt.Fprintf(w, "{\"encoding\":\"base64\",\"content\":%q}",
				base64.StdEncoding.EncodeToString([]byte("*.bin filter=lfs diff=lfs merge=lfs -text\n")))
		case "/repos/octocat/example/contents/.gitmodules":
			_, _ = fmt.Fprintf(w, "{\"encoding\":\"base64\",\"content\":%q}",
				base64.StdEncoding.EncodeToString([]byte("[submodule \"lib\"]\n")))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := &githubClient{http: server.Client(), apiBase: server.URL, userAgent: "test"}
	repo := client.repositoryWarnings(context.Background(), "user-token", Repository{
		Owner: "octocat", Name: "example", DefaultBranch: "main",
	})
	if !repo.HasLFS || !repo.HasSubmodule {
		t.Fatalf("warnings = lfs:%v submodule:%v", repo.HasLFS, repo.HasSubmodule)
	}
}

func TestRepositoryWarningsTreatsMissingFilesAsNoWarning(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	client := &githubClient{http: server.Client(), apiBase: server.URL, userAgent: "test"}
	repo := client.repositoryWarnings(context.Background(), "token", Repository{
		Owner: "octocat", Name: "example", DefaultBranch: "main",
	})
	if repo.HasLFS || repo.HasSubmodule {
		t.Fatalf("unexpected warnings = %+v", repo)
	}
}

func TestBranchesReturnsPagedBranchMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer installation-token" {
			t.Fatalf("Authorization = %q", got)
		}
		if r.URL.Path != "/repos/octocat/example/branches" ||
			r.URL.Query().Get("per_page") != "100" || r.URL.Query().Get("page") != "2" {
			t.Fatalf("request URL = %s", r.URL.String())
		}
		_, _ = fmt.Fprint(w, `[
			{"name":"feature/login","protected":false,"commit":{"sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}},
			{"name":"release/v2","protected":true,"commit":{"sha":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}}
		]`)
	}))
	defer server.Close()

	client := &githubClient{http: server.Client(), apiBase: server.URL, userAgent: "test"}
	branches, more, err := client.branches(
		context.Background(), "installation-token", "octocat", "example", 2,
	)
	if err != nil {
		t.Fatal(err)
	}
	if more || len(branches) != 2 || branches[0].Name != "feature/login" ||
		branches[1].SHA != "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" ||
		!branches[1].Protected {
		t.Fatalf("branches = %+v, more = %v", branches, more)
	}
}
