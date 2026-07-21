package project

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	brokerCredentialTTL = 12 * time.Hour
	approvalTTL         = 5 * time.Minute
)

type classifiedCommand struct {
	category    string
	label       string
	permissions map[string]string
	highRisk    bool
}

func (s *Service) IssueBrokerCredential(
	ctx context.Context,
	id Identity,
	conversationID string,
	runID string,
) (string, error) {
	if !s.GitHubAgentWriteEnabled() {
		return "", ErrDisabled
	}
	if strings.TrimSpace(runID) == "" {
		return "", ErrInvalidArgument
	}
	workspace, projectValue, err := s.store.GetWorkspace(ctx, id, conversationID)
	if err != nil {
		return "", err
	}
	if !brokerProjectReady(projectValue) {
		return "", ErrInvalidArgument
	}
	projectValue, _, connection, _, err := s.currentProjectInstallation(ctx, id, projectValue)
	if err != nil {
		return "", err
	}
	registration, err := s.store.GetAppRegistration(ctx, id)
	if err != nil {
		return "", ErrConnectionRequired
	}
	if connection.Status != ConnectionReady ||
		connection.RegistrationID != registration.ID ||
		connection.InstallationID != projectValue.InstallationID {
		return "", ErrConnectionRequired
	}
	now := s.now()
	claims := BrokerCredentialClaims{
		TenantID: id.TenantID, UserID: id.UserID, ConversationID: workspace.ConversationID,
		RunID: runID, ProjectID: projectValue.ID,
		RepositoryID:       projectValue.RepositoryExternalID,
		RepositoryFullName: projectValue.RepositoryOwner + "/" + projectValue.RepositoryName,
		InstallationID:     projectValue.InstallationID, RegistrationID: registration.ID,
		TaskBranch: workspace.BranchName, ExpiresAt: now.Add(brokerCredentialTTL).Unix(),
	}
	credential, err := s.box.signBrokerCredential(claims)
	if err != nil {
		return "", err
	}
	if err := s.store.SaveBrokerRun(ctx, BrokerRun{
		TenantID: claims.TenantID, UserID: claims.UserID,
		ConversationID: claims.ConversationID, RunID: claims.RunID,
		ProjectID: claims.ProjectID, RepositoryID: claims.RepositoryID,
		RegistrationID: claims.RegistrationID,
		ExpiresAt:      time.Unix(claims.ExpiresAt, 0).UTC(), CreatedAt: now,
	}); err != nil {
		return "", err
	}
	return credential, nil
}

func (s *Service) VerifyBrokerCredential(value string) (BrokerCredentialClaims, error) {
	if s == nil || s.box == nil {
		return BrokerCredentialClaims{}, ErrDisabled
	}
	return s.box.verifyBrokerCredential(strings.TrimSpace(value), s.now())
}

