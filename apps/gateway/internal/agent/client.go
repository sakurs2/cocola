// Package agent is the gateway's client to the agent-runtime gRPC service. It
// exposes a narrow Streamer interface so the HTTP/SSE layer depends on an
// abstraction (real gRPC in prod, a fake in tests) rather than the generated
// stubs directly.
package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/cocola-project/cocola/packages/go-common/tracing"
	agentv1 "github.com/cocola-project/cocola/packages/proto/gen/go/cocola/agent/v1"
)

// Event is the transport-neutral shape the HTTP layer streams to the client. It
// mirrors the proto AgentEvent (a kind + flat string map) without leaking the
// generated type past this package.
type Event struct {
	Kind string            `json:"kind"`
	Data map[string]string `json:"data,omitempty"`
}

func IsRuntimeInterruption(err error) bool {
	if err == nil {
		return false
	}
	switch status.Code(err) {
	case codes.Unavailable, codes.Canceled:
		return true
	default:
		return false
	}
}

// Query is the resolved request the gateway forwards to agent-runtime. The
// caller (HTTP layer) fills UserID/SessionId from the verified identity, never
// from client-supplied fields.
type Query struct {
	UserID              string
	SessionID           string
	RuntimeID           string
	SkillID             string
	Prompt              string
	SandboxID           string
	AllowWorkspaceReset bool
	MemoryContext       string
	MaxTurns            int32
	ModelRouteID        string
	TraceID             string
	ParentSpanID        string
	// SandboxAuthToken is a fresh per-user cocola token the gateway mints from
	// the verified identity (sub=UserID, ten=TenantID) for THIS turn. It is
	// forwarded to agent-runtime over gRPC metadata and injected into the
	// sandbox as ANTHROPIC_AUTH_TOKEN so the in-sandbox brain calls the
	// llm-gateway as the real user (per-user quota / usage / revocation),
	// replacing static cluster-wide credentials. Empty is supported by tests
	// with an unauthenticated fake runtime; production config always wires an issuer.
	SandboxAuthToken        string
	SCMToken                string
	ProjectBrokerCredential string
	Project                 *ProjectContext
	Attachments             []Attachment
}

type ProjectContext struct {
	ProjectID          string
	RepositoryID       int64
	CloneURL           string
	DefaultBranch      string
	BaseRef            string
	BaseSHA            string
	TaskBranch         string
	GitAuthorName      string
	GitAuthorEmail     string
	RepositoryProvider string
	RepositoryFullName string
	CredentialMode     string
}

type GitChange struct {
	Path, OldPath, Status, Area string
}

type GitCommit struct {
	SHA, Subject, AuthorName, AuthoredAt, Body string
	Parents, Refs                              []string
	FilesChanged, Additions, Deletions         int
}

type GitCommitFile struct {
	Path, OldPath, Status string
	Binary                bool
}

type GitSnapshot struct {
	Branch, BaseRef, BaseSHA, HeadSHA  string
	Ahead                              int
	Dirty, Truncated, HistoryTruncated bool
	Changes                            []GitChange
	Commits                            []GitCommit
}

type GitInspection struct {
	Snapshot          GitSnapshot
	Commit            *GitCommit
	CommitFiles       []GitCommitFile
	Diff              string
	Binary, Truncated bool
}

type InspectRequest struct {
	UserID, SessionID, Operation, Path, DiffTarget, CommitSHA, SCMToken string
	Project                                                             ProjectContext
}

type GitInspector interface {
	InspectWorkspaceGit(context.Context, InspectRequest) (GitInspection, error)
}

type PublishRequest struct {
	UserID, SessionID, SCMToken, RemoteCloneURL, ExpectedHeadSHA string
	Project                                                      ProjectContext
}

type GitPublisher interface {
	PublishWorkspaceGit(context.Context, PublishRequest) (string, error)
}

// Attachment is one user-uploaded file forwarded to agent-runtime. Content is
// raw bytes (already base64-decoded at the HTTP edge), mapping onto the proto
// `bytes` field so binaries survive intact.
type Attachment struct {
	Filename string
	Content  []byte
	Mime     string
	// OssKey is the object-storage key (source of truth) set for every upload
	// once MinIO is configured. Size is the original byte length. For large
	// files Content is empty and agent-runtime pulls the bytes via OssKey
	// (ADR-0017 P1a); for small files both Content and OssKey are populated.
	OssKey string
	Size   int64
}

