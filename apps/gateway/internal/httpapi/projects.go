package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/cocola-project/cocola/apps/gateway/internal/agent"
	"github.com/cocola-project/cocola/apps/gateway/internal/auth"
	"github.com/cocola-project/cocola/apps/gateway/internal/chatrun"
	"github.com/cocola-project/cocola/apps/gateway/internal/project"
)

type oauthStartRequest struct {
	ReturnTo string `json:"return_to"`
}

type oauthCallbackRequest struct {
	State string `json:"state"`
	Code  string `json:"code"`
}

type manifestCompleteRequest struct {
	State string `json:"state"`
	Code  string `json:"code"`
}

type archiveProjectRequest struct {
	ExpectedVersion int64 `json:"expected_version"`
}

type inspectGitRequest struct {
	Operation  string `json:"operation"`
	Path       string `json:"path"`
	DiffTarget string `json:"diff_target"`
	CommitSHA  string `json:"commit_sha"`
}

func projectIdentity(r *http.Request) (project.Identity, bool) {
	id, ok := auth.IdentityOf(r)
	return project.Identity{
		TenantID: id.TenantID, UserID: id.UserID, Email: id.Email,
		Name: id.Name, Username: id.Username,
	}, ok
}

func (a *API) githubConnection(w http.ResponseWriter, r *http.Request) {
	id, ok := projectIdentity(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "UNAUTHENTICATED", "missing identity")
		return
	}
	if a.projects == nil {
		writeJSON(w, http.StatusOK, project.ConnectionView{Status: "disabled", Enabled: false})
		return
	}
	result, err := a.projects.Connection(r.Context(), id)
	if a.writeProjectError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *API) connectors(w http.ResponseWriter, r *http.Request) {
	id, ok := projectIdentity(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "UNAUTHENTICATED", "missing identity")
		return
	}
	github := project.ConnectionView{Status: "disabled", Enabled: false}
	if a.projects != nil {
		value, err := a.projects.Connection(r.Context(), id)
		if a.writeProjectError(w, err) {
			return
		}
		github = value
	}
	writeJSON(w, http.StatusOK, []map[string]any{{
		"id": "github", "name": "GitHub", "status": github.Status,
		"enabled": github.Enabled, "connection": github,
	}})
}

