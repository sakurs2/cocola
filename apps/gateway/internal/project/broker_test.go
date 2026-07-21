package project

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

const brokerTestRequestID = "d6a5efdb-9bc8-4eec-b768-e8668b24212d"

type brokerLifecycleStore struct {
	Store
	active       bool
	revokedRunID string
	markedLeases []string
}

func (s *brokerLifecycleStore) BrokerRunActive(
	_ context.Context,
	_ BrokerCredentialClaims,
	_ time.Time,
) (bool, error) {
	return s.active, nil
}

func (s *brokerLifecycleStore) RevokeBrokerRun(
	_ context.Context,
	_ Identity,
	runID string,
	_ time.Time,
) error {
	s.revokedRunID = runID
	return nil
}

func (s *brokerLifecycleStore) MarkTokenLeaseRevoked(
	_ context.Context,
	_ Identity,
	_ string,
	leaseID string,
	_ time.Time,
) error {
	s.markedLeases = append(s.markedLeases, leaseID)
	return nil
}

func TestAcquireTokenLeaseRejectsRevokedDurableBrokerRun(t *testing.T) {
	now := time.Date(2026, 7, 22, 1, 0, 0, 0, time.UTC)
	box := testSecretBox(t)
	credential, err := box.signBrokerCredential(BrokerCredentialClaims{
		TenantID: "tenant-a", UserID: "user-a", ConversationID: "conversation-a",
		RunID: "run-a", ProjectID: "11111111-1111-1111-1111-111111111111",
		RepositoryID: 42, RepositoryFullName: "owner/repository", InstallationID: 7,
		RegistrationID: "22222222-2222-2222-2222-222222222222",
		TaskBranch:     "cocola/task-abcd", ExpiresAt: now.Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{
		store: &brokerLifecycleStore{}, box: box, now: func() time.Time { return now },
		githubAgentWriteEnabled: true,
	}
	_, err = service.AcquireTokenLease(context.Background(), credential, BrokerCommand{
		Operation: "gh", Arguments: []string{"repo", "view"}, RequestID: brokerTestRequestID,
	})
	if !errors.Is(err, ErrRunInactive) {
		t.Fatalf("AcquireTokenLease error = %v, want ErrRunInactive", err)
	}
}

func TestBrokerCleanupWithoutSecretClosesLocalState(t *testing.T) {
	store := &brokerLifecycleStore{}
	service := &Service{store: store, now: func() time.Time { return time.Now().UTC() }}
	id := Identity{TenantID: "tenant-a", UserID: "user-a"}
	if err := service.revokeActiveLeases(context.Background(), id, []TokenLease{
		{ID: "11111111-1111-1111-1111-111111111111", RunID: "run-a"},
	}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(store.markedLeases, []string{"11111111-1111-1111-1111-111111111111"}) {
		t.Fatalf("marked leases = %#v", store.markedLeases)
	}
	if err := service.RevokeBrokerRun(context.Background(), id, "run-a"); err != nil {
		t.Fatal(err)
	}
	if store.revokedRunID != "run-a" {
		t.Fatalf("revoked run = %q", store.revokedRunID)
	}
	if _, err := service.VerifyBrokerCredential("credential"); !errors.Is(err, ErrDisabled) {
		t.Fatalf("VerifyBrokerCredential error = %v, want ErrDisabled", err)
	}
	if _, err := service.githubForRegistration(id, AppRegistration{}); !errors.Is(err, ErrDisabled) {
		t.Fatalf("githubForRegistration error = %v, want ErrDisabled", err)
	}
}

func TestClassifyBrokerCommandUsesMinimumKnownPermissions(t *testing.T) {
	tests := []struct {
		name        string
		arguments   []string
		category    string
		permissions map[string]string
		highRisk    bool
	}{
		{"read pull request", []string{"pr", "view", "42"}, "gh_pr_read", map[string]string{"pull_requests": "read"}, false},
		{"create issue", []string{"issue", "create", "--title", "Bug"}, "gh_issue_write", map[string]string{"issues": "write"}, false},
		{"upload release", []string{"release", "upload", "v1", "artifact.tgz"}, "gh_release_write", map[string]string{"contents": "write"}, false},
		{"manage secret", []string{"secret", "set", "TOKEN"}, "gh_secret", map[string]string{"secrets": "write"}, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, digest, err := classifyBrokerCommand(BrokerCommand{
				Operation: "gh", Arguments: test.arguments, RequestID: brokerTestRequestID,
			}, "cocola/task-abcd")
			if err != nil {
				t.Fatalf("classifyBrokerCommand: %v", err)
			}
			if result.category != test.category || result.highRisk != test.highRisk ||
				!reflect.DeepEqual(result.permissions, test.permissions) || len(digest) != 64 {
				t.Fatalf("classification = %#v, digest=%q", result, digest)
			}
		})
	}
}

func TestClassifyBrokerCommandRequiresExplicitAPIAndGitScopes(t *testing.T) {
	if _, _, err := classifyBrokerCommand(BrokerCommand{
		Operation: "gh", Arguments: []string{"api", "repos/owner/repo"},
		RequestID: brokerTestRequestID,
	}, "cocola/task-abcd"); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("gh api without declared permissions error = %v", err)
	}
	api, _, err := classifyBrokerCommand(BrokerCommand{
		Operation: "gh", Arguments: []string{"api", "repos/owner/repo"},
		DeclaredPermissions: []string{"contents=read"},
		RequestID:           brokerTestRequestID,
	}, "cocola/task-abcd")
	if err != nil || !api.highRisk || api.permissions["contents"] != "read" {
		t.Fatalf("declared gh api classification = %#v, %v", api, err)
	}

	branch := "cocola/task-abcd"
	push, _, err := classifyBrokerCommand(BrokerCommand{
		Operation: "git", Arguments: []string{"push", "origin", "HEAD:refs/heads/" + branch},
		RequestID: brokerTestRequestID,
	}, branch)
	if err != nil || push.highRisk || push.permissions["contents"] != "write" {
		t.Fatalf("task branch push classification = %#v, %v", push, err)
	}
	implicit, _, err := classifyBrokerCommand(BrokerCommand{
		Operation: "git", Arguments: []string{"push", "origin", "HEAD"},
		RequestID: brokerTestRequestID,
	}, branch)
	if err != nil || !implicit.highRisk {
		t.Fatalf("implicit git target classification = %#v, %v", implicit, err)
	}
	force, _, err := classifyBrokerCommand(BrokerCommand{
		Operation: "git", Arguments: []string{"push", "--force", "origin", "HEAD:refs/heads/" + branch},
		RequestID: brokerTestRequestID,
	}, branch)
	if err != nil || !force.highRisk {
		t.Fatalf("force push classification = %#v, %v", force, err)
	}
	mainPush, _, err := classifyBrokerCommand(BrokerCommand{
		Operation: "git", Arguments: []string{"push", "origin", "HEAD:refs/heads/main"},
		RequestID: brokerTestRequestID,
	}, "main")
	if err != nil || !mainPush.highRisk || mainPush.permissions["contents"] != "write" {
		t.Fatalf("local main push classification = %#v, %v", mainPush, err)
	}
	multiple, _, err := classifyBrokerCommand(BrokerCommand{
		Operation: "git", Arguments: []string{
			"push", "origin", "HEAD:refs/heads/" + branch, "HEAD:refs/heads/backdoor",
		}, RequestID: brokerTestRequestID,
	}, branch)
	if err != nil || !multiple.highRisk {
		t.Fatalf("multi-refspec push classification = %#v, %v", multiple, err)
	}
	prefix, _, err := classifyBrokerCommand(BrokerCommand{
		Operation: "git", Arguments: []string{
			"push", "origin", "HEAD:refs/heads/" + branch + "-other",
		}, RequestID: brokerTestRequestID,
	}, branch)
	if err != nil || !prefix.highRisk {
		t.Fatalf("task-branch prefix push classification = %#v, %v", prefix, err)
	}
}

func TestBrokerCommandDigestBindsExactArguments(t *testing.T) {
	_, first, err := classifyBrokerCommand(BrokerCommand{
		Operation: "gh", Arguments: []string{"issue", "delete", "1"},
		RequestID: brokerTestRequestID,
	}, "cocola/task-abcd")
	if err != nil {
		t.Fatal(err)
	}
	_, second, err := classifyBrokerCommand(BrokerCommand{
		Operation: "gh", Arguments: []string{"issue", "delete", "2"},
		RequestID: brokerTestRequestID,
	}, "cocola/task-abcd")
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("different command arguments produced the same approval digest")
	}
}

