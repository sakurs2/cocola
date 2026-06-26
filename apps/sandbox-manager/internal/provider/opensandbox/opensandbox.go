// Package opensandbox implements SandboxProvider on top of an OpenSandbox
// control-plane server (https://github.com/opensandbox-group/OpenSandbox).
//
// This is the #18 PoC skeleton (ADR-0013). It wraps the OpenSandbox REST
// lifecycle API (POST/GET/DELETE /v1/sandboxes, OPEN-SANDBOX-API-KEY header)
// as a cocola backend, proving the seam works WITHOUT touching the core
// SandboxProvider interface or the docker/k8s backends (ADR-0002).
//
// Scope of this skeleton: Create / Health / Destroy are implemented end to end
// against the REST API. The remaining five methods (Exec / WriteFile /
// ReadFile / Pause / Resume) return errNotImplemented on purpose; they are the
// subject of P2, where Exec maps to the SDK's SSE stream and Pause/Resume map
// to OpenSandbox snapshots. See docs/archive/opensandbox-poc-p0-research.md for
// the full REST<->interface mapping that this package is built against.
//
// The REST client here is deliberately stdlib-only (net/http + encoding/json)
// rather than importing github.com/alibaba/OpenSandbox/sdks/sandbox/go: the
// three lifecycle calls have no streaming, so a thin client keeps the PoC
// self-contained and offline-buildable. P2 will evaluate adopting the official
// Go SDK for the streaming Exec path, where its SSE/NDJSON handling earns its
// keep.
package opensandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
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

// defaultHTTPTimeout bounds a single lifecycle REST call. Streaming exec (P2)
// will use a separate, unbounded-by-timeout path.
const defaultHTTPTimeout = 30 * time.Second

// errNotImplemented marks the methods deferred to P2. Returning a clear sentinel
// (rather than panicking) keeps the provider safe to register: selecting it only
// fails the operations not yet built, never the process.
var errNotImplemented = errors.New("opensandbox: not implemented in PoC skeleton (P1); deferred to P2")

// Provider is an OpenSandbox-backed SandboxProvider. It holds no sandbox state
// of its own beyond a cocola-sid -> opensandbox-id map; the OpenSandbox server
// is the source of truth for lifecycle.
type Provider struct {
	baseURL string // includes the /v1 version prefix, e.g. http://host:8090/v1
	apiKey  string
	http    *http.Client

	mu  sync.RWMutex
	ids map[string]string // cocola sandbox id -> opensandbox id
}

// Option configures the Provider.
type Option func(*Provider)

// WithBaseURL overrides the OpenSandbox server base URL (must include /v1).
func WithBaseURL(u string) Option { return func(p *Provider) { p.baseURL = strings.TrimRight(u, "/") } }

// WithAPIKey overrides the API key.
func WithAPIKey(k string) Option { return func(p *Provider) { p.apiKey = k } }

// WithHTTPClient injects a custom http.Client. This is the seam unit tests use
// to supply a stub RoundTripper, so no real socket is ever opened.
func WithHTTPClient(c *http.Client) Option { return func(p *Provider) { p.http = c } }

// New constructs an OpenSandbox provider. Connection settings come from env by
// default (COCOLA_OPENSANDBOX_URL, COCOLA_OPENSANDBOX_API_KEY) and can be
// overridden by options.
func New(opts ...Option) (*Provider, error) {
	p := &Provider{
		baseURL: strings.TrimRight(os.Getenv("COCOLA_OPENSANDBOX_URL"), "/"),
		apiKey:  os.Getenv("COCOLA_OPENSANDBOX_API_KEY"),
		http:    &http.Client{Timeout: defaultHTTPTimeout},
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

// Exec is deferred to P2 (maps to the OpenSandbox SSE/NDJSON exec stream).
func (p *Provider) Exec(ctx context.Context, sid string, req provider.ExecRequest) (<-chan provider.ExecEvent, error) {
	return nil, errNotImplemented
}

// WriteFile is deferred to P2 (maps to UploadFile).
func (p *Provider) WriteFile(ctx context.Context, sid, path string, data []byte) error {
	return errNotImplemented
}

// ReadFile is deferred to P2 (maps to DownloadFile).
func (p *Provider) ReadFile(ctx context.Context, sid, path string) ([]byte, error) {
	return nil, errNotImplemented
}

// Pause is deferred to P2 (maps to POST /sandboxes/{id}/pause + snapshot).
func (p *Provider) Pause(ctx context.Context, sid string) error { return errNotImplemented }

// Resume is deferred to P2 (maps to POST /sandboxes/{id}/resume).
func (p *Provider) Resume(ctx context.Context, sid string) error { return errNotImplemented }

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