func (a *API) githubManifestStart(w http.ResponseWriter, r *http.Request) {
	id, ok := projectIdentity(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "UNAUTHENTICATED", "missing identity")
		return
	}
	var input oauthStartRequest
	if !decodeProjectJSON(w, r, &input) {
		return
	}
	if a.projects == nil {
		writeErr(w, http.StatusNotImplemented, "PROJECTS_DISABLED", "Projects are not configured")
		return
	}
	result, err := a.projects.StartManifest(r.Context(), id, input.ReturnTo, r.Header.Get("Origin"))
	if a.writeProjectError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *API) githubManifestComplete(w http.ResponseWriter, r *http.Request) {
	id, ok := projectIdentity(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "UNAUTHENTICATED", "missing identity")
		return
	}
	var input manifestCompleteRequest
	if !decodeProjectJSON(w, r, &input) {
		return
	}
	if a.projects == nil {
		writeErr(w, http.StatusNotImplemented, "PROJECTS_DISABLED", "Projects are not configured")
		return
	}
	result, err := a.projects.CompleteManifest(r.Context(), id, input.State, input.Code)
	if a.writeProjectError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *API) githubInstallationComplete(w http.ResponseWriter, r *http.Request) {
	id, ok := projectIdentity(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "UNAUTHENTICATED", "missing identity")
		return
	}
	var input oauthStartRequest
	if !decodeProjectJSON(w, r, &input) {
		return
	}
	if a.projects == nil {
		writeErr(w, http.StatusNotImplemented, "PROJECTS_DISABLED", "Projects are not configured")
		return
	}
	result, err := a.projects.StartOAuth(r.Context(), id, input.ReturnTo)
	if a.writeProjectError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *API) githubOAuthStart(w http.ResponseWriter, r *http.Request) {
	id, ok := projectIdentity(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "UNAUTHENTICATED", "missing identity")
		return
	}
	var input oauthStartRequest
	if !decodeProjectJSON(w, r, &input) {
		return
	}
	if a.projects == nil {
		writeErr(w, http.StatusNotImplemented, "GITHUB_DISABLED", "GitHub Projects are not configured")
		return
	}
	result, err := a.projects.StartOAuth(r.Context(), id, input.ReturnTo)
	if a.writeProjectError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *API) githubOAuthCallback(w http.ResponseWriter, r *http.Request) {
	id, ok := projectIdentity(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "UNAUTHENTICATED", "missing identity")
		return
	}
	var input oauthCallbackRequest
	if !decodeProjectJSON(w, r, &input) {
		return
	}
	if a.projects == nil {
		writeErr(w, http.StatusNotImplemented, "GITHUB_DISABLED", "GitHub Projects are not configured")
		return
	}
	result, err := a.projects.CompleteOAuth(r.Context(), id, input.State, input.Code)
	if a.writeProjectError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *API) githubDisconnect(w http.ResponseWriter, r *http.Request) {
	id, ok := projectIdentity(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "UNAUTHENTICATED", "missing identity")
		return
	}
	if a.projects == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if a.writeProjectError(w, a.projects.Disconnect(r.Context(), id)) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) githubRepositories(w http.ResponseWriter, r *http.Request) {
	id, ok := projectIdentity(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "UNAUTHENTICATED", "missing identity")
		return
	}
	if a.projects == nil {
		writeErr(w, http.StatusNotImplemented, "GITHUB_DISABLED", "GitHub Projects are not configured")
		return
	}
	result, err := a.projects.Repositories(r.Context(), id, r.URL.Query().Get("cursor"))
	if a.writeProjectError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *API) listProjects(w http.ResponseWriter, r *http.Request) {
	id, ok := projectIdentity(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "UNAUTHENTICATED", "missing identity")
		return
	}
	if a.projects == nil {
		writeJSON(w, http.StatusOK, []project.Project{})
		return
	}
	result, err := a.projects.List(r.Context(), id)
	if a.writeProjectError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *API) createProject(w http.ResponseWriter, r *http.Request) {
	id, ok := projectIdentity(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "UNAUTHENTICATED", "missing identity")
		return
	}
	var input project.CreateInput
	if !decodeProjectJSON(w, r, &input) {
		return
	}
	if a.projects == nil {
		writeErr(w, http.StatusNotImplemented, "GITHUB_DISABLED", "GitHub Projects are not configured")
		return
	}
	if strings.TrimSpace(input.RuntimeID) == "" {
		for _, runtime := range a.runtimes {
			if runtime.IsDefault {
				input.RuntimeID = runtime.ID
				break
			}
		}
	}
	if _, supported := a.runtimeByID[input.RuntimeID]; !supported {
		writeErr(w, http.StatusBadRequest, "UNSUPPORTED_RUNTIME", "agent runtime is not supported")
		return
	}
	result, err := a.projects.Create(r.Context(), id, input)
	if a.writeProjectError(w, err) {
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func (a *API) getProject(w http.ResponseWriter, r *http.Request) {
	id, ok := projectIdentity(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "UNAUTHENTICATED", "missing identity")
		return
	}
	if a.projects == nil {
		writeErr(w, http.StatusNotFound, "NOT_FOUND", "project not found")
		return
	}
	result, err := a.projects.Get(r.Context(), id, r.PathValue("id"))
	if a.writeProjectError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *API) updateProject(w http.ResponseWriter, r *http.Request) {
	id, ok := projectIdentity(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "UNAUTHENTICATED", "missing identity")
		return
	}
	var input project.UpdateInput
	if !decodeProjectJSON(w, r, &input) {
		return
	}
	if a.projects == nil {
		writeErr(w, http.StatusNotFound, "NOT_FOUND", "project not found")
		return
	}
	if _, supported := a.runtimeByID[strings.TrimSpace(input.RuntimeID)]; !supported {
		writeErr(w, http.StatusBadRequest, "UNSUPPORTED_RUNTIME", "agent runtime is not supported")
		return
	}
	result, err := a.projects.Update(r.Context(), id, r.PathValue("id"), input)
	if a.writeProjectError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *API) archiveProject(w http.ResponseWriter, r *http.Request) {
	id, ok := projectIdentity(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "UNAUTHENTICATED", "missing identity")
		return
	}
	var input archiveProjectRequest
	if !decodeProjectJSON(w, r, &input) {
		return
	}
	if a.projects == nil {
		writeErr(w, http.StatusNotFound, "NOT_FOUND", "project not found")
		return
	}
	result, err := a.projects.Archive(r.Context(), id, r.PathValue("id"), input.ExpectedVersion)
	if a.writeProjectError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *API) retryProject(w http.ResponseWriter, r *http.Request) {
	id, ok := projectIdentity(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "UNAUTHENTICATED", "missing identity")
		return
	}
	if a.projects == nil {
		writeErr(w, http.StatusNotFound, "NOT_FOUND", "project not found")
		return
	}
	result, err := a.projects.Retry(r.Context(), id, r.PathValue("id"))
	if a.writeProjectError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *API) projectTasks(w http.ResponseWriter, r *http.Request) {
	id, ok := projectIdentity(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "UNAUTHENTICATED", "missing identity")
		return
	}
	if a.projects == nil {
		writeErr(w, http.StatusNotFound, "NOT_FOUND", "project not found")
		return
	}
	result, err := a.projects.Tasks(r.Context(), id, r.PathValue("id"))
	if a.writeProjectError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *API) gitStatus(w http.ResponseWriter, r *http.Request) {
	id, ok := projectIdentity(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "UNAUTHENTICATED", "missing identity")
		return
	}
	if a.projects == nil {
		writeErr(w, http.StatusNotFound, "NOT_FOUND", "project workspace not found")
		return
	}
	workspace, projectValue, err := a.projects.Workspace(r.Context(), id, r.PathValue("id"))
	if a.writeProjectError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"workspace": workspace, "project": projectValue})
}

