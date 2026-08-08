package project

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type forgejoClient struct {
	http      *http.Client
	apiBase   string
	cloneBase string
	username  string
	password  string
}

type forgejoHTTPError struct {
	Status  int
	Message string
}

func (e *forgejoHTTPError) Error() string {
	return fmt.Sprintf("forgejo request failed with status %d", e.Status)
}

type forgejoGitError struct {
	Stage  string
	Output string
	Err    error
}

func (e *forgejoGitError) Error() string {
	if e.Output == "" {
		return fmt.Sprintf("git repository initialization failed at %s: %v", e.Stage, e.Err)
	}
	return fmt.Sprintf("git repository initialization failed at %s: %v: %s", e.Stage, e.Err, e.Output)
}

func (e *forgejoGitError) Unwrap() error { return e.Err }

type forgejoPull struct {
	Number    int64      `json:"number"`
	HTMLURL   string     `json:"html_url"`
	State     string     `json:"state"`
	Mergeable bool       `json:"mergeable"`
	Merged    bool       `json:"merged"`
	MergedAt  *time.Time `json:"merged_at"`
	MergeBase string     `json:"merge_base"`
	Head      struct {
		SHA string `json:"sha"`
		Ref string `json:"ref"`
	} `json:"head"`
	Base struct {
		SHA string `json:"sha"`
	} `json:"base"`
}

func newForgejoClient(client *http.Client, apiBase, cloneBase, username, password string) (*forgejoClient, error) {
	apiBase = strings.TrimRight(strings.TrimSpace(apiBase), "/")
	cloneBase = strings.TrimRight(strings.TrimSpace(cloneBase), "/")
	username = strings.TrimSpace(username)
	if apiBase == "" || cloneBase == "" || username == "" || password == "" {
		return nil, ErrInternalSCMUnavailable
	}
	for _, raw := range []string{apiBase, cloneBase} {
		parsed, err := url.Parse(raw)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil {
			return nil, errors.New("invalid Forgejo URL")
		}
	}
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &forgejoClient{http: client, apiBase: apiBase, cloneBase: cloneBase, username: username, password: password}, nil
}

func (f *forgejoClient) createRepository(ctx context.Context, projectID, description string) (Repository, error) {
	name := "p-" + strings.ReplaceAll(projectID, "-", "")
	var payload struct {
		ID            int64  `json:"id"`
		Name          string `json:"name"`
		HTMLURL       string `json:"html_url"`
		DefaultBranch string `json:"default_branch"`
		Owner         struct {
			Login string `json:"login"`
		} `json:"owner"`
	}
	err := f.request(ctx, http.MethodPost,
		"/api/v1/admin/users/"+url.PathEscape(f.username)+"/repos", "",
		map[string]any{
			"name": name, "description": description, "private": true,
			"auto_init": false, "default_branch": "main", "has_issues": false,
			"has_projects": false, "has_wiki": false, "has_pull_requests": true,
		}, &payload)
	if forgejoStatus(err, http.StatusConflict) || forgejoStatus(err, http.StatusUnprocessableEntity) {
		err = f.request(ctx, http.MethodGet,
			"/api/v1/repos/"+url.PathEscape(f.username)+"/"+url.PathEscape(name), "", nil, &payload)
	}
	if err != nil {
		return Repository{}, err
	}
	return Repository{
		ID: payload.ID, Owner: payload.Owner.Login, Name: payload.Name,
		FullName: payload.Owner.Login + "/" + payload.Name, HTMLURL: payload.HTMLURL,
		CloneURL:      f.cloneBase + "/" + url.PathEscape(payload.Owner.Login) + "/" + url.PathEscape(payload.Name) + ".git",
		DefaultBranch: "main", Visibility: "private", Private: true, CreatedAt: time.Now().UTC(),
	}, nil
}