func TestBrokerCommandDigestBindsInvocationAndDeclaredFallback(t *testing.T) {
	_, first, err := classifyBrokerCommand(BrokerCommand{
		Operation: "gh", Arguments: []string{"repo", "view"}, RequestID: brokerTestRequestID,
	}, "cocola/task-abcd")
	if err != nil {
		t.Fatal(err)
	}
	_, second, err := classifyBrokerCommand(BrokerCommand{
		Operation: "gh", Arguments: []string{"repo", "view"},
		RequestID: "1c0e076d-2839-494e-86c9-9f20aca12cfd",
	}, "cocola/task-abcd")
	if err != nil || first == second {
		t.Fatalf("invocation digest was not unique: first=%q second=%q err=%v", first, second, err)
	}

	custom, _, err := classifyBrokerCommand(BrokerCommand{
		Operation: "gh", Arguments: []string{"workflow", "enable", "ci.yml"},
		DeclaredPermissions: []string{"actions=write"}, RequestID: brokerTestRequestID,
	}, "cocola/task-abcd")
	if err != nil || !custom.highRisk || custom.category != "gh_custom" ||
		custom.permissions["actions"] != "write" {
		t.Fatalf("declared fallback = %#v, %v", custom, err)
	}
}

func TestRepositoryAdministrationAndVariablesAreHighRisk(t *testing.T) {
	repoEdit, _, err := classifyBrokerCommand(BrokerCommand{
		Operation: "gh", Arguments: []string{"repo", "edit", "--visibility", "public"},
		RequestID: brokerTestRequestID,
	}, "cocola/task-abcd")
	if err != nil || !repoEdit.highRisk || repoEdit.permissions["administration"] != "write" {
		t.Fatalf("repo edit classification = %#v, %v", repoEdit, err)
	}
	variable, _, err := classifyBrokerCommand(BrokerCommand{
		Operation: "gh", Arguments: []string{"variable", "set", "DEPLOY_ENV"},
		RequestID: brokerTestRequestID,
	}, "cocola/task-abcd")
	if err != nil || !variable.highRisk || variable.permissions["variables"] != "write" {
		t.Fatalf("variable classification = %#v, %v", variable, err)
	}
}
