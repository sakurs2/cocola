// Copyright 2026 The cocola authors. Licensed under Apache-2.0.

package opensandbox

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"mime"
	"mime/multipart"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/cocola-project/cocola/apps/sandbox-manager/internal/provider"
)

// roundTripFunc adapts a function to http.RoundTripper so tests can stub the
// OpenSandbox REST server without opening any socket (honors the no-listening-
// process rule).
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func jsonResp(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

// newStub builds a Provider whose HTTP client is backed by handler.
func newStub(t *testing.T, handler roundTripFunc) *Provider {
	t.Helper()
	p, err := New(
		WithBaseURL("http://opensandbox.test/v1"),
		WithAPIKey("test-key"),
		WithHTTPClient(&http.Client{Transport: handler}),
		// Disable the non-root drop by default so tests can assert the inner
		// command body directly; TestExec_RunsAsExecUser* cover the wrap.
		WithExecUser(""),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return p
}

func TestNew_RequiresBaseURL(t *testing.T) {
	t.Setenv("COCOLA_OPENSANDBOX_URL", "")
	if _, err := New(); err == nil {
		t.Fatal("expected error when base URL is unset, got nil")
	}
}

func TestNew_EnvDefaults(t *testing.T) {
	t.Setenv("COCOLA_OPENSANDBOX_URL", "http://from-env:8090/v1/")
	t.Setenv("COCOLA_OPENSANDBOX_API_KEY", "env-key")
	p, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if p.baseURL != "http://from-env:8090/v1" { // trailing slash trimmed
		t.Errorf("baseURL = %q, want trimmed env value", p.baseURL)
	}
	if p.apiKey != "env-key" {
		t.Errorf("apiKey = %q, want env-key", p.apiKey)
	}
}

func TestCreate_HappyPath(t *testing.T) {
	var gotMethod, gotPath, gotAPIKey, gotCT string
	gotBody := ""
	p := newStub(t, func(r *http.Request) (*http.Response, error) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAPIKey = r.Header.Get(apiKeyHeader)
		gotCT = r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		return jsonResp(http.StatusOK, `{"id":"sbx-123","status":{"state":"Pending"}}`), nil
	})

	sb, err := p.Create(context.Background(), provider.SandboxSpec{
		UserID:     "u1",
		SessionID:  "s1",
		Image:      "cocola/sandbox-runtime:dev",
		Resources:  provider.Resources{CPUCores: 0.5, MemoryMiB: 512},
		Networking: provider.Networking{EgressAllowlist: []string{"api.anthropic.com"}},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if sb.ID != "sbx-123" {
		t.Errorf("sandbox id = %q, want sbx-123", sb.ID)
	}
	if sb.UserID != "u1" || sb.SessionID != "s1" {
		t.Errorf("sandbox user/session = %q/%q, want u1/s1", sb.UserID, sb.SessionID)
	}
	if gotMethod != http.MethodPost || gotPath != "/v1/sandboxes" {
		t.Errorf("request = %s %s, want POST /v1/sandboxes", gotMethod, gotPath)
	}
	if gotAPIKey != "test-key" {
		t.Errorf("api key header = %q, want test-key", gotAPIKey)
	}
	if gotCT != "application/json" {
		t.Errorf("content-type = %q, want application/json", gotCT)
	}
	// Body assertions: resource + egress mapping landed on the wire.
	for _, want := range []string{`"uri":"cocola/sandbox-runtime:dev"`, `"entrypoint":["sleep","infinity"]`, `"cpu":"500m"`, `"memory":"512Mi"`, `"defaultAction":"deny"`, `"target":"api.anthropic.com"`} {
		if !strings.Contains(gotBody, want) {
			t.Errorf("request body missing %s\nbody: %s", want, gotBody)
		}
	}
	// id mapping recorded
	if got, err := p.resolve("sbx-123"); err != nil || got != "sbx-123" {
		t.Errorf("resolve after create = %q,%v", got, err)
	}
}

func TestCreate_EmptyIDFails(t *testing.T) {
	p := newStub(t, func(r *http.Request) (*http.Response, error) {
		return jsonResp(http.StatusOK, `{"id":"","status":{"state":"Pending"}}`), nil
	})
	if _, err := p.Create(context.Background(), provider.SandboxSpec{}); err == nil {
		t.Fatal("expected error on empty id, got nil")
	}
}

func TestCreate_ServerErrorPropagates(t *testing.T) {
	p := newStub(t, func(r *http.Request) (*http.Response, error) {
		return jsonResp(http.StatusInternalServerError, `{"error":"boom"}`), nil
	})
	_, err := p.Create(context.Background(), provider.SandboxSpec{Image: "x"})
	if err == nil || !strings.Contains(err.Error(), "status 500") {
		t.Fatalf("want status 500 error, got %v", err)
	}
}

func TestHealth_RunningIsHealthy(t *testing.T) {
	p := newStub(t, func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/sandboxes/sbx-1" {
			t.Errorf("request = %s %s, want GET /v1/sandboxes/sbx-1", r.Method, r.URL.Path)
		}
		return jsonResp(http.StatusOK, `{"id":"sbx-1","status":{"state":"Running"}}`), nil
	})
	h, err := p.Health(context.Background(), "sbx-1")
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	if !h.Healthy || h.Detail != "Running" {
		t.Errorf("health = %+v, want healthy Running", h)
	}
}

func TestHealth_NonRunningIsUnhealthy(t *testing.T) {
	p := newStub(t, func(r *http.Request) (*http.Response, error) {
		return jsonResp(http.StatusOK, `{"id":"sbx-1","status":{"state":"Failed","message":"oom"}}`), nil
	})
	h, err := p.Health(context.Background(), "sbx-1")
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	if h.Healthy {
		t.Error("Failed state should be unhealthy")
	}
	if h.Detail != "Failed: oom" {
		t.Errorf("detail = %q, want 'Failed: oom'", h.Detail)
	}
}

func TestDestroy_DeletesAndDropsMapping(t *testing.T) {
	var gotMethod, gotPath string
	p := newStub(t, func(r *http.Request) (*http.Response, error) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		return jsonResp(http.StatusNoContent, ""), nil
	})
	p.mu.Lock()
	p.ids["sbx-9"] = "sbx-9"
	p.mu.Unlock()

	if err := p.Destroy(context.Background(), "sbx-9"); err != nil {
		t.Fatalf("Destroy: %v", err)
	}
	if gotMethod != http.MethodDelete || gotPath != "/v1/sandboxes/sbx-9" {
		t.Errorf("request = %s %s, want DELETE /v1/sandboxes/sbx-9", gotMethod, gotPath)
	}
	p.mu.RLock()
	_, exists := p.ids["sbx-9"]
	p.mu.RUnlock()
	if exists {
		t.Error("id mapping should be dropped after Destroy")
	}
}

