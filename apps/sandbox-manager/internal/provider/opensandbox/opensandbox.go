// Package opensandbox implements SandboxProvider on top of an OpenSandbox
// control-plane server (https://github.com/opensandbox-group/OpenSandbox).
//
// This is the #18 PoC skeleton (ADR-0013). It wraps the OpenSandbox REST
// lifecycle API (POST/GET/DELETE /v1/sandboxes, OPEN-SANDBOX-API-KEY header)
// as a cocola backend, proving the seam works WITHOUT touching the core
// SandboxProvider interface or the docker/k8s backends (ADR-0002).
//
// Scope: Create / Health / Destroy / Exec / Pause / Resume are implemented
// against the REST + execd APIs. Exec resolves the per-sandbox execd endpoint
// (lifecycle GET /sandboxes/{id}/endpoints/44772) and bridges its SSE/NDJSON
// command stream into cocola's <-chan ExecEvent. Pause/Resume map to the
// lifecycle POST .../pause and .../resume endpoints. WriteFile / ReadFile remain
// deferred (return errNotImplemented): they map to execd multipart upload /
// ranged download against the same resolved endpoint and are not on the P2
// streaming-exec critical path. See docs/archive/opensandbox-poc-p0-research.md
// for the full REST<->interface mapping that this package is built against.
//
// The REST client here is deliberately stdlib-only (net/http + encoding/json)
// rather than importing github.com/alibaba/OpenSandbox/sdks/sandbox/go: the
// lifecycle calls have no streaming, so a thin client keeps the PoC
// self-contained and offline-buildable. The streaming Exec path reimplements
// just the slice of the official SDK's SSE/NDJSON wire format it needs (see
// bridgeExecSSE) rather than importing the SDK, keeping the dependency surface
// at zero; adopting the upstream SDK wholesale is a follow-up decision.
package opensandbox

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cocola-project/cocola/apps/sandbox-manager/internal/provider"
)

// ProviderName is the registry key used in config to select this backend
// (COCOLA_SANDBOX_PROVIDER=opensandbox).
const ProviderName = "opensandbox"

// apiKeyHeader is the OpenSandbox authentication header.
const apiKeyHeader = "OPEN-SANDBOX-API-KEY"

// defaultHTTPTimeout bounds a single lifecycle REST call. Streaming exec uses a
// separate client whose timeout is governed by the per-Exec context/deadline.
const defaultHTTPTimeout = 30 * time.Second

// execdPort is the standard in-sandbox port the execd service listens on. The
// lifecycle endpoints API resolves it to a reachable URL per sandbox. Mirrors
// the official SDK's DefaultExecdPort.
const execdPort = 44772

// execdAuthHeader is the execd authentication header. The lifecycle endpoint
// response usually supplies its value via the headers map; we also fall back to
// the API key, matching the SDK's server-proxy behaviour.
const execdAuthHeader = "X-EXECD-ACCESS-TOKEN"

// defaultExecTimeout caps a single Exec when the caller leaves req.Timeout at 0.
const defaultExecTimeout = 5 * time.Minute

// execEventBuffer sizes the Exec result channel so a fast-producing SSE stream
// does not block on a momentarily slow consumer. Matches the docker backend.
const execEventBuffer = 32

// errNotImplemented marks the methods deferred to P2. Returning a clear sentinel
// (rather than panicking) keeps the provider safe to register: selecting it only
// fails the operations not yet built, never the process.
var errNotImplemented = errors.New("opensandbox: file transfer (WriteFile/ReadFile) not implemented in this PoC; maps to execd upload/download")

// Provider is an OpenSandbox-backed SandboxProvider. It holds no sandbox state
// of its own beyond a cocola-sid -> opensandbox-id map; the OpenSandbox server
// is the source of truth for lifecycle.
type Provider struct {
	baseURL string // includes the /v1 version prefix, e.g. http://host:8090/v1
	apiKey  string
	http    *http.Client // bounded client for lifecycle REST calls

	// stream is used for the long-lived execd SSE connection. It has no
	// client-level timeout: each Exec governs its own lifetime via the
	// request context's deadline, so a 10-minute command is not killed by a
	// 30s lifecycle timeout.
	stream *http.Client

	mu  sync.RWMutex
	ids map[string]string // cocola sandbox id -> opensandbox id
}