func (s *Service) AcquireTokenLease(
	ctx context.Context,
	credential string,
	command BrokerCommand,
) (BrokerDecision, error) {
	if !s.GitHubAgentWriteEnabled() {
		return BrokerDecision{}, ErrDisabled
	}
	started := s.now()
	claims, err := s.VerifyBrokerCredential(credential)
	if err != nil {
		return BrokerDecision{}, err
	}
	active, err := s.store.BrokerRunActive(ctx, claims, started)
	if err != nil {
		return BrokerDecision{}, err
	}
	if !active {
		return BrokerDecision{}, ErrRunInactive
	}
	id := Identity{TenantID: claims.TenantID, UserID: claims.UserID}
	workspace, projectValue, err := s.store.GetWorkspace(ctx, id, claims.ConversationID)
	if err != nil || workspace.ProjectID != claims.ProjectID || workspace.BranchName != claims.TaskBranch ||
		!brokerProjectReady(projectValue) ||
		projectValue.RepositoryExternalID != claims.RepositoryID ||
		projectValue.InstallationID != claims.InstallationID {
		return BrokerDecision{}, ErrRunInactive
	}
	registration, err := s.store.GetAppRegistration(ctx, id)
	if err != nil || registration.ID != claims.RegistrationID {
		return BrokerDecision{}, ErrConnectionRequired
	}
	connection, err := s.store.GetConnection(ctx, id)
	if err != nil || connection.Status != ConnectionReady ||
		connection.RegistrationID != claims.RegistrationID ||
		connection.InstallationID != claims.InstallationID {
		return BrokerDecision{}, ErrConnectionRequired
	}
	classification, digest, err := classifyBrokerCommand(command, claims.TaskBranch)
	if err != nil {
		return BrokerDecision{}, err
	}

	approvalID := ""
	if classification.highRisk {
		now := s.now()
		approval, approvalErr := s.store.GetOrCreateApproval(ctx, Approval{
			ID: uuid.NewString(), TenantID: claims.TenantID, UserID: claims.UserID,
			ConversationID: claims.ConversationID, RunID: claims.RunID,
			ProjectID: claims.ProjectID, RepositoryID: claims.RepositoryID,
			CommandDigest: digest, CommandCategory: classification.category,
			CommandLabel: classification.label, Permissions: classification.permissions,
			Status: "pending", ExpiresAt: now.Add(approvalTTL), CreatedAt: now, UpdatedAt: now,
		})
		if approvalErr != nil {
			return BrokerDecision{}, approvalErr
		}
		approvalID = approval.ID
		if approval.ExpiresAt.Before(now) && approval.Status == "pending" {
			approval, _ = s.store.ExpireApproval(ctx, id, approval.ID, now)
			return BrokerDecision{Status: "expired", ApprovalID: approval.ID,
				Category: classification.category, Label: classification.label,
				Permissions: classification.permissions}, ErrApprovalDenied
		}
		switch approval.Status {
		case "pending":
			return BrokerDecision{Status: "pending", ApprovalID: approval.ID,
				Category: classification.category, Label: classification.label,
				Permissions: classification.permissions}, ErrApprovalRequired
		case "approved":
			if _, claimErr := s.store.ClaimApproval(ctx, id, approval.ID, now); claimErr != nil {
				current, expireErr := s.store.ExpireApproval(ctx, id, approval.ID, now)
				if expireErr == nil && current.Status == "expired" {
					return BrokerDecision{Status: "expired", ApprovalID: approval.ID,
						Category: classification.category, Label: classification.label,
						Permissions: classification.permissions}, ErrApprovalDenied
				}
				if !errors.Is(claimErr, ErrNotFound) {
					return BrokerDecision{}, claimErr
				}
				return BrokerDecision{Status: "consumed", ApprovalID: approval.ID,
					Category: classification.category, Label: classification.label,
					Permissions: classification.permissions}, ErrApprovalDenied
			}
		case "denied", "expired", "consumed":
			return BrokerDecision{Status: approval.Status, ApprovalID: approval.ID,
				Category: classification.category, Label: classification.label,
				Permissions: classification.permissions}, ErrApprovalDenied
		default:
			return BrokerDecision{}, ErrConflict
		}
	}

	github, err := s.githubForRegistration(id, registration)
	if err != nil {
		return BrokerDecision{}, err
	}
	token, tokenExpiry, err := github.installationToken(ctx, claims.InstallationID,
		claims.RepositoryID, classification.permissions)
	if err != nil {
		s.auditBroker(ctx, claims, classification, "token_error", started)
		return BrokerDecision{}, err
	}
	lease := TokenLease{
		ID: uuid.NewString(), ApprovalID: approvalID, TenantID: claims.TenantID,
		UserID: claims.UserID, ConversationID: claims.ConversationID, RunID: claims.RunID,
		ProjectID: claims.ProjectID, RepositoryID: claims.RepositoryID,
		CommandCategory: classification.category, Permissions: classification.permissions,
		ExpiresAt: tokenExpiry, CreatedAt: s.now(),
	}
	lease.TokenCiphertext, err = s.box.encrypt(token, tokenLeaseAAD(lease))
	if err != nil {
		_ = github.revokeInstallationToken(ctx, token)
		return BrokerDecision{}, err
	}
	if err := s.store.SaveTokenLease(ctx, lease); err != nil {
		_ = github.revokeInstallationToken(ctx, token)
		return BrokerDecision{}, err
	}
	s.auditBroker(ctx, claims, classification, "issued", started)
	return BrokerDecision{
		Status: "ready", LeaseID: lease.ID, Token: token, ExpiresAt: tokenExpiry,
		ApprovalID: approvalID, Category: classification.category,
		Label: classification.label, Permissions: classification.permissions,
	}, nil
}

func brokerProjectReady(value Project) bool {
	if value.Status != ProjectReady || value.RepositoryExternalID <= 0 || value.InstallationID <= 0 {
		return false
	}
	return value.RepositoryProvider == ProviderGitHub ||
		(value.RepositoryProvider == ProviderLocal && value.GitHubPublishStatus == "published")
}