func TestPause_PostsPauseEndpoint(t *testing.T) {
	var gotMethod, gotPath string
	p := newStub(t, func(r *http.Request) (*http.Response, error) {
		gotMethod, gotPath = r.Method, r.URL.Path
		return jsonResp(http.StatusOK, ""), nil
	})
	if err := p.Pause(context.Background(), "sbx-7"); err != nil {
		t.Fatalf("Pause: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/v1/sandboxes/sbx-7/pause" {
		t.Errorf("request = %s %s, want POST /v1/sandboxes/sbx-7/pause", gotMethod, gotPath)
	}
}

func TestResume_PostsResumeEndpoint(t *testing.T) {
	var gotMethod, gotPath string
	p := newStub(t, func(r *http.Request) (*http.Response, error) {
		gotMethod, gotPath = r.Method, r.URL.Path
		return jsonResp(http.StatusOK, ""), nil
	})
	if err := p.Resume(context.Background(), "sbx-7"); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/v1/sandboxes/sbx-7/resume" {
		t.Errorf("request = %s %s, want POST /v1/sandboxes/sbx-7/resume", gotMethod, gotPath)
	}
}

// sseResp builds an SSE/NDJSON streaming response from a body string.
func sseResp(body string) *http.Response {
	h := make(http.Header)
	h.Set("Content-Type", "text/event-stream")
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     h,
	}
}

// drainExec collects all events from an Exec channel into a slice.
func drainExec(ch <-chan provider.ExecEvent) []provider.ExecEvent {
	var evs []provider.ExecEvent
	for e := range ch {
		evs = append(evs, e)
	}
	return evs
}

// TestExec_BridgesSSEStream wires Exec end to end against a stub that first
// resolves the execd endpoint (GET .../endpoints/44772) and then serves an
// NDJSON exec stream. It verifies the two-step flow, the command body, and the
// stdout/stderr/exit bridging onto cocola's ExecEvent channel.
func TestExec_BridgesSSEStream(t *testing.T) {
	var endpointHit, commandHit bool
	var gotCmdBody, gotAccept, gotExecdToken string
	p := newStub(t, func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/sandboxes/sbx-1"):
			return jsonResp(http.StatusOK, `{"id":"sbx-1","status":{"state":"Running"}}`), nil
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/endpoints/44772"):
			endpointHit = true
			return jsonResp(http.StatusOK, `{"endpoint":"http://execd.test:44772","headers":{"X-EXECD-ACCESS-TOKEN":"etok"}}`), nil
		case r.Method == http.MethodPost && r.URL.Path == "/command":
			commandHit = true
			gotAccept = r.Header.Get("Accept")
			gotExecdToken = r.Header.Get("X-EXECD-ACCESS-TOKEN")
			b, _ := io.ReadAll(r.Body)
			gotCmdBody = string(b)
			stream := `{"type":"stdout","text":"hello\n"}` + "\n" +
				`{"type":"stderr","text":"warn\n"}` + "\n" +
				`{"type":"execution_complete","exit_code":0}` + "\n"
			return sseResp(stream), nil
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
			return nil, nil
		}
	})

	ch, err := p.Exec(context.Background(), "sbx-1", provider.ExecRequest{
		Cmd: []string{"echo", "hi"},
		Cwd: "/work",
		Env: map[string]string{"FOO": "bar"},
	})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	evs := drainExec(ch)

	if !endpointHit || !commandHit {
		t.Fatalf("expected endpoint+command hits, got endpoint=%v command=%v", endpointHit, commandHit)
	}
	if gotAccept != "text/event-stream" {
		t.Errorf("Accept = %q, want text/event-stream", gotAccept)
	}
	if gotExecdToken != "etok" {
		t.Errorf("execd token header = %q, want etok (from endpoint headers)", gotExecdToken)
	}
	for _, want := range []string{`"command":"'echo' 'hi'"`, `"cwd":"/work"`, `"FOO":"bar"`} {
		if !strings.Contains(gotCmdBody, want) {
			t.Errorf("command body missing %s\nbody: %s", want, gotCmdBody)
		}
	}

	if len(evs) != 3 {
		t.Fatalf("got %d events, want 3: %+v", len(evs), evs)
	}
	if evs[0].Kind != provider.ExecEventStdout || string(evs[0].Stdout) != "hello\n" {
		t.Errorf("event[0] = %+v, want stdout hello", evs[0])
	}
	if evs[1].Kind != provider.ExecEventStderr || string(evs[1].Stderr) != "warn\n" {
		t.Errorf("event[1] = %+v, want stderr warn", evs[1])
	}
	if evs[2].Kind != provider.ExecEventExit || evs[2].Exit != 0 {
		t.Errorf("event[2] = %+v, want exit 0", evs[2])
	}
}

