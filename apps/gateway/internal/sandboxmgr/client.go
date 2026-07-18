// Package sandboxmgr is the gateway's thin client to the sandbox-manager gRPC
// service. The gateway normally reaches the sandbox only indirectly (through
// agent-runtime, which drives agent turns). The Preview Proxy is the one
// data-plane feature the gateway serves directly: to reverse-proxy a user's
// in-sandbox dev server it must resolve that port to a reachable URL, which is
// a sandbox-manager capability. Keeping this in its own narrow package means
// the HTTP layer depends on an EndpointResolver abstraction, not the generated
// stubs.
package sandboxmgr

import (
	"context"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/cocola-project/cocola/packages/go-common/tracing"
	sandboxv1 "github.com/cocola-project/cocola/packages/proto/gen/go/cocola/sandbox/v1"
)

// ResolvedEndpoint is a server-reachable URL for an in-sandbox port plus the
// headers to replay on every proxied request (auth/routing).
type ResolvedEndpoint struct {
	URL     string
	Headers map[string]string
}

// EndpointResolver maps a session's bound sandbox + in-sandbox port to a
// reachable URL. The Preview Proxy handler depends on this interface so tests
// can inject a fake without a real sandbox-manager.
type EndpointResolver interface {
	ResolveEndpoint(ctx context.Context, userID, sessionID string, port int) (*ResolvedEndpoint, error)
}

// Client is the gRPC-backed EndpointResolver. Build it with Dial.
type Client struct {
	conn *grpc.ClientConn
	rpc  sandboxv1.SandboxServiceClient
}

// Dial opens a lazy plaintext connection to sandbox-manager at addr. Like the
// agent-runtime client it connects on first RPC. sandbox-manager is an internal
// service reached over the cluster network, not the public internet.
func Dial(addr string) (*Client, error) {
	conn, err := grpc.Dial(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		tracing.GRPCClientDialOption(),
	)
	if err != nil {
		return nil, fmt.Errorf("sandboxmgr: dial %q: %w", addr, err)
	}
	return NewClient(conn), nil
}

// NewClient wraps an already-established gRPC connection (used by tests with a
// bufconn-backed conn).
func NewClient(conn *grpc.ClientConn) *Client {
	return &Client{conn: conn, rpc: sandboxv1.NewSandboxServiceClient(conn)}
}

// Close releases the underlying connection.
func (c *Client) Close() error {
	if c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

// ResolveEndpoint asks sandbox-manager for the reachable URL of an in-sandbox
// port for the session's bound sandbox.
func (c *Client) ResolveEndpoint(ctx context.Context, userID, sessionID string, port int) (*ResolvedEndpoint, error) {
	resp, err := c.rpc.ResolveEndpoint(ctx, &sandboxv1.ResolveEndpointRequest{
		SessionId: sessionID,
		UserId:    userID,
		Port:      int32(port),
	})
	if err != nil {
		return nil, err
	}
	return &ResolvedEndpoint{URL: resp.GetUrl(), Headers: resp.GetHeaders()}, nil
}