func (s *Service) RevokeTokenLease(
	ctx context.Context,
	credential string,
	leaseID string,
	completion LeaseCompletion,
) error {
	if _, err := uuid.Parse(leaseID); err != nil {
		return ErrInvalidArgument
	}
	result := strings.ToLower(strings.TrimSpace(completion.Result))
	if result == "" {
		result = "unknown"
	}
	if result != "success" && result != "failed" && result != "unknown" {
		return ErrInvalidArgument
	}
	claims, err := s.VerifyBrokerCredential(credential)
	if err != nil {
		return err
	}
	id := Identity{TenantID: claims.TenantID, UserID: claims.UserID}
	lease, err := s.store.GetTokenLease(ctx, id, claims.RunID, leaseID)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	registration, err := s.store.GetAppRegistration(ctx, id)
	if err != nil {
		return err
	}
	github, err := s.githubForRegistration(id, registration)
	if err != nil {
		return err
	}
	if err := s.revokeLease(ctx, id, github, lease); err != nil {
		return err
	}
	duration := s.now().Sub(lease.CreatedAt).Milliseconds()
	if duration < 0 {
		duration = 0
	}
	return s.store.SaveAuditEvent(ctx, AuditEvent{
		TenantID: lease.TenantID, UserID: lease.UserID, ProjectID: lease.ProjectID,
		RepositoryID: lease.RepositoryID, RunID: lease.RunID,
		CommandCategory: lease.CommandCategory, Permissions: lease.Permissions,
		Result: "command_" + result, DurationMS: duration, CreatedAt: s.now(),
	})
}

func (s *Service) RevokeRunTokenLeases(ctx context.Context, id Identity, runID string) error {
	if strings.TrimSpace(runID) == "" {
		return ErrInvalidArgument
	}
	leases, err := s.store.ListActiveTokenLeasesForRun(ctx, id, runID, s.now())
	if err != nil {
		return err
	}
	return s.revokeActiveLeases(ctx, id, leases)
}

func (s *Service) RevokeBrokerRun(ctx context.Context, id Identity, runID string) error {
	if strings.TrimSpace(runID) == "" {
		return ErrInvalidArgument
	}
	return s.store.RevokeBrokerRun(ctx, id, runID, s.now())
}

func (s *Service) revokeProjectTokenLeases(ctx context.Context, id Identity, projectID string) error {
	leases, err := s.store.ListActiveTokenLeasesForProject(ctx, id, projectID, s.now())
	if err != nil {
		return err
	}
	return s.revokeActiveLeases(ctx, id, leases)
}

func (s *Service) revokeUserTokenLeases(ctx context.Context, id Identity) error {
	leases, err := s.store.ListActiveTokenLeasesForUser(ctx, id, s.now())
	if err != nil {
		return err
	}
	return s.revokeActiveLeases(ctx, id, leases)
}

func (s *Service) revokeActiveLeases(ctx context.Context, id Identity, leases []TokenLease) error {
	if len(leases) == 0 {
		return nil
	}
	if s.box == nil {
		return s.markLeasesRevoked(ctx, id, leases)
	}
	registration, err := s.store.GetAppRegistration(ctx, id)
	if errors.Is(err, ErrNotFound) {
		return s.markLeasesRevoked(ctx, id, leases)
	}
	if err != nil {
		return err
	}
	github, err := s.githubForRegistration(id, registration)
	if err != nil {
		return err
	}
	var revokeErr error
	for _, lease := range leases {
		if err := s.revokeLease(ctx, id, github, lease); err != nil {
			revokeErr = errors.Join(revokeErr, err)
		}
	}
	return revokeErr
}

func (s *Service) markLeasesRevoked(ctx context.Context, id Identity, leases []TokenLease) error {
	var revokeErr error
	for _, lease := range leases {
		err := s.store.MarkTokenLeaseRevoked(ctx, id, lease.RunID, lease.ID, s.now())
		if err != nil && !errors.Is(err, ErrNotFound) {
			revokeErr = errors.Join(revokeErr, err)
		}
	}
	return revokeErr
}