// TestExec_StdinPipedAsBase64 verifies that ExecRequest.Stdin is delivered to
// the command despite execd's /command API having no stdin field: the provider
// base64-encodes the bytes and pipes them in-shell into the real command. This
// is what makes the Route A shim (which reads its Request JSON from stdin) work
// over the OpenSandbox backend.
func TestExec_StdinPipedAsBase64(t *testing.T) {
	var gotCmdBody string
	p := newStub(t, func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/sandboxes/sbx-1"):
			return jsonResp(http.StatusOK, `{"id":"sbx-1","status":{"state":"Running"}}`), nil
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/endpoints/44772"):
			return jsonResp(http.StatusOK, `{"endpoint":"http://execd.test:44772"}`), nil
		case r.Method == http.MethodPost && r.URL.Path == "/command":
			b, _ := io.ReadAll(r.Body)
			gotCmdBody = string(b)
			return sseResp(`{"type":"execution_complete","exit_code":0}` + "\n"), nil
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
			return nil, nil
		}
	})

	ch, err := p.Exec(context.Background(), "sbx-1", provider.ExecRequest{
		Cmd:   []string{"/opt/cocola/shim/entrypoint.sh"},
		Stdin: []byte(`{"prompt":"hi"}`),
	})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	_ = drainExec(ch)

	// The command must base64-decode the exact stdin bytes and pipe them into
	// the shell-quoted argv. JSON-escaped, the pipe and quotes survive as-is.
	wantPipe := `printf %s 'eyJwcm9tcHQiOiJoaSJ9' | base64 -d | '/opt/cocola/shim/entrypoint.sh'`
	if !strings.Contains(gotCmdBody, wantPipe) {
		t.Errorf("command body missing stdin pipe\n  want substring: %s\n  body: %s", wantPipe, gotCmdBody)
	}
}

// TestExec_NoStdinNoPipe verifies the stdin pipe is only added when Stdin is
// non-empty: a plain command must not be wrapped.
func TestExec_NoStdinNoPipe(t *testing.T) {
	var gotCmdBody string
	p := newStub(t, func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/sandboxes/sbx-1"):
			return jsonResp(http.StatusOK, `{"id":"sbx-1","status":{"state":"Running"}}`), nil
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/endpoints/44772"):
			return jsonResp(http.StatusOK, `{"endpoint":"http://execd.test:44772"}`), nil
		case r.Method == http.MethodPost && r.URL.Path == "/command":
			b, _ := io.ReadAll(r.Body)
			gotCmdBody = string(b)
			return sseResp(`{"type":"execution_complete","exit_code":0}` + "\n"), nil
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
			return nil, nil
		}
	})

	ch, err := p.Exec(context.Background(), "sbx-1", provider.ExecRequest{
		Cmd: []string{"echo", "hi"},
	})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	_ = drainExec(ch)

	if strings.Contains(gotCmdBody, "base64 -d") {
		t.Errorf("command body unexpectedly wrapped with stdin pipe: %s", gotCmdBody)
	}
	if !strings.Contains(gotCmdBody, `"command":"'echo' 'hi'"`) {
		t.Errorf("command body missing plain argv: %s", gotCmdBody)
	}
}

// execStub is like newStub but keeps the configured execUser (default "cocola"),
// so the runuser privilege-drop wrap is exercised.
func execStub(t *testing.T, handler roundTripFunc) *Provider {
	t.Helper()
	p, err := New(
		WithBaseURL("http://opensandbox.test/v1"),
		WithAPIKey("test-key"),
		WithHTTPClient(&http.Client{Transport: handler}),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return p
}

// TestExec_RunsAsExecUser asserts that, with the default execUser, the command
// body re-execs the (stdin-piped) pipeline as the non-root brain user via
// runuser. execd runs /command as root and the in-sandbox claude CLI refuses
// --dangerously-skip-permissions under root, so this drop is mandatory.
func TestExec_RunsAsExecUser(t *testing.T) {
	var gotCmdBody string
	p := execStub(t, func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/sandboxes/sbx-1"):
			return jsonResp(http.StatusOK, `{"id":"sbx-1","status":{"state":"Running"}}`), nil
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/endpoints/44772"):
			return jsonResp(http.StatusOK, `{"endpoint":"http://execd.test:44772"}`), nil
		case r.Method == http.MethodPost && r.URL.Path == "/command":
			b, _ := io.ReadAll(r.Body)
			gotCmdBody = string(b)
			return sseResp(`{"type":"execution_complete","exit_code":0}` + "\n"), nil
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
			return nil, nil
		}
	})

	ch, err := p.Exec(context.Background(), "sbx-1", provider.ExecRequest{
		Cmd:   []string{"/opt/cocola/shim/entrypoint.sh"},
		Stdin: []byte(`{"prompt":"hi"}`),
	})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	_ = drainExec(ch)

	// The decoded stdin is piped INTO runuser, which forwards it to the shim.
	// Flat pipeline, no nested bash -c (nesting broke shell parsing).
	wantPipe := `printf %s 'eyJwcm9tcHQiOiJoaSJ9' | base64 -d | runuser -u 'cocola' -- '/opt/cocola/shim/entrypoint.sh'`
	if !strings.Contains(gotCmdBody, wantPipe) {
		t.Errorf("command body missing runuser-piped stdin\n  want substring: %s\n  body: %s", wantPipe, gotCmdBody)
	}
}

