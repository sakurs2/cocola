package project

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestCreateRepositoryTokenUsesRepositoryRestriction(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/users/cocola/tokens" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		var body struct {
			Name         string              `json:"name"`
			Scopes       []string            `json:"scopes"`
			Repositories []map[string]string `json:"repositories"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Name != "cocola-project-project-id" ||
			!reflect.DeepEqual(body.Scopes, []string{"write:repository"}) ||
			!reflect.DeepEqual(body.Repositories, []map[string]string{{"owner": "cocola", "name": "p-project"}}) {
			t.Fatalf("token body = %+v", body)
		}
		_, _ = fmt.Fprint(w, `{"id":7,"sha1":"project-token"}`)
	}))
	defer server.Close()

	client := &forgejoClient{http: server.Client(), apiBase: server.URL, username: "cocola", password: "admin-password"}
	id, token, err := client.createRepositoryToken(context.Background(), Repository{Owner: "cocola", Name: "p-project"}, "project-id")
	if err != nil {
		t.Fatal(err)
	}
	if id != 7 || token != "project-token" {
		t.Fatalf("token = (%d, %q)", id, token)
	}
}

func TestProtectMainSendsRuleAndReconcilesExistingProtection(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/repos/cocola/p-project/branch_protections":
			var body struct {
				RuleName      string `json:"rule_name"`
				ApplyToAdmins bool   `json:"apply_to_admins"`
				EnablePush    bool   `json:"enable_push"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.RuleName != "main" || !body.ApplyToAdmins || body.EnablePush {
				t.Fatalf("protection body = %+v", body)
			}
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = fmt.Fprint(w, `{"message":"already protected"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/cocola/p-project/branch_protections/main":
			_, _ = fmt.Fprint(w, `{"rule_name":"main"}`)
		default:
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client := &forgejoClient{http: server.Client(), apiBase: server.URL, username: "cocola", password: "admin-password"}
	if err := client.protectMain(context.Background(), Repository{Owner: "cocola", Name: "p-project"}); err != nil {
		t.Fatal(err)
	}
	if requests != 2 {
		t.Fatalf("requests = %d", requests)
	}
}

func TestCleanGitEnvironmentRemovesInheritedGitAndCredentialState(t *testing.T) {
	values := []string{
		"PATH=/usr/bin", "GIT_DIR=/tmp/other", "GIT_ASKPASS=/tmp/leaked",
		"COCOLA_SCM_TOKEN=leaked", "SSH_ASKPASS=/tmp/ssh", "LANG=en_US.UTF-8",
	}
	want := []string{"PATH=/usr/bin", "LANG=en_US.UTF-8"}
	if got := cleanGitEnvironment(values); !reflect.DeepEqual(got, want) {
		t.Fatalf("clean environment = %#v, want %#v", got, want)
	}
}