func (s *Service) revokeLease(
	ctx context.Context,
	id Identity,
	github *githubClient,
	lease TokenLease,
) error {
	token, err := s.box.decrypt(lease.TokenCiphertext, tokenLeaseAAD(lease))
	if err != nil {
		return err
	}
	if err := github.revokeInstallationToken(ctx, token); err != nil {
		var httpErr *githubHTTPError
		if !errors.As(err, &httpErr) || (httpErr.Status != http.StatusNotFound && httpErr.Status != http.StatusUnauthorized) {
			return err
		}
	}
	err = s.store.MarkTokenLeaseRevoked(ctx, id, lease.RunID, lease.ID, s.now())
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
}

func (s *Service) ApprovalStatus(
	ctx context.Context,
	credential string,
	approvalID string,
) (Approval, error) {
	claims, err := s.VerifyBrokerCredential(credential)
	if err != nil {
		return Approval{}, err
	}
	value, err := s.store.GetApproval(ctx, approvalID)
	if err != nil {
		return Approval{}, err
	}
	if value.TenantID != claims.TenantID || value.UserID != claims.UserID ||
		value.RunID != claims.RunID || value.ConversationID != claims.ConversationID ||
		value.ProjectID != claims.ProjectID || value.RepositoryID != claims.RepositoryID {
		return Approval{}, ErrNotFound
	}
	if value.Status == "pending" && value.ExpiresAt.Before(s.now()) {
		return s.store.ExpireApproval(ctx, Identity{
			TenantID: claims.TenantID, UserID: claims.UserID,
		}, value.ID, s.now())
	}
	return value, nil
}

func (s *Service) DecideApproval(
	ctx context.Context,
	id Identity,
	approvalID string,
	decision string,
) (Approval, error) {
	if _, err := uuid.Parse(approvalID); err != nil {
		return Approval{}, ErrInvalidArgument
	}
	return s.store.DecideApproval(ctx, id, approvalID, decision, s.now())
}

func (s *Service) auditBroker(
	ctx context.Context,
	claims BrokerCredentialClaims,
	command classifiedCommand,
	result string,
	started time.Time,
) {
	_ = s.store.SaveAuditEvent(ctx, AuditEvent{
		TenantID: claims.TenantID, UserID: claims.UserID, ProjectID: claims.ProjectID,
		RepositoryID: claims.RepositoryID, RunID: claims.RunID,
		CommandCategory: command.category, Permissions: command.permissions,
		Result: result, DurationMS: time.Since(started).Milliseconds(), CreatedAt: s.now(),
	})
}

func classifyBrokerCommand(command BrokerCommand, taskBranch string) (classifiedCommand, string, error) {
	operation := strings.ToLower(strings.TrimSpace(command.Operation))
	requestID := strings.TrimSpace(command.RequestID)
	if _, err := uuid.Parse(requestID); err != nil {
		return classifiedCommand{}, "", ErrInvalidArgument
	}
	argv := make([]string, 0, len(command.Arguments))
	for _, value := range command.Arguments {
		value = strings.TrimSpace(value)
		if value == "" || strings.ContainsAny(value, "\x00\r\n") || len(value) > 4096 {
			return classifiedCommand{}, "", ErrInvalidArgument
		}
		argv = append(argv, value)
	}
	if (operation != "gh" && operation != "git") || len(argv) == 0 || len(argv) > 128 {
		return classifiedCommand{}, "", ErrInvalidArgument
	}
	raw, _ := json.Marshal(struct {
		Operation string   `json:"operation"`
		Arguments []string `json:"arguments"`
		RequestID string   `json:"request_id"`
	}{operation, argv, requestID})
	sum := sha256.Sum256(raw)
	digest := hex.EncodeToString(sum[:])
	if operation == "git" {
		return classifyGitPush(argv, taskBranch, digest)
	}
	return classifyGH(argv, command.DeclaredPermissions, digest)
}

func classifyGitPush(argv []string, taskBranch string, digest string) (classifiedCommand, string, error) {
	if argv[0] != "push" {
		return classifiedCommand{}, "", ErrInvalidArgument
	}
	expectedRefspec := "HEAD:refs/heads/" + taskBranch
	if strings.HasPrefix(taskBranch, "cocola/task-") && len(argv) == 3 &&
		argv[1] == "origin" && argv[2] == expectedRefspec {
		return classifiedCommand{category: "git_push", label: "Push the current Project task branch",
			permissions: map[string]string{"contents": "write"}}, digest, nil
	}
	// Any push other than the exact single task-branch refspec requires one-time
	// approval. This prevents an extra refspec from being smuggled alongside the
	// allowed task branch and covers force, delete, default-branch and ambiguous pushes.
	return classifiedCommand{category: "git_push", label: "Run a non-task-branch Git push",
		permissions: map[string]string{"contents": "write"}, highRisk: true}, digest, nil
}

