package project

import (
	"context"
	"strings"
	"time"
)

type providerChangeRequest struct {
	Number    int64
	URL       string
	Status    string
	ErrorCode string
	HeadSHA   string
	MergedAt  *time.Time
}

// RepositoryProvider is the provider-neutral delivery boundary used by the
// Project domain. Credential acquisition and repository provisioning remain in
// Service because GitHub credentials are user-installation scoped while Local
// credentials are Project scoped.
type RepositoryProvider interface {
	CreateChangeRequest(context.Context, string, Project, Workspace, string) (providerChangeRequest, error)
	GetChangeRequestStatus(context.Context, string, Project, int64) (providerChangeRequest, error)
	SquashMerge(context.Context, string, Project, int64, string, string) (string, error)
	DeleteTaskBranch(context.Context, string, Project, string) error
}

type forgejoRepositoryProvider struct{ client *forgejoClient }

func (p forgejoRepositoryProvider) CreateChangeRequest(
	ctx context.Context, token string, project Project, workspace Workspace, title string,
) (providerChangeRequest, error) {
	pull, err := p.client.createPull(ctx, token, project, workspace, title)
	return providerChangeRequest{
		Number: pull.Number, Status: "open", HeadSHA: pull.Head.SHA,
	}, err
}

func (p forgejoRepositoryProvider) GetChangeRequestStatus(
	ctx context.Context, token string, project Project, number int64,
) (providerChangeRequest, error) {
	pull, err := p.client.pull(ctx, token, project, number)
	if err != nil {
		return providerChangeRequest{}, err
	}
	result := providerChangeRequest{Number: pull.Number, Status: "open", HeadSHA: pull.Head.SHA}
	switch {
	case pull.Merged:
		result.Status, result.MergedAt = "merged", pull.MergedAt
	case pull.State == "closed":
		result.Status = "closed"
	case !pull.Mergeable:
		result.Status = "conflict"
	}
	return result, nil
}

func (p forgejoRepositoryProvider) SquashMerge(
	ctx context.Context, token string, project Project, number int64, expectedHead, title string,
) (string, error) {
	return p.client.mergePull(ctx, token, project, number, expectedHead, title)
}

func (p forgejoRepositoryProvider) DeleteTaskBranch(
	ctx context.Context, token string, project Project, branch string,
) error {
	return p.client.deleteBranch(ctx, token, project, branch)
}

type githubRepositoryProvider struct{ client *githubClient }

func (p githubRepositoryProvider) CreateChangeRequest(
	ctx context.Context, token string, project Project, workspace Workspace, title string,
) (providerChangeRequest, error) {
	pull, err := p.client.createPull(ctx, token, project, workspace, title)
	return providerChangeRequest{
		Number: pull.Number, URL: pull.HTMLURL, Status: "open", HeadSHA: pull.Head.SHA,
	}, err
}

func (p githubRepositoryProvider) GetChangeRequestStatus(
	ctx context.Context, token string, project Project, number int64,
) (providerChangeRequest, error) {
	pull, err := p.client.pull(ctx, token, project, number)
	if err != nil {
		return providerChangeRequest{}, err
	}
	result := providerChangeRequest{
		Number: pull.Number, URL: pull.HTMLURL, Status: "open", HeadSHA: pull.Head.SHA,
	}
	switch {
	case pull.Merged:
		result.Status, result.MergedAt = "merged", pull.MergedAt
		return result, nil
	case pull.State == "closed":
		result.Status = "closed"
		return result, nil
	case pull.Mergeable == nil:
		result.Status = "checks_pending"
		return result, nil
	case !*pull.Mergeable:
		result.Status = "conflict"
		return result, nil
	}
	pending, failed, err := p.client.commitChecks(ctx, token, project, pull.Head.SHA)
	if err != nil {
		return providerChangeRequest{}, err
	}
	if failed {
		result.Status, result.ErrorCode = "failed", "CHECKS_FAILED"
	} else if pending || pull.Draft || githubMergeabilityPending(pull.MergeableState) {
		result.Status = "checks_pending"
	}
	return result, nil
}

func githubMergeabilityPending(state string) bool {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "blocked", "behind", "draft", "unstable", "unknown":
		return true
	default:
		return false
	}
}

func (p githubRepositoryProvider) SquashMerge(
	ctx context.Context, token string, project Project, number int64, expectedHead, title string,
) (string, error) {
	return p.client.mergePull(ctx, token, project, number, expectedHead, title)
}

func (p githubRepositoryProvider) DeleteTaskBranch(
	ctx context.Context, token string, project Project, branch string,
) error {
	return p.client.deleteBranch(ctx, token, project, branch)
}