func (f *forgejoClient) deleteRepositoryTokensByName(ctx context.Context, name string) error {
	ids := make([]int64, 0)
	for page := 1; page <= 1000; page++ {
		var tokens []struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		}
		path := "/api/v1/users/" + url.PathEscape(f.username) + "/tokens?limit=100&page=" + strconv.Itoa(page)
		if err := f.request(ctx, http.MethodGet, path, "", nil, &tokens); err != nil {
			return err
		}
		for _, token := range tokens {
			if token.Name == name {
				ids = append(ids, token.ID)
			}
		}
		if len(tokens) < 100 {
			break
		}
	}
	for _, tokenID := range ids {
		if err := f.deleteRepositoryToken(ctx, tokenID); err != nil {
			return err
		}
	}
	return nil
}

func (f *forgejoClient) createRepositoryToken(ctx context.Context, repo Repository, projectID string) (int64, string, error) {
	var payload struct {
		ID   int64  `json:"id"`
		SHA1 string `json:"sha1"`
	}
	err := f.request(ctx, http.MethodPost,
		"/api/v1/users/"+url.PathEscape(f.username)+"/tokens", "",
		map[string]any{
			"name":   "cocola-project-" + projectID,
			"scopes": []string{"write:repository"},
			"repositories": []map[string]string{{
				"owner": repo.Owner,
				"name":  repo.Name,
			}},
		}, &payload)
	if err != nil {
		return 0, "", err
	}
	if payload.ID <= 0 || payload.SHA1 == "" {
		return 0, "", errors.New("forgejo returned an incomplete repository token")
	}
	return payload.ID, payload.SHA1, nil
}

func (f *forgejoClient) protectMain(ctx context.Context, repo Repository) error {
	err := f.request(ctx, http.MethodPost,
		"/api/v1/repos/"+url.PathEscape(repo.Owner)+"/"+url.PathEscape(repo.Name)+"/branch_protections", "",
		map[string]any{
			"rule_name":                         "main",
			"branch_name":                       "main",
			"apply_to_admins":                   true,
			"enable_push":                       false,
			"enable_push_whitelist":             true,
			"push_whitelist_usernames":          []string{},
			"enable_merge_whitelist":            true,
			"merge_whitelist_usernames":         []string{f.username},
			"block_on_rejected_reviews":         false,
			"block_on_official_review_requests": false,
			"block_on_outdated_branch":          false,
			"dismiss_stale_approvals":           false,
			"require_signed_commits":            false,
		}, nil)
	if forgejoStatus(err, http.StatusConflict) || forgejoStatus(err, http.StatusUnprocessableEntity) {
		var existing struct {
			RuleName string `json:"rule_name"`
		}
		lookupErr := f.request(ctx, http.MethodGet,
			"/api/v1/repos/"+url.PathEscape(repo.Owner)+"/"+url.PathEscape(repo.Name)+
				"/branch_protections/main", "", nil, &existing)
		if lookupErr == nil && existing.RuleName == "main" {
			return nil
		}
		if lookupErr != nil {
			return lookupErr
		}
	}
	return err
}

func (f *forgejoClient) deleteRepositoryToken(ctx context.Context, tokenID int64) error {
	if tokenID <= 0 {
		return nil
	}
	err := f.request(ctx, http.MethodDelete,
		"/api/v1/users/"+url.PathEscape(f.username)+"/tokens/"+strconv.FormatInt(tokenID, 10), "", nil, nil)
	if forgejoStatus(err, http.StatusNotFound) {
		return nil
	}
	return err
}

func (f *forgejoClient) archiveRepository(ctx context.Context, project Project) error {
	owner := strings.TrimSpace(project.RepositoryOwner)
	if owner == "" {
		owner = f.username
	}
	name := strings.TrimSpace(project.RepositoryName)
	if name == "" {
		name = "p-" + strings.ReplaceAll(project.ID, "-", "")
	}
	err := f.request(ctx, http.MethodPatch,
		"/api/v1/repos/"+url.PathEscape(owner)+"/"+url.PathEscape(name), "",
		map[string]any{"archived": true}, nil)
	if forgejoStatus(err, http.StatusNotFound) {
		return nil
	}
	return err
}