func classifyGH(argv []string, declared []string, digest string) (classifiedCommand, string, error) {
	root := strings.ToLower(argv[0])
	action := ""
	if len(argv) > 1 && !strings.HasPrefix(argv[1], "-") {
		action = strings.ToLower(argv[1])
	}
	readActions := map[string]bool{
		"list": true, "view": true, "status": true, "checks": true, "diff": true,
		"watch": true, "download": true,
	}
	writeActions := map[string]bool{
		"create": true, "edit": true, "comment": true, "close": true, "reopen": true,
		"ready": true, "review": true, "rerun": true, "cancel": true, "run": true,
		"upload": true,
	}
	destructiveActions := map[string]bool{
		"merge": true, "delete": true, "archive": true, "rename": true, "set-default": true,
		"add": true, "remove": true,
	}
	resource := map[string]string{
		"repo": "contents", "pr": "pull_requests", "issue": "issues", "run": "actions",
		"workflow": "actions", "release": "contents", "cache": "actions",
	}[root]
	if root == "api" {
		permissions, err := parseDeclaredPermissions(declared)
		if err != nil || len(permissions) == 0 {
			return classifiedCommand{}, "", ErrInvalidArgument
		}
		return classifiedCommand{category: "gh_api", label: "Run a write-capable GitHub API request",
			permissions: permissions, highRisk: true}, digest, nil
	}
	if root == "repo" && (action == "edit" || destructiveActions[action]) {
		return classifiedCommand{category: "gh_repo_admin", label: "Modify repository settings",
			permissions: map[string]string{"administration": "write"}, highRisk: true}, digest, nil
	}
	if root == "secret" || root == "variable" || root == "key" || root == "webhook" || root == "ruleset" {
		permission := "administration"
		if root == "secret" {
			permission = "secrets"
		} else if root == "variable" {
			permission = "variables"
		} else if root == "webhook" {
			permission = "repository_hooks"
		}
		return classifiedCommand{category: "gh_" + root, label: "Modify sensitive repository settings",
			permissions: map[string]string{permission: "write"}, highRisk: true}, digest, nil
	}
	if resource == "" {
		return classifyDeclaredGHCommand(root, declared, digest)
	}
	if readActions[action] || (root == "repo" && action == "") {
		return classifiedCommand{category: "gh_" + root + "_read", label: "Read GitHub repository data",
			permissions: map[string]string{resource: "read"}}, digest, nil
	}
	if destructiveActions[action] {
		return classifiedCommand{category: "gh_" + root + "_admin", label: "Run a destructive repository operation",
			permissions: map[string]string{resource: "write"}, highRisk: true}, digest, nil
	}
	if writeActions[action] {
		return classifiedCommand{category: "gh_" + root + "_write", label: "Update GitHub repository data",
			permissions: map[string]string{resource: "write"}}, digest, nil
	}
	return classifyDeclaredGHCommand(root+" "+action, declared, digest)
}

func classifyDeclaredGHCommand(label string, declared []string, digest string) (classifiedCommand, string, error) {
	permissions, err := parseDeclaredPermissions(declared)
	if err != nil || len(permissions) == 0 {
		return classifiedCommand{}, "", ErrInvalidArgument
	}
	return classifiedCommand{
		category: "gh_custom", label: "Run a declared GitHub operation (" + strings.TrimSpace(label) + ")",
		permissions: permissions, highRisk: true,
	}, digest, nil
}

func parseDeclaredPermissions(values []string) (map[string]string, error) {
	allowed := map[string]bool{
		"actions": true, "administration": true, "checks": true, "contents": true,
		"deployments": true, "environments": true, "issues": true, "metadata": true,
		"packages": true, "pages": true, "pull_requests": true, "repository_hooks": true,
		"secret_scanning_alerts": true, "secrets": true, "security_events": true,
		"statuses": true, "variables": true, "vulnerability_alerts": true, "workflows": true,
	}
	result := make(map[string]string)
	for _, value := range values {
		name, level, ok := strings.Cut(strings.ToLower(strings.TrimSpace(value)), "=")
		if !ok || !allowed[name] || (level != "read" && level != "write") {
			return nil, ErrInvalidArgument
		}
		result[name] = level
	}
	return result, nil
}