// TestExec_RunsAsExecUserNoStdin asserts the drop applies even without stdin.
func TestExec_RunsAsExecUserNoStdin(t *testing.T) {
	var gotCmdBody string
	p := execStub(t, func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/sandboxes/sbx-1"):
			return jsonResp(http.StatusOK, `{"id":"sbx-1","status":{"state":"Running"}}`), nil
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/endpoints/44772"):
			return jsonResp(http.StatusOK, `{"endpoint":"http://execd.test:44772"}`), nil
		case r.Method == http.MethodPost && r.URL.Path == "/command":
			b, _ := io.ReadAll(r.Body)
			gotCmdBody = string(b)
			return sseResp(`{"type":"execution_complete","exit_code":0}` + "\n"), nil
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
			return nil, nil
		}
	})

	ch, err := p.Exec(context.Background(), "sbx-1", provider.ExecRequest{
		Cmd: []string{"claude", "--version"},
	})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	_ = drainExec(ch)

	if !strings.Contains(gotCmdBody, `"command":"runuser -u 'cocola' -- 'claude' '--version'"`) {
		t.Errorf("command body missing runuser-wrapped argv: %s", gotCmdBody)
	}
	if strings.Contains(gotCmdBody, "base64 -d") {
		t.Errorf("command body unexpectedly piped stdin: %s", gotCmdBody)
	}
}

// TestExec_ExecUserEnvDisablesDrop confirms COCOLA_OPENSANDBOX_EXEC_USER="root"
// disables the privilege drop (runs as execd's default user).
func TestExec_ExecUserEnvDisablesDrop(t *testing.T) {
	t.Setenv("COCOLA_OPENSANDBOX_EXEC_USER", "root")
	var gotCmdBody string
	p := execStub(t, func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/sandboxes/sbx-1"):
			return jsonResp(http.StatusOK, `{"id":"sbx-1","status":{"state":"Running"}}`), nil
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/endpoints/44772"):
			return jsonResp(http.StatusOK, `{"endpoint":"http://execd.test:44772"}`), nil
		case r.Method == http.MethodPost && r.URL.Path == "/command":
			b, _ := io.ReadAll(r.Body)
			gotCmdBody = string(b)
			return sseResp(`{"type":"execution_complete","exit_code":0}` + "\n"), nil
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
			return nil, nil
		}
	})

	ch, err := p.Exec(context.Background(), "sbx-1", provider.ExecRequest{
		Cmd: []string{"echo", "hi"},
	})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	_ = drainExec(ch)

	if strings.Contains(gotCmdBody, "runuser") {
		t.Errorf("EXEC_USER=root should disable the drop, got: %s", gotCmdBody)
	}
	if !strings.Contains(gotCmdBody, `"command":"'echo' 'hi'"`) {
		t.Errorf("command body missing plain argv: %s", gotCmdBody)
	}
}

// TestExec_ErrorEventMapsToExitCode verifies that an "error" event whose value
// is a numeric exit code surfaces as ExecEventExit, not ExecEventError.
func TestExec_ErrorEventMapsToExitCode(t *testing.T) {
	p := newStub(t, func(r *http.Request) (*http.Response, error) {
		if strings.Contains(r.URL.Path, "/endpoints/44772") {
			return jsonResp(http.StatusOK, `{"endpoint":"http://execd.test:44772"}`), nil
		}
		stream := `{"type":"stdout","text":"x"}` + "\n" + `{"type":"error","evalue":"3"}` + "\n"
		return sseResp(stream), nil
	})
	ch, err := p.Exec(context.Background(), "sbx-1", provider.ExecRequest{Cmd: []string{"false"}})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	evs := drainExec(ch)
	last := evs[len(evs)-1]
	if last.Kind != provider.ExecEventExit || last.Exit != 3 {
		t.Errorf("last event = %+v, want exit 3", last)
	}
}

// TestExec_NonNumericErrorMapsToErrorEvent verifies that a non-numeric error
// value surfaces as ExecEventError.
func TestExec_NonNumericErrorMapsToErrorEvent(t *testing.T) {
	p := newStub(t, func(r *http.Request) (*http.Response, error) {
		if strings.Contains(r.URL.Path, "/endpoints/44772") {
			return jsonResp(http.StatusOK, `{"endpoint":"http://execd.test:44772"}`), nil
		}
		return sseResp(`{"type":"error","error":{"evalue":"command not found","ename":"ExecError"}}` + "\n"), nil
	})
	ch, err := p.Exec(context.Background(), "sbx-1", provider.ExecRequest{Cmd: []string{"nope"}})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	evs := drainExec(ch)
	last := evs[len(evs)-1]
	if last.Kind != provider.ExecEventError || last.Err == nil || !strings.Contains(last.Err.Error(), "command not found") {
		t.Errorf("last event = %+v, want error 'command not found'", last)
	}
}