// Option configures the Provider.
type Option func(*Provider)

// WithBaseURL overrides the OpenSandbox server base URL (must include /v1).
func WithBaseURL(u string) Option { return func(p *Provider) { p.baseURL = strings.TrimRight(u, "/") } }

// WithAPIKey overrides the API key.
func WithAPIKey(k string) Option { return func(p *Provider) { p.apiKey = k } }

// WithHTTPClient injects a custom http.Client used for BOTH the lifecycle REST
// calls and the execd SSE stream. This is the seam unit tests use to supply a
// stub RoundTripper, so no real socket is ever opened.
func WithHTTPClient(c *http.Client) Option {
	return func(p *Provider) {
		p.http = c
		p.stream = c
	}
}

// New constructs an OpenSandbox provider. Connection settings come from env by
// default (COCOLA_OPENSANDBOX_URL, COCOLA_OPENSANDBOX_API_KEY) and can be
// overridden by options.
func New(opts ...Option) (*Provider, error) {
	p := &Provider{
		baseURL: strings.TrimRight(os.Getenv("COCOLA_OPENSANDBOX_URL"), "/"),
		apiKey:  os.Getenv("COCOLA_OPENSANDBOX_API_KEY"),
		http:    &http.Client{Timeout: defaultHTTPTimeout},
		stream:  &http.Client{}, // no timeout: Exec context governs lifetime
		ids:     map[string]string{},
	}
	for _, o := range opts {
		o(p)
	}
	if p.baseURL == "" {
		return nil, fmt.Errorf("opensandbox: base URL not set (COCOLA_OPENSANDBOX_URL or WithBaseURL)")
	}
	return p, nil
}

// --- REST wire types (subset of the OpenSandbox lifecycle API) ---------------

type imageSpec struct {
	URI string `json:"uri"`
}

type networkRule struct {
	Action string `json:"action"`
	Target string `json:"target"`
}

type networkPolicy struct {
	DefaultAction string        `json:"defaultAction,omitempty"`
	Egress        []networkRule `json:"egress,omitempty"`
}

type createSandboxRequest struct {
	Image          *imageSpec        `json:"image,omitempty"`
	ResourceLimits map[string]string `json:"resourceLimits,omitempty"`
	Env            map[string]string `json:"env,omitempty"`
	Metadata       map[string]string `json:"metadata,omitempty"`
	NetworkPolicy  *networkPolicy    `json:"networkPolicy,omitempty"`
}

type sandboxStatus struct {
	State   string `json:"state"`
	Reason  string `json:"reason,omitempty"`
	Message string `json:"message,omitempty"`
}

type sandboxInfo struct {
	ID     string        `json:"id"`
	Status sandboxStatus `json:"status"`
}

// endpointInfo is the lifecycle GET .../endpoints/{port} response. Headers
// carries per-sandbox routing/auth (e.g. X-EXECD-ACCESS-TOKEN) that must be
// replayed on every execd request.
type endpointInfo struct {
	Endpoint string            `json:"endpoint"`
	Headers  map[string]string `json:"headers,omitempty"`
}

// runCommandRequest is the execd POST /command body (subset).
type runCommandRequest struct {
	Command string            `json:"command"`
	Cwd     string            `json:"cwd,omitempty"`
	Timeout int64             `json:"timeout,omitempty"`
	Envs    map[string]string `json:"envs,omitempty"`
}

