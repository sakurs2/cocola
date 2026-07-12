// End-to-end integration test for the backend MVP seam: a REAL gRPC server
// (implementing the proto AgentRuntimeService) reached by the REAL agent.Client
// over an in-memory bufconn pipe, driven through the REAL httpapi SSE handler
// with auth enabled. Unlike the package-local handler tests (which inject a fake
// Streamer), this exercises the actual generated stubs, streaming
// serialization, io.EOF termination, and token-derived user_id end to end —
// without binding a TCP port.
package integration

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	"github.com/cocola-project/cocola/apps/gateway/internal/agent"
	"github.com/cocola-project/cocola/apps/gateway/internal/auth"
	"github.com/cocola-project/cocola/apps/gateway/internal/chatrun"
	"github.com/cocola-project/cocola/apps/gateway/internal/convo"
	"github.com/cocola-project/cocola/apps/gateway/internal/httpapi"
	"github.com/cocola-project/cocola/packages/go-common/logger"
	"github.com/cocola-project/cocola/packages/go-common/token"
	agentv1 "github.com/cocola-project/cocola/packages/proto/gen/go/cocola/agent/v1"
)

// scriptedServer is a real AgentRuntimeServiceServer that echoes the request and
// streams a fixed script of events. It records the request it received so the
// test can assert the user_id was derived from the token, not the body.
type scriptedServer struct {
	agentv1.UnimplementedAgentRuntimeServiceServer
	gotUserID      string
	gotPrompt      string
	gotSessionID   string
	gotAttachments []*agentv1.Attachment
}

func (s *scriptedServer) Query(req *agentv1.QueryRequest, stream agentv1.AgentRuntimeService_QueryServer) error {
	s.gotUserID = req.GetUserId()
	s.gotPrompt = req.GetPrompt()
	s.gotSessionID = req.GetSessionId()
	s.gotAttachments = req.GetAttachments()
	events := []*agentv1.AgentEvent{
		{Kind: "text", Data: map[string]string{"text": "hello " + req.GetPrompt()}},
		{Kind: "text", Data: map[string]string{"text": "world"}},
		{Kind: "done", Data: map[string]string{"reason": "stop"}},
	}
	for _, ev := range events {
		if err := stream.Send(ev); err != nil {
			return err
		}
	}
	return nil // io.EOF on the client side
}

// startGRPC stands up the scripted server on an in-memory bufconn and returns a
// dialed *grpc.ClientConn plus a cleanup func.
func startGRPC(t *testing.T, srv agentv1.AgentRuntimeServiceServer) (*grpc.ClientConn, func()) {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	gs := grpc.NewServer()
	agentv1.RegisterAgentRuntimeServiceServer(gs, srv)
	go func() { _ = gs.Serve(lis) }()

	// grpc 1.62.1 has no grpc.NewClient; use DialContext with a bufconn dialer.
	conn, err := grpc.DialContext(
		context.Background(),
		"bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial bufconn: %v", err)
	}
	return conn, func() {
		_ = conn.Close()
		gs.Stop()
		_ = lis.Close()
	}
}

func buildHandler(t *testing.T, conn *grpc.ClientConn) http.Handler {
	t.Helper()
	client := agent.NewClient(conn)
	verifier := auth.NewVerifier(auth.Config{Secret: "test-secret", Issuer: "cocola"})
	conversations := convo.NewMemory()
	runs := chatrun.NewMemory(conversations)
	return httpapi.New(client, verifier, logger.Must()).
		WithConvoStore(conversations).
		WithChatRuns(runs, httpapi.RunConfig{
			RunTimeout: time.Minute, PingEvery: time.Hour,
			MergeWindow: time.Millisecond, DraftInterval: time.Millisecond,
		}).Handler()
}

func TestEndToEndChatStreamsThroughRealGRPC(t *testing.T) {
	srv := &scriptedServer{}
	conn, cleanup := startGRPC(t, srv)
	defer cleanup()

	h := buildHandler(t, conn)

	tok, err := token.Encode(token.Claims{Subject: "emp-77", Issuer: "cocola"}, "test-secret")
	if err != nil {
		t.Fatalf("encode token: %v", err)
	}

	req := httptest.NewRequest("POST", "/v1/chat", strings.NewReader(`{"prompt":"ping","session_id":"sess-1"}`))
	req.Header.Set("authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (body=%q)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("content-type"); ct != "text/event-stream" {
		t.Fatalf("want SSE content-type, got %q", ct)
	}

	body := rec.Body.String()
	// The durable chat path emits an initial replay snapshot, merges adjacent text
	// deltas, and owns the terminal status event.
	for _, want := range []string{
		"event: snapshot\n", "event: text\n", `"text":"hello pingworld"`,
		"event: done\n", `"status":"success"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("SSE body missing %q\n---\n%s", want, body)
		}
	}
	// snapshot + merged text + terminal status => three SSE records.
	if got := strings.Count(body, "\n\n"); got != 3 {
		t.Fatalf("want 3 SSE records, got %d\n---\n%s", got, body)
	}

	// The forwarded user_id MUST come from the verified token; prompt/session
	// from the body.
	if srv.gotUserID != "emp-77" {
		t.Fatalf("user_id must come from token, server saw %q", srv.gotUserID)
	}
	if srv.gotPrompt != "ping" || srv.gotSessionID != "sess-1" {
		t.Fatalf("body fields not forwarded: prompt=%q session=%q", srv.gotPrompt, srv.gotSessionID)
	}
}

func TestEndToEndRejectsMissingToken(t *testing.T) {
	srv := &scriptedServer{}
	conn, cleanup := startGRPC(t, srv)
	defer cleanup()
	h := buildHandler(t, conn)

	req := httptest.NewRequest("POST", "/v1/chat", strings.NewReader(`{"prompt":"ping"}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401 without token, got %d", rec.Code)
	}
	if srv.gotPrompt != "" {
		t.Fatal("agent-runtime must not be reached when auth fails")
	}
}

// TestEndToEndForwardsAttachmentsAsBytes proves an inline attachment survives
// the full edge->gRPC path: the BFF base64-decodes content_b64 to raw bytes and
// the real generated stub delivers filename/mime/bytes to the server intact
// (push model, ADR-0017). "aGVsbG8gd29ybGQ=" decodes to "hello world".
func TestEndToEndForwardsAttachmentsAsBytes(t *testing.T) {
	srv := &scriptedServer{}
	conn, cleanup := startGRPC(t, srv)
	defer cleanup()

	h := buildHandler(t, conn)

	tok, err := token.Encode(token.Claims{Subject: "emp-77", Issuer: "cocola"}, "test-secret")
	if err != nil {
		t.Fatalf("encode token: %v", err)
	}

	body := `{"prompt":"read it","session_id":"sess-1","attachments":[{"filename":"note.txt","content_b64":"aGVsbG8gd29ybGQ=","mime":"text/plain"}]}`
	req := httptest.NewRequest("POST", "/v1/chat", strings.NewReader(body))
	req.Header.Set("authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (body=%q)", rec.Code, rec.Body.String())
	}
	if len(srv.gotAttachments) != 1 {
		t.Fatalf("want 1 attachment at server, got %d", len(srv.gotAttachments))
	}
	att := srv.gotAttachments[0]
	if att.GetFilename() != "note.txt" || att.GetMime() != "text/plain" {
		t.Fatalf("attachment metadata not forwarded: filename=%q mime=%q", att.GetFilename(), att.GetMime())
	}
	if string(att.GetContent()) != "hello world" {
		t.Fatalf("attachment bytes corrupted, got %q", att.GetContent())
	}
}