// TestExec_EmptyCommandRejected ensures Exec validates input before issuing any
// HTTP call.
func TestExec_EmptyCommandRejected(t *testing.T) {
	p := newStub(t, func(r *http.Request) (*http.Response, error) {
		t.Fatal("empty command must not issue HTTP calls")
		return nil, nil
	})
	if _, err := p.Exec(context.Background(), "sbx", provider.ExecRequest{}); err == nil {
		t.Fatal("expected error on empty command, got nil")
	}
}

// TestExec_StreamEndSynthesizesExit verifies a stream that ends without an
// explicit terminal event still yields a final ExecEventExit{0}.
func TestExec_StreamEndSynthesizesExit(t *testing.T) {
	p := newStub(t, func(r *http.Request) (*http.Response, error) {
		if strings.Contains(r.URL.Path, "/endpoints/44772") {
			return jsonResp(http.StatusOK, `{"endpoint":"http://execd.test:44772"}`), nil
		}
		return sseResp(`{"type":"stdout","text":"only output"}` + "\n"), nil
	})
	ch, err := p.Exec(context.Background(), "sbx-1", provider.ExecRequest{Cmd: []string{"echo"}})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	evs := drainExec(ch)
	last := evs[len(evs)-1]
	if last.Kind != provider.ExecEventExit || last.Exit != 0 {
		t.Errorf("last event = %+v, want synthesized exit 0", last)
	}
}

func TestMapResources(t *testing.T) {
	got := mapResources(provider.Resources{CPUCores: 2, MemoryMiB: 1024})
	if got["cpu"] != "2000m" || got["memory"] != "1024Mi" {
		t.Errorf("mapResources = %v, want cpu=2000m memory=1024Mi", got)
	}
	// Zero resources must NOT yield empty limits: OpenSandbox rejects a
	// non-pooled create without resourceLimits, and the on-demand path
	// (binder -> Create, ADR-0015) sets no Resources. The provider supplies a
	// default floor so /v1/chat never 422s. See mapResources / envOr.
	def := mapResources(provider.Resources{})
	if def["cpu"] != defaultCPU || def["memory"] != defaultMemory {
		t.Errorf("zero resources = %v, want cpu=%s memory=%s (default floor)", def, defaultCPU, defaultMemory)
	}
}

func TestMapResources_EnvOverride(t *testing.T) {
	t.Setenv("COCOLA_OPENSANDBOX_DEFAULT_CPU", "250m")
	t.Setenv("COCOLA_OPENSANDBOX_DEFAULT_MEMORY", "256Mi")
	got := mapResources(provider.Resources{})
	if got["cpu"] != "250m" || got["memory"] != "256Mi" {
		t.Errorf("env-overridden defaults = %v, want cpu=250m memory=256Mi", got)
	}
}