// ssePayload is the JSON object carried by each execd SSE/NDJSON event. Only the
// fields cocola needs to drive ExecEvent are decoded. Both the spec-nested error
// object and the legacy flat ename/evalue form are tolerated, mirroring the
// upstream SDK's processStreamEvent.
type ssePayload struct {
	Type     string `json:"type"`
	Text     string `json:"text"`
	ExitCode *int   `json:"exit_code,omitempty"`
	EValue   string `json:"evalue,omitempty"`
	Error    *struct {
		EValue string `json:"evalue,omitempty"`
		EName  string `json:"ename,omitempty"`
	} `json:"error,omitempty"`
}

// --- SandboxProvider implementation ------------------------------------------

// Create provisions a sandbox via POST /v1/sandboxes and records the
// cocola->opensandbox id mapping.
func (p *Provider) Create(ctx context.Context, spec provider.SandboxSpec) (*provider.Sandbox, error) {
	req := createSandboxRequest{
		Env:      spec.Env,
		Metadata: map[string]string{"cocola.user_id": spec.UserID, "cocola.session_id": spec.SessionID},
	}
	if spec.Image != "" {
		req.Image = &imageSpec{URI: spec.Image}
	}
	if rl := mapResources(spec.Resources); len(rl) > 0 {
		req.ResourceLimits = rl
	}
	// Egress mapping: cocola's allowlist -> OpenSandbox networkPolicy
	// (default-deny + per-domain allow). Whether cocola's own egress
	// NetworkPolicy or OpenSandbox's takes ownership is a P2 decision; the
	// mapping is included here to exercise the field, not to settle ownership.
	if spec.Networking.EgressAllowlist != nil {
		np := &networkPolicy{DefaultAction: "deny"}
		for _, d := range spec.Networking.EgressAllowlist {
			np.Egress = append(np.Egress, networkRule{Action: "allow", Target: d})
		}
		req.NetworkPolicy = np
	}

	var info sandboxInfo
	if err := p.do(ctx, http.MethodPost, "/sandboxes", req, &info); err != nil {
		return nil, err
	}
	if info.ID == "" {
		return nil, fmt.Errorf("opensandbox: create returned empty sandbox id")
	}

	sid := info.ID // PoC: reuse the OpenSandbox id as the cocola sandbox id.
	p.mu.Lock()
	p.ids[sid] = info.ID
	p.mu.Unlock()

	return &provider.Sandbox{
		ID:        sid,
		UserID:    spec.UserID,
		SessionID: spec.SessionID,
		Endpoint:  p.baseURL,
	}, nil
}

// Health maps the OpenSandbox lifecycle state to a cocola HealthStatus.
// Running is healthy; every other state (Pending/Pausing/Paused/Stopping/
// Terminated/Failed) is reported unhealthy with the state as detail.
func (p *Provider) Health(ctx context.Context, sid string) (*provider.HealthStatus, error) {
	osbID, err := p.resolve(sid)
	if err != nil {
		return nil, err
	}
	var info sandboxInfo
	if err := p.do(ctx, http.MethodGet, "/sandboxes/"+osbID, nil, &info); err != nil {
		return nil, err
	}
	healthy := info.Status.State == "Running"
	detail := info.Status.State
	if info.Status.Message != "" {
		detail = info.Status.State + ": " + info.Status.Message
	}
	return &provider.HealthStatus{Healthy: healthy, Detail: detail}, nil
}

// Destroy deletes the sandbox via DELETE /v1/sandboxes/{id} and drops the
// local id mapping.
func (p *Provider) Destroy(ctx context.Context, sid string) error {
	osbID, err := p.resolve(sid)
	if err != nil {
		return err
	}
	if err := p.do(ctx, http.MethodDelete, "/sandboxes/"+osbID, nil, nil); err != nil {
		return err
	}
	p.mu.Lock()
	delete(p.ids, sid)
	p.mu.Unlock()
	return nil
}