func (a *API) inspectGit(w http.ResponseWriter, r *http.Request) {
	id, ok := projectIdentity(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "UNAUTHENTICATED", "missing identity")
		return
	}
	if a.projects == nil || a.gitInspector == nil {
		writeErr(w, http.StatusNotImplemented, "GIT_INSPECT_UNAVAILABLE", "Git inspection is not configured")
		return
	}
	var input inspectGitRequest
	if !decodeProjectJSON(w, r, &input) {
		return
	}
	input.Operation = strings.TrimSpace(input.Operation)
	input.Path = strings.TrimSpace(input.Path)
	input.DiffTarget = strings.TrimSpace(input.DiffTarget)
	input.CommitSHA = strings.TrimSpace(input.CommitSHA)
	if input.Operation != "status" && input.Operation != "diff" && input.Operation != "commit" {
		writeErr(w, http.StatusBadRequest, "INVALID_ARGUMENT", "operation must be status, diff, or commit")
		return
	}
	if input.Operation == "diff" && (input.Path == "" || (input.DiffTarget != "working" && input.DiffTarget != "staged")) {
		writeErr(w, http.StatusBadRequest, "INVALID_ARGUMENT", "diff requires a path and working or staged target")
		return
	}
	if input.Operation == "commit" && !validGitCommitSHA(input.CommitSHA) {
		writeErr(w, http.StatusBadRequest, "INVALID_ARGUMENT", "commit requires a full commit SHA")
		return
	}
	conversationID := r.PathValue("id")
	if a.runs != nil {
		if active, err := a.runs.store.Active(r.Context(), conversationID, id.UserID); err == nil {
			writeRunInProgress(w, active, conversationID, "Git status will update after the running answer completes")
			return
		} else if !errors.Is(err, chatrun.ErrNotFound) {
			writeErr(w, http.StatusServiceUnavailable, "RUN_STORE_UNAVAILABLE", "could not verify conversation run state")
			return
		}
	}
	contextValue, scmToken, err := a.projects.ProjectContext(r.Context(), id, conversationID)
	if a.writeProjectError(w, err) {
		return
	}
	inspectCtx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	result, err := a.gitInspector.InspectWorkspaceGit(inspectCtx, agent.InspectRequest{
		UserID: id.UserID, SessionID: conversationID, Operation: input.Operation,
		Path: input.Path, DiffTarget: input.DiffTarget, CommitSHA: input.CommitSHA, SCMToken: scmToken,
		Project: agent.ProjectContext{
			ProjectID: contextValue.ProjectID, RepositoryID: contextValue.RepositoryExternalID,
			CloneURL: contextValue.CloneURL, DefaultBranch: contextValue.DefaultBranch,
			BaseSHA: contextValue.BaseSHA, TaskBranch: contextValue.BranchName,
			GitAuthorName: contextValue.GitAuthorName, GitAuthorEmail: contextValue.GitAuthorEmail,
			RepositoryProvider: contextValue.RepositoryProvider,
			RepositoryFullName: contextValue.RepositoryFullName,
			CredentialMode:     contextValue.CredentialMode,
		},
	})
	if err != nil {
		a.log.Warn("git inspect failed: " + strings.ReplaceAll(err.Error(), "\n", " "))
		writeErr(w, http.StatusBadGateway, "GIT_INSPECT_FAILED", "could not inspect project workspace")
		return
	}
	snapshot := project.GitSnapshot{
		Branch: result.Snapshot.Branch, BaseRef: result.Snapshot.BaseRef, BaseSHA: result.Snapshot.BaseSHA,
		HeadSHA: result.Snapshot.HeadSHA, Ahead: result.Snapshot.Ahead,
		Dirty: result.Snapshot.Dirty, Truncated: result.Snapshot.Truncated,
		HistoryTruncated: result.Snapshot.HistoryTruncated,
		CapturedAt:       time.Now().UTC(),
	}
	for _, change := range result.Snapshot.Changes {
		snapshot.Changes = append(snapshot.Changes, project.Change{
			Path: change.Path, OldPath: change.OldPath, Status: change.Status, Area: change.Area,
		})
	}
	for _, commit := range result.Snapshot.Commits {
		snapshot.Commits = append(snapshot.Commits, projectGitCommit(commit))
	}
	if err := a.projects.SaveSnapshot(r.Context(), id, conversationID, snapshot, snapshot.HeadSHA, "ready"); err != nil {
		a.log.Warn("git snapshot persistence failed: " + err.Error())
	}
	var commit *project.GitCommit
	if result.Commit != nil {
		value := projectGitCommit(*result.Commit)
		commit = &value
	}
	commitFiles := make([]project.GitCommitFile, 0, len(result.CommitFiles))
	for _, value := range result.CommitFiles {
		commitFiles = append(commitFiles, project.GitCommitFile{
			Path: value.Path, OldPath: value.OldPath, Status: value.Status, Binary: value.Binary,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"snapshot": snapshot, "diff": result.Diff, "binary": result.Binary,
		"truncated": result.Truncated, "commit": commit, "commit_files": commitFiles,
	})
}

