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