// Exec runs a command inside the sandbox and bridges OpenSandbox's execd SSE
// stream into cocola's <-chan ExecEvent. It is a two-step operation:
//
//  1. Resolve the per-sandbox execd endpoint via the lifecycle endpoints API
//     (GET /sandboxes/{id}/endpoints/44772). The response carries the reachable
//     URL plus any auth/routing headers (e.g. X-EXECD-ACCESS-TOKEN).
//  2. Open an SSE POST to {execd}/command and translate each event:
//     stdout/stderr -> ExecEvent{Stdout|Stderr}; error -> ExecEventExit with the
//     parsed exit code (or ExecEventError if the value is not numeric);
//     execution_complete -> ExecEventExit{Exit:0} when no error preceded it.
//
// The channel is closed exactly once, when the stream ends or the context is
// cancelled. A non-zero req.Timeout bounds the run; 0 falls back to
// defaultExecTimeout. cocola joins req.Cmd with spaces into a single shell
// command, matching the docker/k8s backends' shell-exec contract.
func (p *Provider) Exec(ctx context.Context, sid string, req provider.ExecRequest) (<-chan provider.ExecEvent, error) {
	osbID, err := p.resolve(sid)
	if err != nil {
		return nil, err
	}
	if len(req.Cmd) == 0 {
		return nil, fmt.Errorf("opensandbox: empty command")
	}

	execdURL, headers, err := p.resolveExecd(ctx, osbID)
	if err != nil {
		return nil, fmt.Errorf("opensandbox: resolve execd: %w", err)
	}

	body := runCommandRequest{
		Command: strings.Join(req.Cmd, " "),
		Cwd:     req.Cwd,
		Envs:    req.Env,
	}
	if req.Timeout > 0 {
		body.Timeout = int64(req.Timeout)
	}
	b, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("opensandbox: marshal command: %w", err)
	}

	timeout := defaultExecTimeout
	if req.Timeout > 0 {
		timeout = time.Duration(req.Timeout) * time.Second
	}
	// Child context bounds the stream lifetime; cancelled when the bridge
	// goroutine returns so the underlying connection is always released.
	runCtx, cancel := context.WithTimeout(ctx, timeout)

	httpReq, err := http.NewRequestWithContext(runCtx, http.MethodPost, execdURL+"/command", bytes.NewReader(b))
	if err != nil {
		cancel()
		return nil, fmt.Errorf("opensandbox: new exec request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	for k, v := range headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := p.stream.Do(httpReq)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("opensandbox: exec stream: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		rb, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		cancel()
		return nil, fmt.Errorf("opensandbox: exec POST /command: status %d: %s", resp.StatusCode, strings.TrimSpace(string(rb)))
	}

	out := make(chan provider.ExecEvent, execEventBuffer)
	go func() {
		defer close(out)
		defer cancel()
		defer resp.Body.Close()
		bridgeExecSSE(runCtx, resp.Body, out)
	}()
	return out, nil
}

// Pause maps to the lifecycle POST /sandboxes/{id}/pause endpoint, which freezes
// a running sandbox while preserving its in-memory state.
func (p *Provider) Pause(ctx context.Context, sid string) error {
	osbID, err := p.resolve(sid)
	if err != nil {
		return err
	}
	return p.do(ctx, http.MethodPost, "/sandboxes/"+osbID+"/pause", nil, nil)
}

// Resume maps to the lifecycle POST /sandboxes/{id}/resume endpoint.
func (p *Provider) Resume(ctx context.Context, sid string) error {
	osbID, err := p.resolve(sid)
	if err != nil {
		return err
	}
	return p.do(ctx, http.MethodPost, "/sandboxes/"+osbID+"/resume", nil, nil)
}

// WriteFile is not implemented in this PoC. It maps to an execd multipart
// upload against the resolved execd endpoint and is off the streaming-exec
// critical path.
func (p *Provider) WriteFile(ctx context.Context, sid, path string, data []byte) error {
	return errNotImplemented
}

// ReadFile is not implemented in this PoC. It maps to an execd ranged download
// against the resolved execd endpoint and is off the streaming-exec critical
// path.
func (p *Provider) ReadFile(ctx context.Context, sid, path string) ([]byte, error) {
	return nil, errNotImplemented
}