func TestShellJoin(t *testing.T) {
	cases := []struct {
		in   []string
		want string
	}{
		{[]string{"echo", "hi"}, "'echo' 'hi'"},
		{[]string{"sh", "-c", "echo a; uname -a"}, "'sh' '-c' 'echo a; uname -a'"},
		{[]string{"sh", "-c", "exit 3"}, "'sh' '-c' 'exit 3'"},
		{[]string{"echo", "it's"}, "'echo' 'it'\\''s'"},
		{[]string{"echo", ""}, "'echo' ''"},
	}
	for _, c := range cases {
		if got := shellJoin(c.in); got != c.want {
			t.Errorf("shellJoin(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSafe(t *testing.T) {
	cases := []struct{ in, want string }{
		{"u1", "u1"},
		{"User_123", "user-123"},
		{"a..b/c", "a-b-c"},
		{"--Foo--", "foo"},
		{"已删除", "x"}, // all non-ASCII collapses then trims to empty -> fallback
		{"", "x"},
		{"A___B", "a-b"},
	}
	for _, c := range cases {
		if got := safe(c.in); got != c.want {
			t.Errorf("safe(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	// Result must satisfy OpenSandbox claim-name rules when prefixed.
	re := regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)
	for _, in := range []string{"u1", "User_123", "a..b/c", "已删除", ""} {
		claim := "cocola-user-" + safe(in)
		if !re.MatchString(claim) {
			t.Errorf("claim %q (from %q) is not a legal volume name", claim, in)
		}
	}
}

func TestMapVolumes(t *testing.T) {
	vols := mapVolumes("u1", "s1")
	if len(vols) != 4 {
		t.Fatalf("mapVolumes returned %d volumes, want 4", len(vols))
	}

	// 0: user files volume at /data/userdata/<uid>, RW, no subPath.
	if v := vols[0]; v.PVC == nil || v.PVC.ClaimName != "cocola-user-u1" ||
		!v.PVC.CreateIfNotExists || v.MountPath != "/data/userdata/u1" ||
		v.ReadOnly || v.SubPath != "" {
		t.Errorf("user volume = %+v (pvc %+v)", v, v.PVC)
	}
	// 1: .claude as subPath of the SAME user volume.
	if v := vols[1]; v.PVC == nil || v.PVC.ClaimName != "cocola-user-u1" ||
		v.MountPath != "/home/cocola/.claude" || v.SubPath != ".claude" || v.ReadOnly {
		t.Errorf("claude volume = %+v (pvc %+v)", v, v.PVC)
	}
	// .claude must reuse the user claim, never a separate volume.
	if vols[0].PVC.ClaimName != vols[1].PVC.ClaimName {
		t.Errorf("claude volume claim %q != user volume claim %q", vols[1].PVC.ClaimName, vols[0].PVC.ClaimName)
	}
	// 2: session workspace, RW, must NOT delete on termination (cocola GC).
	if v := vols[2]; v.PVC == nil || v.PVC.ClaimName != "cocola-session-s1" ||
		!v.PVC.CreateIfNotExists || v.PVC.DeleteOnSandboxTermination ||
		v.MountPath != "/workspace/s1" || v.ReadOnly {
		t.Errorf("session volume = %+v (pvc %+v)", v, v.PVC)
	}
	// 3: shared platform-skill volume, read-only, no createIfNotExists.
	if v := vols[3]; v.PVC == nil || v.PVC.ClaimName != "cocola-plugins" ||
		v.PVC.CreateIfNotExists || v.MountPath != "/data/plugins" || !v.ReadOnly {
		t.Errorf("plugins volume = %+v (pvc %+v)", v, v.PVC)
	}
	// Every volume needs a non-empty, request-unique Name (server-required:
	// real OpenSandbox 422s on a missing volumes[*].name).
	seen := map[string]bool{}
	for i, v := range vols {
		if v.Name == "" {
			t.Errorf("volume %d has empty Name", i)
		}
		if seen[v.Name] {
			t.Errorf("duplicate volume Name %q", v.Name)
		}
		seen[v.Name] = true
	}
}

func TestMapVolumes_SanitisesIDs(t *testing.T) {
	vols := mapVolumes("User_A/B", "Sess..1")
	if vols[0].PVC.ClaimName != "cocola-user-user-a-b" {
		t.Errorf("user claim = %q, want cocola-user-user-a-b", vols[0].PVC.ClaimName)
	}
	if vols[0].MountPath != "/data/userdata/user-a-b" {
		t.Errorf("user mountPath = %q", vols[0].MountPath)
	}
	if vols[2].PVC.ClaimName != "cocola-session-sess-1" {
		t.Errorf("session claim = %q, want cocola-session-sess-1", vols[2].PVC.ClaimName)
	}
	if vols[2].MountPath != "/workspace/sess-1" {
		t.Errorf("session mountPath = %q", vols[2].MountPath)
	}
}

// TestCreate_SendsVolumes asserts the mapped volumes reach the wire on Create.
func TestCreate_SendsVolumes(t *testing.T) {
	var body createSandboxRequest
	p := newStub(t, func(r *http.Request) (*http.Response, error) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		return jsonResp(http.StatusOK, `{"id":"sbx-9","status":{"state":"Pending"}}`), nil
	})
	if _, err := p.Create(context.Background(), provider.SandboxSpec{
		UserID: "u1", SessionID: "s1", Image: "img",
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if len(body.Volumes) != 4 {
		t.Fatalf("wire volumes = %d, want 4\nbody=%+v", len(body.Volumes), body)
	}
	want := map[string]bool{
		"/data/userdata/u1":    false,
		"/home/cocola/.claude": false,
		"/workspace/s1":        false,
		"/data/plugins":        true, // readOnly
	}
	for _, v := range body.Volumes {
		ro, ok := want[v.MountPath]
		if !ok {
			t.Errorf("unexpected volume mountPath %q", v.MountPath)
			continue
		}
		if v.ReadOnly != ro {
			t.Errorf("%s readOnly = %v, want %v", v.MountPath, v.ReadOnly, ro)
		}
		if v.PVC == nil || v.PVC.ClaimName == "" {
			t.Errorf("%s missing pvc claimName", v.MountPath)
		}
		delete(want, v.MountPath)
	}
	if len(want) != 0 {
		t.Errorf("missing volumes on wire: %v", want)
	}
}

// TestExec_StdoutNewlineFraming verifies the provider restores newline framing
// on stdout events. execd line-buffers the child's stdout and emits one event
// per line with the trailing newline STRIPPED; the downstream consumer
// (agent-runtime shim_provider) reassembles NDJSON by splitting on "\n", so
// without re-appended newlines its JSON objects concatenate with no delimiter
// and none are ever parsed (the empty-output bug). Each stdout event must carry
// exactly one trailing newline regardless of whether execd stripped it.
func TestExec_StdoutNewlineFraming(t *testing.T) {
	p := newStub(t, func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/sandboxes/sbx-1"):
			return jsonResp(http.StatusOK, `{"id":"sbx-1","status":{"state":"Running"}}`), nil
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/endpoints/44772"):
			return jsonResp(http.StatusOK, `{"endpoint":"http://execd.test:44772"}`), nil
		case r.Method == http.MethodPost && r.URL.Path == "/command":
			// Two NDJSON lines, neither with a trailing newline inside the
			// event "text" field -- exactly how execd delivers them.
			stream := `{"type":"stdout","text":"{\"type\":\"text\",\"text\":\"a\"}"}` + "\n" +
				`{"type":"stdout","text":"{\"type\":\"done\"}"}` + "\n" +
				`{"type":"execution_complete","exit_code":0}` + "\n"
			return sseResp(stream), nil
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
			return nil, nil
		}
	})

	ch, err := p.Exec(context.Background(), "sbx-1", provider.ExecRequest{Cmd: []string{"x"}})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	evs := drainExec(ch)

	var stdout []string
	for _, e := range evs {
		if e.Kind == provider.ExecEventStdout {
			stdout = append(stdout, string(e.Stdout))
		}
	}
	if len(stdout) != 2 {
		t.Fatalf("got %d stdout events, want 2: %q", len(stdout), stdout)
	}
	for i, s := range stdout {
		if !strings.HasSuffix(s, "\n") {
			t.Errorf("stdout[%d] = %q, want trailing newline", i, s)
		}
		if strings.Count(s, "\n") != 1 {
			t.Errorf("stdout[%d] = %q, want exactly one newline", i, s)
		}
	}
	// Concatenating the bridged stdout must yield newline-delimited NDJSON the
	// consumer can split back into the original two objects.
	joined := strings.Join(stdout, "")
	lines := strings.Split(strings.TrimRight(joined, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("reassembled %d lines, want 2: %q", len(lines), lines)
	}
	for _, ln := range lines {
		var obj map[string]any
		if err := json.Unmarshal([]byte(ln), &obj); err != nil {
			t.Errorf("reassembled line is not valid JSON: %q (%v)", ln, err)
		}
	}
}

// TestExec_WaitsForExecdReady verifies the cold-start readiness gate: a freshly
// started/resumed container reports Running before execd binds its socket, in
// which case the server proxy returns 500 "Server disconnected". Exec must probe
// with an idempotent no-op until execd answers 2xx, then run the real command --
// rather than failing or running the (possibly non-idempotent) command against a
// not-yet-ready execd.
func TestExec_WaitsForExecdReady(t *testing.T) {
	var probeCount, realRan int
	var ranTrueProbe bool
	p := newStub(t, func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/sandboxes/sbx-1"):
			return jsonResp(http.StatusOK, `{"id":"sbx-1","status":{"state":"Running"}}`), nil
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/endpoints/44772"):
			return jsonResp(http.StatusOK, `{"endpoint":"http://execd.test:44772"}`), nil
		case r.Method == http.MethodPost && r.URL.Path == "/command":
			b, _ := io.ReadAll(r.Body)
			body := string(b)
			if strings.Contains(body, `"command":"true"`) {
				ranTrueProbe = true
				probeCount++
				// First probe: execd not yet listening -> proxy 500.
				if probeCount == 1 {
					return jsonResp(http.StatusInternalServerError,
						`Server disconnected without sending a response`), nil
				}
				// Second probe: execd is up.
				return sseResp(`{"type":"execution_complete","exit_code":0}` + "\n"), nil
			}
			// The real command runs only after readiness succeeds.
			realRan++
			return sseResp(`{"type":"stdout","text":"ok"}` + "\n" +
				`{"type":"execution_complete","exit_code":0}` + "\n"), nil
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
			return nil, nil
		}
	})

	ch, err := p.Exec(context.Background(), "sbx-1", provider.ExecRequest{Cmd: []string{"real"}})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	evs := drainExec(ch)

	if !ranTrueProbe {
		t.Fatal("expected an idempotent `true` readiness probe before the real command")
	}
	if probeCount < 2 {
		t.Errorf("probeCount = %d, want >= 2 (first 500 must be retried)", probeCount)
	}
	if realRan != 1 {
		t.Errorf("real command ran %d times, want exactly 1", realRan)
	}
	// The real command's stdout must survive (proves we ran it, not the probe).
	var sawOK bool
	for _, e := range evs {
		if e.Kind == provider.ExecEventStdout && strings.Contains(string(e.Stdout), "ok") {
			sawOK = true
		}
	}
	if !sawOK {
		t.Errorf("did not see real command output; events=%+v", evs)
	}
}

// fileStub builds a Provider wired for the file-transfer tests. handler serves
// the lifecycle GET /sandboxes/{id} (used by thawIfPaused), the endpoints
// resolution, and the execd /files/* call under test.
func fileStub(t *testing.T, handler roundTripFunc) *Provider {
	t.Helper()
	p, err := New(
		WithBaseURL("http://opensandbox.test/v1"),
		WithAPIKey("test-key"),
		WithHTTPClient(&http.Client{Transport: handler}),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return p
}

// TestWriteFile_PostsMultipart asserts WriteFile hits execd's
// POST /files/upload with a two-part multipart body whose metadata part carries
// the target path (and the Exec user as owner) and whose file part carries the
// exact input bytes.
func TestWriteFile_PostsMultipart(t *testing.T) {
	var (
		gotMethod    string
		gotPath      string
		gotCT        string
		metaPath     string
		metaOwner    string
		metaFilename string
		fileBytes    []byte
	)
	p := fileStub(t, func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/sandboxes/sbx-1"):
			return jsonResp(http.StatusOK, `{"id":"sbx-1","status":{"state":"Running"}}`), nil
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/endpoints/44772"):
			return jsonResp(http.StatusOK, `{"endpoint":"http://execd.test:44772"}`), nil
		case r.URL.Path == "/files/upload":
			gotMethod = r.Method
			gotPath = r.URL.Path
			gotCT = r.Header.Get("Content-Type")
			_, params, err := mime.ParseMediaType(gotCT)
			if err != nil {
				t.Fatalf("parse content-type %q: %v", gotCT, err)
			}
			mr := multipart.NewReader(r.Body, params["boundary"])
			for {
				part, err := mr.NextPart()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatalf("next part: %v", err)
				}
				switch part.FormName() {
				case "metadata":
					metaFilename = part.FileName()
					var meta fileMetadata
					if err := json.NewDecoder(part).Decode(&meta); err != nil {
						t.Fatalf("decode metadata: %v", err)
					}
					metaPath = meta.Path
					metaOwner = meta.Owner
				case "file":
					b, _ := io.ReadAll(part)
					fileBytes = b
				default:
					t.Fatalf("unexpected part %q", part.FormName())
				}
			}
			return jsonResp(http.StatusOK, ""), nil
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
			return nil, nil
		}
	})

	want := []byte("hello cocola\n")
	if err := p.WriteFile(context.Background(), "sbx-1", "/workspace/out.txt", want); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/files/upload" {
		t.Errorf("hit %s %s, want POST /files/upload", gotMethod, gotPath)
	}
	if !strings.HasPrefix(gotCT, "multipart/form-data") {
		t.Errorf("Content-Type = %q, want multipart/form-data", gotCT)
	}
	if metaPath != "/workspace/out.txt" {
		t.Errorf("metadata.path = %q, want /workspace/out.txt", metaPath)
	}
	if metaOwner != "cocola" {
		t.Errorf("metadata.owner = %q, want cocola (Exec user)", metaOwner)
	}
	// Regression: execd reads the metadata part via FormFile, so it MUST carry
	// a filename; a nameless part is parsed as a plain form value and execd
	// rejects the upload with "metadata file is missing" (400).
	if metaFilename == "" {
		t.Errorf("metadata part has no filename; execd FormFile would reject it")
	}
	if string(fileBytes) != string(want) {
		t.Errorf("file part = %q, want %q", fileBytes, want)
	}
}

