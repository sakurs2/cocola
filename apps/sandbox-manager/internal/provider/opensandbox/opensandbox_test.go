// Copyright 2026 The cocola authors. Licensed under Apache-2.0.

package opensandbox

import (
	"context"
	"encoding/json"
	"errors"
	"io"
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
	for _, want := range []string{`"uri":"cocola/sandbox-runtime:dev"`, `"entrypoint":["tail","-f","/dev/null"]`, `"cpu":"500m"`, `"memory":"512Mi"`, `"defaultAction":"deny"`, `"target":"api.anthropic.com"`} {
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

func TestDeferredFileMethods_ReturnNotImplemented(t *testing.T) {
	p := newStub(t, func(r *http.Request) (*http.Response, error) {
		t.Fatal("deferred file methods must not issue HTTP calls")
		return nil, nil
	})
	ctx := context.Background()

	if err := p.WriteFile(ctx, "sbx", "/tmp/x", nil); !errors.Is(err, errNotImplemented) {
		t.Errorf("WriteFile err = %v, want errNotImplemented", err)
	}
	if _, err := p.ReadFile(ctx, "sbx", "/tmp/x"); !errors.Is(err, errNotImplemented) {
		t.Errorf("ReadFile err = %v, want errNotImplemented", err)
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
	if len(mapResources(provider.Resources{})) != 0 {
		t.Error("zero resources should map to empty limits")
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
		"/data/userdata/u1":     false,
		"/home/cocola/.claude":  false,
		"/workspace/s1":         false,
		"/data/plugins":         true, // readOnly
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