// --- helpers -----------------------------------------------------------------

// mapResources converts cocola Resources to OpenSandbox resourceLimits.
// CPU cores -> milli-cpu string (e.g. 0.5 -> "500m"); memory MiB -> "<n>Mi".
func mapResources(r provider.Resources) map[string]string {
	out := map[string]string{}
	if r.CPUCores > 0 {
		out["cpu"] = fmt.Sprintf("%dm", int64(r.CPUCores*1000))
	}
	if r.MemoryMiB > 0 {
		out["memory"] = fmt.Sprintf("%dMi", r.MemoryMiB)
	}
	return out
}

// resolveExecd asks the lifecycle endpoints API for the reachable execd URL of
// a sandbox and the headers to replay on execd calls. If the response omits an
// auth header, the provider API key is added as a fallback, matching the SDK's
// server-proxy behaviour.
func (p *Provider) resolveExecd(ctx context.Context, osbID string) (string, map[string]string, error) {
	var ep endpointInfo
	path := fmt.Sprintf("/sandboxes/%s/endpoints/%d", osbID, execdPort)
	if err := p.do(ctx, http.MethodGet, path, nil, &ep); err != nil {
		return "", nil, err
	}
	if ep.Endpoint == "" {
		return "", nil, fmt.Errorf("opensandbox: empty execd endpoint for %s", osbID)
	}
	url := ep.Endpoint
	if !strings.HasPrefix(url, "http") {
		url = "http://" + url
	}
	url = strings.TrimRight(url, "/")

	headers := make(map[string]string, len(ep.Headers)+1)
	for k, v := range ep.Headers {
		headers[k] = v
	}
	if _, ok := headers[execdAuthHeader]; !ok && p.apiKey != "" {
		headers[execdAuthHeader] = p.apiKey
	}
	return url, headers, nil
}

// bridgeExecSSE reads an execd SSE/NDJSON stream from r and emits cocola
// ExecEvents on out. It handles both wire forms the execd server uses:
// standard "data:"-prefixed SSE lines and bare NDJSON JSON objects, with blank
// lines (SSE) or newlines (NDJSON) delimiting events. The scanner buffer is
// grown to 4 MiB so a large single stdout chunk does not split mid-event.
//
// Exit semantics mirror the upstream SDK's processStreamEvent: an "error" event
// whose value parses as an integer becomes ExecEventExit{Exit:n}; a non-numeric
// error value becomes ExecEventError; "execution_complete" with no prior error
// yields ExecEventExit{Exit:0}. Context cancellation surfaces as ExecEventError.
func bridgeExecSSE(ctx context.Context, r io.Reader, out chan<- provider.ExecEvent) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)

	var dataLines []string
	exited := false

	dispatch := func(payload string) {
		if payload == "" {
			return
		}
		if processSSEPayload(payload, out) {
			exited = true
		}
	}

	for {
		select {
		case <-ctx.Done():
			out <- provider.ExecEvent{Kind: provider.ExecEventError, Err: ctx.Err()}
			return
		default:
		}

		if !scanner.Scan() {
			if len(dataLines) > 0 {
				dispatch(strings.Join(dataLines, "\n"))
			}
			if err := scanner.Err(); err != nil {
				out <- provider.ExecEvent{Kind: provider.ExecEventError, Err: fmt.Errorf("opensandbox: sse read: %w", err)}
				return
			}
			// Stream ended cleanly. Synthesize a success exit if the server
			// never sent an explicit terminal event, so callers always see a
			// terminal ExecEventExit.
			if !exited {
				out <- provider.ExecEvent{Kind: provider.ExecEventExit, Exit: 0}
			}
			return
		}

		line := scanner.Text()
		switch {
		case line == "":
			if len(dataLines) > 0 {
				dispatch(strings.Join(dataLines, "\n"))
				dataLines = nil
			}
		case strings.HasPrefix(line, ":"):
			// SSE comment line, ignore.
		case strings.HasPrefix(line, "{"):
			// NDJSON: one JSON object per line.
			dispatch(line)
		case strings.HasPrefix(line, "data:"):
			v := strings.TrimPrefix(line, "data:")
			v = strings.TrimPrefix(v, " ")
			dataLines = append(dataLines, v)
		default:
			// Other SSE fields (event:, id:) carry no payload cocola needs.
		}
		if exited {
			// Terminal event already emitted; stop. The deferred Body.Close in
			// Exec releases the connection.
			return
		}
	}
}