func (f *forgejoClient) initializeRepository(ctx context.Context, repo Repository, token string) (string, error) {
	temporary, err := os.MkdirTemp("", "cocola-forgejo-init-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(temporary)
	askpass := filepath.Join(temporary, "askpass.sh")
	if err := os.WriteFile(askpass, []byte("#!/bin/sh\ncase \"$1\" in *Username*) printf '%s' \"$COCOLA_SCM_USERNAME\" ;; *) printf '%s' \"$COCOLA_SCM_TOKEN\" ;; esac\n"), 0o700); err != nil {
		return "", err
	}
	run := func(stage string, args ...string) ([]byte, error) {
		command := exec.CommandContext(ctx, "git", append([]string{"-c", "credential.helper=", "-c", "core.hooksPath=/dev/null"}, args...)...)
		command.Dir = temporary
		command.Env = cleanGitEnvironment(os.Environ())
		command.Env = append(command.Env,
			"GIT_CONFIG_GLOBAL=/dev/null",
			"GIT_CONFIG_SYSTEM=/dev/null",
			"GIT_TERMINAL_PROMPT=0",
			"GIT_ASKPASS_REQUIRE=force",
			"GIT_ASKPASS="+askpass,
			"COCOLA_SCM_USERNAME="+f.username,
			"COCOLA_SCM_TOKEN="+token,
		)
		output, commandErr := command.CombinedOutput()
		if commandErr != nil {
			safeOutput := strings.TrimSpace(strings.ReplaceAll(string(output), token, "[REDACTED]"))
			if len(safeOutput) > 512 {
				safeOutput = safeOutput[len(safeOutput)-512:]
			}
			return nil, &forgejoGitError{Stage: stage, Output: safeOutput, Err: commandErr}
		}
		return output, nil
	}
	if _, err = run("init", "init", "-b", "main"); err != nil {
		return "", err
	}
	if _, err = run("config-name", "config", "user.name", "Cocola"); err != nil {
		return "", err
	}
	if _, err = run("config-email", "config", "user.email", "cocola@localhost"); err != nil {
		return "", err
	}
	if _, err = run("commit", "commit", "--allow-empty", "-m", "Initialize empty Cocola project"); err != nil {
		return "", err
	}
	internalCloneURL := f.apiBase + "/" + url.PathEscape(repo.Owner) + "/" +
		url.PathEscape(repo.Name) + ".git"
	if _, err = run("remote", "remote", "add", "origin", internalCloneURL); err != nil {
		return "", err
	}
	if _, err = run("push", "push", "origin", "HEAD:refs/heads/main"); err != nil {
		return "", err
	}
	output, err := run("rev-parse", "rev-parse", "HEAD")
	return strings.TrimSpace(string(output)), err
}

func cleanGitEnvironment(values []string) []string {
	cleaned := make([]string, 0, len(values))
	for _, value := range values {
		name, _, _ := strings.Cut(value, "=")
		if strings.HasPrefix(name, "GIT_") || strings.HasPrefix(name, "COCOLA_SCM_") ||
			name == "SSH_ASKPASS" || name == "SSH_ASKPASS_REQUIRE" {
			continue
		}
		cleaned = append(cleaned, value)
	}
	return cleaned
}

func (f *forgejoClient) branchSHA(ctx context.Context, token, owner, repo, branch string) (string, error) {
	var payload struct {
		Commit struct {
			ID string `json:"id"`
		} `json:"commit"`
	}
	err := f.request(ctx, http.MethodGet, "/api/v1/repos/"+url.PathEscape(owner)+"/"+url.PathEscape(repo)+"/branches/"+url.PathEscape(branch), token, nil, &payload)
	return payload.Commit.ID, err
}

func (f *forgejoClient) createPull(ctx context.Context, token string, project Project, workspace Workspace, title string) (forgejoPull, error) {
	var result forgejoPull
	err := f.request(ctx, http.MethodPost, "/api/v1/repos/"+url.PathEscape(project.RepositoryOwner)+"/"+url.PathEscape(project.RepositoryName)+"/pulls", token,
		map[string]any{"base": project.DefaultBranch, "head": workspace.BranchName, "title": title, "body": "Created by Cocola."}, &result)
	if forgejoStatus(err, http.StatusConflict) || forgejoStatus(err, http.StatusUnprocessableEntity) {
		var pulls []forgejoPull
		path := "/api/v1/repos/" + url.PathEscape(project.RepositoryOwner) + "/" + url.PathEscape(project.RepositoryName) +
			"/pulls?state=all&limit=50"
		if listErr := f.request(ctx, http.MethodGet, path, token, nil, &pulls); listErr != nil {
			return forgejoPull{}, err
		}
		for _, pull := range pulls {
			if pull.Head.Ref == workspace.BranchName {
				return pull, nil
			}
		}
	}
	return result, err
}

func (f *forgejoClient) pull(ctx context.Context, token string, project Project, number int64) (forgejoPull, error) {
	var result forgejoPull
	err := f.request(ctx, http.MethodGet, "/api/v1/repos/"+url.PathEscape(project.RepositoryOwner)+"/"+url.PathEscape(project.RepositoryName)+"/pulls/"+strconv.FormatInt(number, 10), token, nil, &result)
	return result, err
}

func (f *forgejoClient) mergePull(ctx context.Context, token string, project Project, number int64, expectedHead, title string) (string, error) {
	var result struct {
		SHA     string `json:"sha"`
		Merged  bool   `json:"merged"`
		Message string `json:"message"`
	}
	err := f.request(ctx, http.MethodPost, "/api/v1/repos/"+url.PathEscape(project.RepositoryOwner)+"/"+url.PathEscape(project.RepositoryName)+"/pulls/"+strconv.FormatInt(number, 10)+"/merge", token,
		map[string]any{"Do": "squash", "head_commit_id": expectedHead, "MergeTitleField": title, "delete_branch_after_merge": true}, &result)
	if forgejoStatus(err, http.StatusMethodNotAllowed) || forgejoStatus(err, http.StatusConflict) ||
		forgejoStatus(err, http.StatusUnprocessableEntity) {
		return "", ErrChangeRequestNotReady
	}
	if err == nil && !result.Merged {
		return "", ErrChangeRequestNotReady
	}
	return result.SHA, err
}

func (f *forgejoClient) deleteBranch(ctx context.Context, token string, project Project, branch string) error {
	err := f.request(ctx, http.MethodDelete,
		"/api/v1/repos/"+url.PathEscape(project.RepositoryOwner)+"/"+url.PathEscape(project.RepositoryName)+
			"/git/refs/heads/"+url.PathEscape(branch), token, nil, nil)
	if forgejoStatus(err, http.StatusNotFound) {
		return nil
	}
	return err
}

func (f *forgejoClient) request(ctx context.Context, method, path, token string, body, out any) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, f.apiBase+path, reader)
	if err != nil {
		return err
	}
	if token != "" {
		req.Header.Set("Authorization", "token "+token)
	} else {
		req.SetBasicAuth(f.username, f.password)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := f.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var payload struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(raw, &payload)
		return &forgejoHTTPError{Status: resp.StatusCode, Message: payload.Message}
	}
	if out != nil && len(raw) > 0 {
		return json.Unmarshal(raw, out)
	}
	return nil
}

func forgejoStatus(err error, status int) bool {
	var value *forgejoHTTPError
	return errors.As(err, &value) && value.Status == status
}