// TestReadFile_GetsDownload asserts ReadFile hits GET /files/download?path=
// and returns the body bytes verbatim.
func TestReadFile_GetsDownload(t *testing.T) {
	var gotQueryPath string
	want := []byte("file contents 123\n")
	p := fileStub(t, func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/sandboxes/sbx-1"):
			return jsonResp(http.StatusOK, `{"id":"sbx-1","status":{"state":"Running"}}`), nil
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/endpoints/44772"):
			return jsonResp(http.StatusOK, `{"endpoint":"http://execd.test:44772"}`), nil
		case r.Method == http.MethodGet && r.URL.Path == "/files/download":
			gotQueryPath = r.URL.Query().Get("path")
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(string(want))),
				Header:     make(http.Header),
			}, nil
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
			return nil, nil
		}
	})

	got, err := p.ReadFile(context.Background(), "sbx-1", "/workspace/data.csv")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if gotQueryPath != "/workspace/data.csv" {
		t.Errorf("download path query = %q, want /workspace/data.csv", gotQueryPath)
	}
	if string(got) != string(want) {
		t.Errorf("ReadFile = %q, want %q", got, want)
	}
}

// TestReadFile_NotFound asserts a 404 from execd surfaces as fs.ErrNotExist so
// callers can distinguish a missing file from a transport failure.
func TestReadFile_NotFound(t *testing.T) {
	p := fileStub(t, func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/sandboxes/sbx-1"):
			return jsonResp(http.StatusOK, `{"id":"sbx-1","status":{"state":"Running"}}`), nil
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/endpoints/44772"):
			return jsonResp(http.StatusOK, `{"endpoint":"http://execd.test:44772"}`), nil
		case r.Method == http.MethodGet && r.URL.Path == "/files/download":
			return jsonResp(http.StatusNotFound, `{"error":"not found"}`), nil
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
			return nil, nil
		}
	})

	_, err := p.ReadFile(context.Background(), "sbx-1", "/workspace/missing.txt")
	if err == nil {
		t.Fatal("expected error for 404, got nil")
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("error = %v, want wrapping fs.ErrNotExist", err)
	}
}

func TestChownEntrypoint(t *testing.T) {
	// Empty exec user runs Exec as root -> no chown needed, bare blocker.
	if got := chownEntrypoint("", "u1", "s1"); len(got) != 2 || got[0] != "sleep" || got[1] != "infinity" {
		t.Fatalf("empty execUser entrypoint = %v, want [sleep infinity]", got)
	}

	// Non-empty exec user -> one-time chown of the three mounts, then exec blocker.
	got := chownEntrypoint("cocola", "u1", "s1")
	if len(got) != 3 || got[0] != "/bin/sh" || got[1] != "-c" {
		t.Fatalf("entrypoint prefix = %v, want [/bin/sh -c ...]", got)
	}
	script := got[2]
	for _, want := range []string{
		"chown -R 'cocola':'cocola'",
		"'/home/cocola/.claude'",
		"'/data/userdata/u1'",
		"'/workspace/s1'",
		"|| true",
		"exec sleep infinity",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("chown script missing %q\nscript: %s", want, script)
		}
	}
}