// processSSEPayload decodes one JSON event payload and emits the matching
// ExecEvent(s). It returns true if the event was terminal (an exit or error was
// emitted), so the caller can suppress a synthesized success exit.
func processSSEPayload(payload string, out chan<- provider.ExecEvent) bool {
	var ev ssePayload
	if err := json.Unmarshal([]byte(payload), &ev); err != nil {
		// Not JSON: treat the raw text as stdout.
		out <- provider.ExecEvent{Kind: provider.ExecEventStdout, Stdout: []byte(payload)}
		return false
	}

	switch ev.Type {
	case "stdout":
		out <- provider.ExecEvent{Kind: provider.ExecEventStdout, Stdout: []byte(ev.Text)}
	case "stderr":
		out <- provider.ExecEvent{Kind: provider.ExecEventStderr, Stderr: []byte(ev.Text)}
	case "error":
		evalue := ev.EValue
		if ev.Error != nil && ev.Error.EValue != "" {
			evalue = ev.Error.EValue
		}
		if code, err := strconv.Atoi(evalue); err == nil {
			out <- provider.ExecEvent{Kind: provider.ExecEventExit, Exit: int32(code)}
		} else {
			msg := evalue
			if msg == "" && ev.Error != nil {
				msg = ev.Error.EName
			}
			out <- provider.ExecEvent{Kind: provider.ExecEventError, Err: fmt.Errorf("opensandbox: exec error: %s", msg)}
		}
		return true
	case "execution_complete":
		exit := int32(0)
		if ev.ExitCode != nil {
			exit = int32(*ev.ExitCode)
		}
		out <- provider.ExecEvent{Kind: provider.ExecEventExit, Exit: exit}
		return true
	case "init", "ping", "result":
		// Lifecycle/keepalive/result events carry nothing cocola streams.
	default:
		if ev.Text != "" {
			out <- provider.ExecEvent{Kind: provider.ExecEventStdout, Stdout: []byte(ev.Text)}
		}
	}
	return false
}

func (p *Provider) resolve(sid string) (string, error) {
	p.mu.RLock()
	osbID, ok := p.ids[sid]
	p.mu.RUnlock()
	if !ok {
		// Fall back to treating sid as the opensandbox id directly: lets a
		// fresh provider instance (e.g. after a restart) still address a
		// sandbox it did not create in-process.
		if sid == "" {
			return "", fmt.Errorf("opensandbox: empty sandbox id")
		}
		return sid, nil
	}
	return osbID, nil
}

// do issues a JSON REST call. body is marshaled when non-nil; out is unmarshaled
// when non-nil. Non-2xx responses become errors carrying the status and body.
func (p *Provider) do(ctx context.Context, method, path string, body, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("opensandbox: marshal %s %s: %w", method, path, err)
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, p.baseURL+path, rdr)
	if err != nil {
		return fmt.Errorf("opensandbox: new request %s %s: %w", method, path, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if p.apiKey != "" {
		req.Header.Set(apiKeyHeader, p.apiKey)
	}

	resp, err := p.http.Do(req)
	if err != nil {
		return fmt.Errorf("opensandbox: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("opensandbox: %s %s: status %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(b)))
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("opensandbox: decode %s %s: %w", method, path, err)
		}
	}
	return nil
}

// compile-time assertion: Provider satisfies the core contract.
var _ provider.SandboxProvider = (*Provider)(nil)
