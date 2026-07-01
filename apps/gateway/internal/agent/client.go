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

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

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

// Query is the resolved request the gateway forwards to agent-runtime. The
// caller (HTTP layer) fills UserID/SessionId from the verified identity, never
// from client-supplied fields.
type Query struct {
	UserID      string
	SessionID   string
	Prompt      string
	SandboxID   string
	MaxTurns    int32
	Attachments []Attachment
}

// Attachment is one user-uploaded file forwarded to agent-runtime. Content is
// raw bytes (already base64-decoded at the HTTP edge), mapping onto the proto
// `bytes` field so binaries survive intact.
type Attachment struct {
	Filename string
	Content  []byte
	Mime     string
}

// Streamer runs one agent query and pushes each event to onEvent in order. It
// returns when the stream ends (nil) or on the first transport/agent error. The
// abstraction is what makes the SSE handler unit-testable without a real server.
type Streamer interface {
	Stream(ctx context.Context, q Query, onEvent func(Event) error) error
}

// Client is the gRPC-backed Streamer. Build it with Dial.
type Client struct {
	conn *grpc.ClientConn
	rpc  agentv1.AgentRuntimeServiceClient
}

// Dial opens a lazy connection to the agent-runtime at addr (it connects on
// first RPC, not here). The connection
// is plaintext: agent-runtime is an internal service reached over the cluster
// network, not the public internet (TLS/mTLS is an M6 hardening item).
func Dial(addr string) (*Client, error) {
	// Trace propagation: the client stats handler injects the current span's
	// W3C traceparent into outbound gRPC metadata, carrying the trace from the
	// gateway into agent-runtime. No-op when tracing is disabled.
	conn, err := grpc.Dial(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		tracing.GRPCClientDialOption(),
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
	atts := make([]*agentv1.Attachment, 0, len(q.Attachments))
	for i := range q.Attachments {
		atts = append(atts, &agentv1.Attachment{
			Filename: q.Attachments[i].Filename,
			Content:  q.Attachments[i].Content,
			Mime:     q.Attachments[i].Mime,
		})
	}
	stream, err := c.rpc.Query(ctx, &agentv1.QueryRequest{
		UserId:      q.UserID,
		SessionId:   q.SessionID,
		Prompt:      q.Prompt,
		SandboxId:   q.SandboxID,
		MaxTurns:    q.MaxTurns,
		Attachments: atts,
	})
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