func projectGitCommit(value agent.GitCommit) project.GitCommit {
	return project.GitCommit{
		SHA: value.SHA, Parents: append([]string(nil), value.Parents...), Subject: value.Subject,
		AuthorName: value.AuthorName, AuthoredAt: value.AuthoredAt,
		Refs: append([]string(nil), value.Refs...), FilesChanged: value.FilesChanged,
		Additions: value.Additions, Deletions: value.Deletions, Body: value.Body,
	}
}

func validGitCommitSHA(value string) bool {
	if len(value) != 40 {
		return false
	}
	for _, character := range value {
		if !strings.ContainsRune("0123456789abcdefABCDEF", character) {
			return false
		}
	}
	return true
}

func (a *API) publishLocalProject(w http.ResponseWriter, r *http.Request) {
	id, ok := projectIdentity(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "UNAUTHENTICATED", "missing identity")
		return
	}
	if a.projects == nil || a.gitInspector == nil || a.gitPublisher == nil {
		writeErr(w, http.StatusNotImplemented, "PROJECT_PUBLISH_UNAVAILABLE", "Project publishing is not configured")
		return
	}
	var input project.PublishInput
	if !decodeProjectJSON(w, r, &input) {
		return
	}
	projectID := r.PathValue("id")
	projectValue, err := a.projects.Get(r.Context(), id, projectID)
	if a.writeProjectError(w, err) {
		return
	}
	conversationID := projectValue.PrimaryConversationID
	if conversationID == "" {
		writeErr(w, http.StatusConflict, "PROJECT_WORKSPACE_REQUIRED", "start the Project workspace before publishing")
		return
	}
	if a.runs != nil {
		if active, activeErr := a.runs.store.Active(r.Context(), conversationID, id.UserID); activeErr == nil {
			writeRunInProgress(w, active, conversationID, "Wait for the running answer before publishing")
			return
		} else if !errors.Is(activeErr, chatrun.ErrNotFound) {
			writeErr(w, http.StatusServiceUnavailable, "RUN_STORE_UNAVAILABLE", "could not verify conversation run state")
			return
		}
	}
	contextValue, _, err := a.projects.ProjectContext(r.Context(), id, conversationID)
	if a.writeProjectError(w, err) {
		return
	}
	publishContext, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	gitContext := agent.ProjectContext{
		ProjectID: contextValue.ProjectID, RepositoryID: contextValue.RepositoryExternalID,
		CloneURL: contextValue.CloneURL, DefaultBranch: contextValue.DefaultBranch,
		BaseSHA: contextValue.BaseSHA, TaskBranch: contextValue.BranchName,
		GitAuthorName: contextValue.GitAuthorName, GitAuthorEmail: contextValue.GitAuthorEmail,
		RepositoryProvider: contextValue.RepositoryProvider,
		RepositoryFullName: contextValue.RepositoryFullName,
		CredentialMode:     contextValue.CredentialMode,
	}
	inspection, err := a.gitInspector.InspectWorkspaceGit(publishContext, agent.InspectRequest{
		UserID: id.UserID, SessionID: conversationID, Operation: "status", Project: gitContext,
	})
	if err != nil {
		writeErr(w, http.StatusBadGateway, "GIT_INSPECT_FAILED", "could not inspect the local Project workspace")
		return
	}
	if inspection.Snapshot.Dirty || inspection.Snapshot.HeadSHA == "" {
		writeErr(w, http.StatusConflict, "PROJECT_WORKSPACE_DIRTY", "commit all local changes before publishing")
		return
	}
	preparation, err := a.projects.PrepareLocalPublish(publishContext, id, projectID, input)
	if a.writeProjectError(w, err) {
		return
	}
	defer func() {
		revokeContext, revokeCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer revokeCancel()
		a.projects.RevokeGitHubToken(revokeContext, id, preparation.Token)
	}()
	headSHA, err := a.gitPublisher.PublishWorkspaceGit(publishContext, agent.PublishRequest{
		UserID: id.UserID, SessionID: conversationID, SCMToken: preparation.Token,
		RemoteCloneURL: preparation.CloneURL, ExpectedHeadSHA: inspection.Snapshot.HeadSHA,
		Project: gitContext,
	})
	if err != nil || !strings.EqualFold(headSHA, inspection.Snapshot.HeadSHA) {
		a.log.Warn("local Project publish failed")
		writeErr(w, http.StatusBadGateway, "PROJECT_PUBLISH_FAILED", "could not push the local Project to GitHub")
		return
	}
	result, err := a.projects.CompleteLocalPublish(r.Context(), id, preparation)
	if a.writeProjectError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func decodeProjectJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_ARGUMENT", "malformed JSON body")
		return false
	}
	return true
}