// Streamer runs one agent query and pushes each event to onEvent in order. It
// returns when the stream ends (nil) or on the first transport/agent error. The
// abstraction is what makes the SSE handler unit-testable without a real server.
type Streamer interface {
	Stream(ctx context.Context, q Query, onEvent func(Event) error) error
}

// Releaser frees runtime resources bound to a cocola session. Callers treat
// errors as best-effort because deleting durable conversation history must not
// be blocked by a transient runtime release failure.
type Releaser interface {
	ReleaseSession(ctx context.Context, userID, sessionID string) error
}

// Runtime describes one built-in Agent Runtime. IDs and model protocols are
// stable product contracts; labels are display-only.
type Runtime struct {
	ID            string `json:"id"`
	Label         string `json:"label"`
	ModelProtocol string `json:"model_protocol"`
	IsDefault     bool   `json:"is_default"`
}

// Client is the gRPC-backed Streamer. Build it with Dial.
type Client struct {
	conn *grpc.ClientConn
	rpc  agentv1.AgentRuntimeServiceClient
}

// defaultMaxMessageBytes is 64 MiB -- comfortably above the 32 MiB frontend
// upload cap, leaving headroom for base64/proto framing overhead.
const defaultMaxMessageBytes = 64 * 1024 * 1024

// maxMessageBytes resolves the configured gRPC single-message ceiling. A
// non-positive/invalid COCOLA_GRPC_MAX_MESSAGE_BYTES falls back to the default.
func maxMessageBytes() int {
	if v := os.Getenv("COCOLA_GRPC_MAX_MESSAGE_BYTES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return defaultMaxMessageBytes
}

// Dial opens a lazy connection to the agent-runtime at addr (it connects on
// first RPC, not here). The connection
// is plaintext: agent-runtime is an internal service reached over the cluster
// network, not the public internet (TLS/mTLS is an M6 hardening item).
func Dial(addr string) (*Client, error) {
	// Trace propagation: the client stats handler injects the current span's
	// W3C traceparent into outbound gRPC metadata, carrying the trace from the
	// gateway into agent-runtime. No-op when tracing is disabled.
	// Raise the single-message ceiling above gRPC's 4 MiB default so inline
	// attachment bytes (up to the ADR-0017 split threshold) are not rejected
	// as ResourceExhausted on the way to agent-runtime. This is a transport
	// safety cap, distinct from the inline/backend-pull split threshold;
	// configurable via COCOLA_GRPC_MAX_MESSAGE_BYTES (default 64 MiB).
	maxMsg := maxMessageBytes()
	conn, err := grpc.Dial(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		tracing.GRPCClientDialOption(),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(maxMsg),
			grpc.MaxCallSendMsgSize(maxMsg),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("agent: dial %q: %w", addr, err)
	}
	return NewClient(conn), nil
}

// NewClient wraps an already-established gRPC connection. Dial uses it for the
// production path; tests inject a bufconn-backed conn to exercise the real stub
// + streaming serialization without binding a port.
func NewClient(conn *grpc.ClientConn) *Client {
	return &Client{conn: conn, rpc: agentv1.NewAgentRuntimeServiceClient(conn)}
}

// Close releases the underlying connection.
func (c *Client) Close() error {
	if c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

// Stream forwards q to agent-runtime and relays each AgentEvent to onEvent. A
// context cancel (client disconnect, deadline) aborts the RPC promptly.
func (c *Client) Stream(ctx context.Context, q Query, onEvent func(Event) error) error {
	if strings.TrimSpace(q.ModelRouteID) != "" {
		ctx = metadata.AppendToOutgoingContext(ctx, "x-cocola-model-route-id", strings.TrimSpace(q.ModelRouteID))
	}
	// Per-user sandbox token: carry it as gRPC metadata (same seam as the model
	// route ID) so agent-runtime can inject it as ANTHROPIC_AUTH_TOKEN per turn
	// without a proto change. Never logged; treated as a credential.
	if strings.TrimSpace(q.SandboxAuthToken) != "" {
		ctx = metadata.AppendToOutgoingContext(ctx, "x-cocola-sandbox-token", strings.TrimSpace(q.SandboxAuthToken))
	}
	if strings.TrimSpace(q.SCMToken) != "" {
		ctx = metadata.AppendToOutgoingContext(ctx, "x-cocola-scm-token", strings.TrimSpace(q.SCMToken))
	}
	if strings.TrimSpace(q.ProjectBrokerCredential) != "" {
		ctx = metadata.AppendToOutgoingContext(ctx, "x-cocola-project-broker-credential",
			strings.TrimSpace(q.ProjectBrokerCredential))
	}
	if len(q.TraceID) == 32 && len(q.ParentSpanID) == 16 {
		// otelgrpc owns the standard traceparent key and may replace it with its
		// transport span. Keep the product parent explicit so model.generate is
		// attached to agent.execute rather than to an unpersisted OTel span.
		ctx = metadata.AppendToOutgoingContext(ctx, "x-cocola-product-traceparent", "00-"+q.TraceID+"-"+q.ParentSpanID+"-01")
	}
	atts := make([]*agentv1.Attachment, 0, len(q.Attachments))
	for i := range q.Attachments {
		atts = append(atts, &agentv1.Attachment{
			Filename: q.Attachments[i].Filename,
			Content:  q.Attachments[i].Content,
			Mime:     q.Attachments[i].Mime,
			OssKey:   q.Attachments[i].OssKey,
			Size:     q.Attachments[i].Size,
		})
	}
	request := &agentv1.QueryRequest{
		UserId:              q.UserID,
		SessionId:           q.SessionID,
		Prompt:              q.Prompt,
		SandboxId:           q.SandboxID,
		MaxTurns:            q.MaxTurns,
		Attachments:         atts,
		RuntimeId:           q.RuntimeID,
		SkillId:             q.SkillID,
		AllowWorkspaceReset: q.AllowWorkspaceReset,
		MemoryContext:       q.MemoryContext,
	}
	if q.Project != nil {
		request.ProjectContext = projectContextProto(*q.Project)
	}
	stream, err := c.rpc.Query(ctx, request)
	if err != nil {
		return fmt.Errorf("agent: query: %w", err)
	}
	for {
		msg, err := stream.Recv()
		if err != nil {
			// io.EOF is the normal end-of-stream; surface it as nil.
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		ev := Event{Kind: msg.GetKind(), Data: msg.GetData()}
		if err := onEvent(ev); err != nil {
			return err
		}
	}
}

func (c *Client) InspectWorkspaceGit(ctx context.Context, request InspectRequest) (GitInspection, error) {
	if strings.TrimSpace(request.SCMToken) != "" {
		ctx = metadata.AppendToOutgoingContext(ctx, "x-cocola-scm-token", strings.TrimSpace(request.SCMToken))
	}
	response, err := c.rpc.InspectWorkspaceGit(ctx, &agentv1.InspectWorkspaceGitRequest{
		UserId: request.UserID, SessionId: request.SessionID, Operation: request.Operation,
		Path: request.Path, DiffTarget: request.DiffTarget, CommitSha: request.CommitSHA,
		ProjectContext: projectContextProto(request.Project),
	})
	if err != nil {
		return GitInspection{}, fmt.Errorf("agent: inspect workspace git: %w", err)
	}
	result := GitInspection{Diff: response.GetDiff(), Binary: response.GetBinary(), Truncated: response.GetTruncated()}
	if snapshot := response.GetSnapshot(); snapshot != nil {
		result.Snapshot = GitSnapshot{
			Branch: snapshot.GetBranch(), BaseRef: snapshot.GetBaseRef(), BaseSHA: snapshot.GetBaseSha(), HeadSHA: snapshot.GetHeadSha(),
			Ahead: int(snapshot.GetAhead()), Dirty: snapshot.GetDirty(), Truncated: snapshot.GetTruncated(),
			HistoryTruncated: snapshot.GetHistoryTruncated(),
		}
		for _, change := range snapshot.GetChanges() {
			result.Snapshot.Changes = append(result.Snapshot.Changes, GitChange{
				Path: change.GetPath(), OldPath: change.GetOldPath(), Status: change.GetStatus(), Area: change.GetArea(),
			})
		}
		for _, commit := range snapshot.GetCommits() {
			result.Snapshot.Commits = append(result.Snapshot.Commits, gitCommitFromProto(commit))
		}
	}
	if commit := response.GetCommit(); commit != nil && commit.GetSha() != "" {
		value := gitCommitFromProto(commit)
		result.Commit = &value
	}
	for _, value := range response.GetCommitFiles() {
		result.CommitFiles = append(result.CommitFiles, GitCommitFile{
			Path: value.GetPath(), OldPath: value.GetOldPath(), Status: value.GetStatus(), Binary: value.GetBinary(),
		})
	}
	return result, nil
}

func gitCommitFromProto(value *agentv1.GitCommit) GitCommit {
	return GitCommit{
		SHA: value.GetSha(), Parents: append([]string(nil), value.GetParents()...),
		Subject: value.GetSubject(), AuthorName: value.GetAuthorName(), AuthoredAt: value.GetAuthoredAt(),
		Refs: append([]string(nil), value.GetRefs()...), FilesChanged: int(value.GetFilesChanged()),
		Additions: int(value.GetAdditions()), Deletions: int(value.GetDeletions()), Body: value.GetBody(),
	}
}

func (c *Client) PublishWorkspaceGit(ctx context.Context, request PublishRequest) (string, error) {
	if strings.TrimSpace(request.SCMToken) != "" {
		ctx = metadata.AppendToOutgoingContext(ctx, "x-cocola-scm-token", strings.TrimSpace(request.SCMToken))
	}
	response, err := c.rpc.PublishWorkspaceGit(ctx, &agentv1.PublishWorkspaceGitRequest{
		UserId: request.UserID, SessionId: request.SessionID,
		ProjectContext: projectContextProto(request.Project),
		RemoteCloneUrl: request.RemoteCloneURL, ExpectedHeadSha: request.ExpectedHeadSHA,
	})
	if err != nil {
		return "", fmt.Errorf("agent: publish workspace git: %w", err)
	}
	if strings.TrimSpace(response.GetHeadSha()) == "" {
		return "", errors.New("agent: publish workspace git returned an empty HEAD")
	}
	return response.GetHeadSha(), nil
}

func projectContextProto(value ProjectContext) *agentv1.ProjectContext {
	return &agentv1.ProjectContext{
		ProjectId: value.ProjectID, RepositoryId: value.RepositoryID, CloneUrl: value.CloneURL,
		DefaultBranch: value.DefaultBranch, BaseSha: value.BaseSHA, TaskBranch: value.TaskBranch,
		GitAuthorName: value.GitAuthorName, GitAuthorEmail: value.GitAuthorEmail,
		RepositoryProvider: value.RepositoryProvider, RepositoryFullName: value.RepositoryFullName,
		CredentialMode: value.CredentialMode, BaseRef: value.BaseRef,
	}
}

// ListRuntimes fetches the authoritative built-in runtime catalog. The
// gateway calls this once during startup and refuses to serve chat if the
// catalog is unavailable or malformed.
func (c *Client) ListRuntimes(ctx context.Context) ([]Runtime, error) {
	response, err := c.rpc.ListRuntimes(ctx, &agentv1.ListRuntimesRequest{})
	if err != nil {
		return nil, fmt.Errorf("agent: list runtimes: %w", err)
	}
	runtimes := make([]Runtime, 0, len(response.GetRuntimes()))
	seen := make(map[string]struct{}, len(response.GetRuntimes()))
	defaults := 0
	for _, runtime := range response.GetRuntimes() {
		item := Runtime{
			ID:            strings.TrimSpace(runtime.GetId()),
			Label:         strings.TrimSpace(runtime.GetLabel()),
			ModelProtocol: strings.TrimSpace(runtime.GetModelProtocol()),
			IsDefault:     runtime.GetIsDefault(),
		}
		if item.ID == "" || item.Label == "" || item.ModelProtocol == "" {
			return nil, fmt.Errorf("agent: invalid runtime descriptor")
		}
		if _, ok := seen[item.ID]; ok {
			return nil, fmt.Errorf("agent: duplicate runtime %q", item.ID)
		}
		seen[item.ID] = struct{}{}
		if item.IsDefault {
			defaults++
		}
		runtimes = append(runtimes, item)
	}
	if len(runtimes) == 0 || defaults != 1 {
		return nil, fmt.Errorf("agent: runtime catalog must contain exactly one default")
	}
	return runtimes, nil
}

// ReleaseSession asks agent-runtime to free any sandbox/resume state for a
// conversation. Ownership is still enforced by the gateway before this call.
func (c *Client) ReleaseSession(ctx context.Context, userID, sessionID string) error {
	_, err := c.rpc.ReleaseSession(ctx, &agentv1.ReleaseSessionRequest{
		UserId:    userID,
		SessionId: sessionID,
	})
	if err != nil {
		return fmt.Errorf("agent: release session: %w", err)
	}
	return nil
}