func (a *API) writeProjectError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, project.ErrDisabled):
		writeErr(w, http.StatusNotImplemented, "GITHUB_DISABLED", "GitHub Projects are not configured")
	case errors.Is(err, project.ErrLocalProjectsDisabled):
		writeErr(w, http.StatusNotImplemented, "LOCAL_PROJECTS_DISABLED", "Local Projects are disabled")
	case errors.Is(err, project.ErrNotFound):
		writeErr(w, http.StatusNotFound, "NOT_FOUND", "project resource not found")
	case errors.Is(err, project.ErrInvalidArgument):
		writeErr(w, http.StatusBadRequest, "INVALID_ARGUMENT", "invalid project request")
	case errors.Is(err, project.ErrConnectionRequired):
		writeErr(w, http.StatusConflict, "GITHUB_CONNECTION_REQUIRED", "connect or reauthorize GitHub")
	case errors.Is(err, project.ErrInstallationRequired):
		writeErr(w, http.StatusConflict, "GITHUB_INSTALLATION_REQUIRED", "install the GitHub App on your personal account")
	case errors.Is(err, project.ErrRepositoryNotInstalled):
		writeErr(w, http.StatusConflict, "REPOSITORY_NOT_INSTALLED", "grant the GitHub App access to this repository")
	case errors.Is(err, project.ErrRepositoryTooLarge):
		writeErr(w, http.StatusUnprocessableEntity, "REPOSITORY_TOO_LARGE", "repository exceeds the configured project size limit")
	case errors.Is(err, project.ErrProjectNotReady):
		writeErr(w, http.StatusConflict, "PROJECT_NOT_READY", "project is not ready")
	case errors.Is(err, project.ErrVersionConflict):
		writeErr(w, http.StatusConflict, "VERSION_CONFLICT", "project was changed by another request")
	case errors.Is(err, project.ErrConflict):
		writeErr(w, http.StatusConflict, "PROJECT_CONFLICT", "project conflicts with an existing resource")
	default:
		a.log.Warn("project request failed: " + strings.ReplaceAll(err.Error(), "\n", " "))
		writeErr(w, http.StatusBadGateway, "PROJECT_UPSTREAM_ERROR", "project operation could not be completed")
	}
	return true
}
