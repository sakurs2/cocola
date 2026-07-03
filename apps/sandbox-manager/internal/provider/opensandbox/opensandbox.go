// Package opensandbox implements SandboxProvider on top of an OpenSandbox
// control-plane server (https://github.com/opensandbox-group/OpenSandbox).
//
// This is the #18 PoC skeleton (ADR-0013). It wraps the OpenSandbox REST
// lifecycle API (POST/GET/DELETE /v1/sandboxes, OPEN-SANDBOX-API-KEY header)
// as a cocola backend, proving the seam works WITHOUT touching the core
// SandboxProvider interface or the docker/k8s backends (ADR-0002).
//
// Scope: Create / Health / Destroy / Exec / Pause / Resume are implemented
// against the REST + execd APIs. Create maps cocola's filesystem model onto
// OpenSandbox volumes (see mapVolumes): per-session workspace, per-session
// Claude config, read-only platform skills. Exec resolves the per-sandbox execd endpoint
// (lifecycle GET /sandboxes/{id}/endpoints/44772) and bridges its SSE/NDJSON
// command stream into cocola's <-chan ExecEvent. Pause/Resume map to the
// lifecycle POST .../pause and .../resume endpoints. WriteFile / ReadFile map to
// execd's multipart POST /files/upload and GET /files/download against the same
// resolved endpoint, completing all 8 SandboxProvider methods. See
// docs/archive/opensandbox-poc-p0-research.md
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
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"mime/multipart"
	"net/http"
	"net/textproto"
	neturl "net/url"
	"os"
	"path/filepath"
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

// defaultHTTPTimeout bounds a single lifecycle REST call. Kubernetes-backed
// OpenSandbox create waits up to 60s for the sandbox pod to become Running, so
// the client timeout must be longer than the server-side wait to surface the
// real K8s reason instead of a local context deadline. Streaming exec uses a
// separate client whose timeout is governed by the per-Exec context/deadline.
const defaultHTTPTimeout = 90 * time.Second

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

// resumeWaitTimeout / resumePollInterval bound the wait for a thawed sandbox to
// report Running before Exec opens the execd stream (see thawIfPaused).
const (
	resumeWaitTimeout  = 30 * time.Second
	resumePollInterval = 250 * time.Millisecond
)

// execReadyTimeout / execReadyPollInterval bound the wait for execd to start
// accepting commands after the container reports Running. A freshly started
// (or freshly resumed) sandbox container races: the lifecycle state flips to
// Running the instant the container process starts, but execd has not yet bound
// its listening socket, so the server proxy returns 500 "Server disconnected
// without sending a response" for that brief window. waitExecdReady probes with
// an idempotent no-op command until execd answers (see waitExecdReady).
const (
	execReadyTimeout      = 30 * time.Second
	execReadyPollInterval = 300 * time.Millisecond
)

// execEventBuffer sizes the Exec result channel so a fast-producing SSE stream
// does not block on a momentarily slow consumer. Matches the docker backend.
const execEventBuffer = 32

// Guest path contract: these MUST match the docker provider (docker.go) and the
// brain image (deploy/sandbox-runtime/Dockerfile). Both backends mount the same
// brain image, so the in-container paths are a shared contract, not a per-
// provider choice.
const (
	// guestWorkspace is the user-visible per-session workspace root.
	guestWorkspace = "/workspace"
	// guestClaudeConfig is the hidden session-local Claude Code config root.
	guestClaudeConfig = "/home/cocola/.claude"
	// guestPlugins is the read-only platform-skill mount.
	guestPlugins = "/data/plugins"
	// workspaceSubPath and claudeSubPath split one session PVC into a visible
	// workspace and hidden Claude state. The state remains session-scoped without
	// leaking .claude into /workspace file listings.
	workspaceSubPath = "workspace"
	claudeSubPath    = "claude"
	// pluginsClaimName is the shared, pre-provisioned platform-skill volume.
	// It is mounted read-only into every sandbox.
	pluginsClaimName  = "cocola-plugins"
	volumeBackendPVC  = "pvc"
	volumeBackendHost = "host"
	// defaultCPU / defaultMemory are applied when a SandboxSpec carries no
	// Resources. OpenSandbox rejects a non-pooled create without resourceLimits
	// ("resourceLimits is required when poolRef is not provided"), and the
	// on-demand allocation path (binder -> Create, ADR-0015) sets no Resources,
	// so the provider must supply a sane floor itself. Override via
	// COCOLA_OPENSANDBOX_DEFAULT_CPU / _DEFAULT_MEMORY (raw resourceLimits
	// strings, e.g. "500m" / "512Mi").
	defaultCPU    = "500m"
	defaultMemory = "512Mi"
	// sandboxExecUser is the non-root user every Exec runs as, matching the
	// brain image's Dockerfile (useradd -u 10001 cocola). execd runs the
	// /command body as root by default; the claude CLI refuses
	// --dangerously-skip-permissions under root for safety, so the control
	// plane MUST drop to this user -- mirroring the docker provider, which pins
	// `docker exec --user cocola`. Overridable via COCOLA_OPENSANDBOX_EXEC_USER
	// (empty disables the drop, i.e. run as the image default / root).
	sandboxExecUser = "cocola"
)

// Provider is an OpenSandbox-backed SandboxProvider. It holds no sandbox state
// of its own beyond a cocola-sid -> opensandbox-id map; the OpenSandbox server
// is the source of truth for lifecycle.
type Provider struct {
	baseURL string // includes the /v1 version prefix, e.g. http://host:8090/v1
	apiKey  string

	// useServerProxy controls how the execd endpoint is reached. When true
	// (the default), Exec asks the server for a server-proxied endpoint URL
	// (`{server}/sandboxes/{id}/proxy/{port}`), which is reachable by any client
	// that can reach the server. When false the server returns the sandbox's
	// direct endpoint (e.g. host.docker.internal:PORT), which is only reachable
	// from inside the sandbox network -- appropriate only when sandbox-manager is
	// co-located with the sandboxes.
	useServerProxy bool
	http           *http.Client // bounded client for lifecycle REST calls

	// stream is used for the long-lived execd SSE connection. It has no
	// client-level timeout: each Exec governs its own lifetime via the
	// request context's deadline, so a 10-minute command is not killed by a
	// 30s lifecycle timeout.
	stream *http.Client

	mu  sync.RWMutex
	ids map[string]string // cocola sandbox id -> opensandbox id

	// execUser is the OS user every Exec drops to (default "cocola", uid 10001).
	// Empty runs commands as execd's default (root). See sandboxExecUser.
	execUser string

	// volumeBackend selects how cocola exposes persistent session storage to
	// OpenSandbox. "pvc" preserves the historical Docker-volume/K8s-PVC request
	// shape; "host" points OpenSandbox at directories under root, which may be an
	// NFS/NAS mount already present on every node.
	volumeBackend string
	root          string
}

// Option configures the Provider.
type Option func(*Provider)

// WithBaseURL overrides the OpenSandbox server base URL (must include /v1).
func WithBaseURL(u string) Option { return func(p *Provider) { p.baseURL = strings.TrimRight(u, "/") } }

// WithAPIKey overrides the API key.
func WithAPIKey(k string) Option { return func(p *Provider) { p.apiKey = k } }

// WithServerProxy controls whether Exec reaches execd via the server proxy
// (true, default) or the sandbox's direct endpoint (false). Direct only works
// when the caller shares the sandbox network.
func WithServerProxy(v bool) Option { return func(p *Provider) { p.useServerProxy = v } }

// WithExecUser overrides the OS user Exec drops to (default "cocola"). An empty
// string disables the privilege drop, running commands as execd's default user.
func WithExecUser(u string) Option { return func(p *Provider) { p.execUser = u } }

// WithVolumeBackend overrides COCOLA_SANDBOX_VOLUME_BACKEND (pvc|host).
func WithVolumeBackend(v string) Option {
	return func(p *Provider) { p.volumeBackend = strings.TrimSpace(strings.ToLower(v)) }
}

// WithRoot overrides COCOLA_SANDBOX_ROOT for host-backed volumes.
func WithRoot(root string) Option { return func(p *Provider) { p.root = root } }

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
	home, _ := os.UserHomeDir()
	root := filepath.Join(home, ".cocola", "sandboxes")
	if r := os.Getenv("COCOLA_SANDBOX_ROOT"); r != "" {
		root = r
	}
	p := &Provider{
		baseURL: strings.TrimRight(os.Getenv("COCOLA_OPENSANDBOX_URL"), "/"),
		apiKey:  os.Getenv("COCOLA_OPENSANDBOX_API_KEY"),
		http:    &http.Client{Timeout: httpTimeoutFromEnv()},
		// No timeout: the Exec context governs stream lifetime. Keep-alives are
		// DISABLED on purpose: the OpenSandbox server proxy ({server}/sandboxes/
		// {id}/proxy/{port}) mishandles connection reuse for streaming exec -- a
		// pooled connection from the waitExecdReady probe, reused for the real
		// command, truncates the command's SSE stream after ~1s (only an empty
		// EXIT is seen). A fresh connection per request sidesteps the proxy's
		// connection-state bug at negligible cost (each exec already lives for
		// minutes, so per-exec connection setup is noise).
		stream: &http.Client{Transport: &http.Transport{DisableKeepAlives: true}},
		ids:    map[string]string{},
		// Default to the always-reachable server-proxy endpoint. Opt out via
		// COCOLA_OPENSANDBOX_DIRECT_EXEC=1 (or WithServerProxy(false)) only when
		// sandbox-manager runs inside the sandbox network.
		useServerProxy: !envTruthy("COCOLA_OPENSANDBOX_DIRECT_EXEC"),
		// Drop privileges to the brain image's non-root user by default so the
		// in-sandbox claude CLI accepts --dangerously-skip-permissions. Override
		// (incl. "" to disable) via COCOLA_OPENSANDBOX_EXEC_USER.
		execUser:      execUserFromEnv(),
		volumeBackend: volumeBackendFromEnv(),
		root:          root,
	}
	for _, o := range opts {
		o(p)
	}
	if p.baseURL == "" {
		return nil, fmt.Errorf("opensandbox: base URL not set (COCOLA_OPENSANDBOX_URL or WithBaseURL)")
	}
	switch p.volumeBackend {
	case "", volumeBackendPVC:
		p.volumeBackend = volumeBackendPVC
	case volumeBackendHost:
		if p.root == "" {
			return nil, fmt.Errorf("opensandbox: host volume backend requires COCOLA_SANDBOX_ROOT or WithRoot")
		}
	default:
		return nil, fmt.Errorf("opensandbox: unsupported volume backend %q (want pvc or host)", p.volumeBackend)
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
	Entrypoint     []string          `json:"entrypoint,omitempty"`
	ResourceLimits map[string]string `json:"resourceLimits,omitempty"`
	Env            map[string]string `json:"env,omitempty"`
	Metadata       map[string]string `json:"metadata,omitempty"`
	NetworkPolicy  *networkPolicy    `json:"networkPolicy,omitempty"`
	Volumes        []volumeSpec      `json:"volumes,omitempty"`
}

// volumeSpec is one entry of CreateSandboxRequest.volumes. Exactly one backend
// (here always PVC) is set; the common fields MountPath/ReadOnly/SubPath apply
// regardless of backend. cocola only uses the pvc backend (Docker named volume
// locally, K8s PVC in prod) — see docs/plan/opensandbox-volume-mapping.md.
type volumeSpec struct {
	// Name is a per-request unique volume identifier (server-required).
	Name      string       `json:"name"`
	PVC       *pvcBackend  `json:"pvc,omitempty"`
	Host      *hostBackend `json:"host,omitempty"`
	MountPath string       `json:"mountPath"`
	ReadOnly  bool         `json:"readOnly,omitempty"`
	SubPath   string       `json:"subPath,omitempty"`
}

// pvcBackend is the OpenSandbox PVC volume backend. ClaimName names the volume
// (K8s PVC name / Docker named-volume name). CreateIfNotExists lets the server
// provision it on first use so cocola need not pre-create per-user/session
// volumes. Storage/StorageClass/AccessModes/DeleteOnSandboxTermination are
// passed through only when set.
type pvcBackend struct {
	ClaimName                  string   `json:"claimName"`
	CreateIfNotExists          bool     `json:"createIfNotExists,omitempty"`
	Storage                    string   `json:"storage,omitempty"`
	StorageClass               string   `json:"storageClass,omitempty"`
	AccessModes                []string `json:"accessModes,omitempty"`
	DeleteOnSandboxTermination bool     `json:"deleteOnSandboxTermination,omitempty"`
}

// hostBackend is the OpenSandbox host volume backend. Path must be allowed by
// the server's [storage].allowed_host_paths and visible at the same absolute
// path from the OpenSandbox server container and the host Docker daemon.
type hostBackend struct {
	Path string `json:"path"`
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
		// OpenSandbox requires a non-empty entrypoint whenever an image is given.
		// cocola sandboxes are long-lived (create once, then Exec into them), so the
		// entry process is a no-op idle-blocker and real work is driven via Exec.
		//
		// The entry process runs as root (the brain image has no USER directive), so
		// we use it for a ONE-TIME chown of the freshly-provisioned, root-owned PVCs
		// to the Exec user (uid 10001 cocola) before blocking. Without this, every
		// Exec drops to non-root cocola (see execUser) and cannot write /workspace or
		// /home/cocola/.claude, which breaks project writes and --resume session
		// files (a dangling resume then degrades to a fresh conversation). The docker
		// provider fixes this by chowning the host bind-mount; opensandbox volumes
		// live inside the server runtime and are unreachable from here, and the server
		// API exposes no fsGroup/securityContext, so the entrypoint is the only seam.
		req.Entrypoint = chownEntrypoint(p.execUser)
	}
	if rl := mapResources(spec.Resources); len(rl) > 0 {
		req.ResourceLimits = rl
	}
	// Volume mapping: per-session workspace, per-session Claude config, plus
	// read-only platform skills. See mapVolumes. Claude config is mounted outside
	// /workspace so user file listings do not expose it.
	vols, err := p.mapVolumes(spec.UserID, spec.SessionID)
	if err != nil {
		return nil, err
	}
	req.Volumes = vols
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

// CleanupSessionStorage removes the durable storage owned by one conversation
// when host-backed volumes are enabled. PVC storage is intentionally left to
// cluster/volume lifecycle tooling because this client only sends OpenSandbox
// lifecycle requests and has no Kubernetes/Docker volume API access here.
func (p *Provider) CleanupSessionStorage(ctx context.Context, userID, sessionID string) error {
	if p.volumeBackend != volumeBackendHost {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	dir := p.sessionRoot(userID, sessionID)
	if !isSubpath(p.root, dir) {
		return fmt.Errorf("opensandbox: refusing to remove session dir outside root: %s", dir)
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("opensandbox: remove session storage: %w", err)
	}
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

	// On-demand allocation (ADR-0015) pauses an idle sandbox between turns to
	// free memory; execd in a paused container accepts the POST but the frozen
	// process never runs, so the stream returns empty (no stdout, no error).
	// Thaw before exec -- mirrors the docker provider's thawIfPaused.
	if err := p.thawIfPaused(ctx, osbID); err != nil {
		return nil, err
	}

	execdURL, headers, err := p.resolveExecd(ctx, osbID)
	if err != nil {
		return nil, fmt.Errorf("opensandbox: resolve execd: %w", err)
	}

	// A freshly started or freshly resumed container reports Running before
	// execd has bound its listening socket; an exec in that window gets a 500
	// "Server disconnected" from the server proxy. The real command may be
	// non-idempotent (e.g. it drives the brain), so probe readiness first with
	// an idempotent no-op until execd answers.
	if err := p.waitExecdReady(ctx, execdURL, headers); err != nil {
		return nil, err
	}

	// cocola ExecRequest.Cmd is an argv (like docker exec), but execd's
	// /command takes a single shell string and re-parses it with a shell.
	// Naively space-joining would double-shell (e.g. ["sh","-c","a; b"] would
	// be reparsed so that only "a" runs under the inner sh). Shell-quote each
	// argv element so execd's shell reconstructs the exact argv we intended.
	command := shellJoin(req.Cmd)

	// Drop privileges to the non-root brain user. execd runs the /command body
	// as root; the in-sandbox claude CLI refuses --dangerously-skip-permissions
	// under root, so we re-exec the command as p.execUser via runuser. runuser
	// runs the argv DIRECTLY (no nested shell), preserves the environment --
	// including the injected ANTHROPIC_* creds -- and sets HOME to the user's
	// home so ~/.claude resolves. This mirrors the docker provider's
	// `docker exec --user cocola`. Empty execUser keeps the execd default user.
	if p.execUser != "" {
		command = fmt.Sprintf("runuser -u %s -- %s", shellQuote(p.execUser), command)
	}

	// execd's /command API has no stdin field (specs/execd-api.yaml
	// RunCommandRequest is command/cwd/envs/timeout/uid/gid only), so cocola's
	// ExecRequest.Stdin cannot be delivered natively. The Route A shim reads its
	// one-shot Request JSON from stdin, so we MUST feed it. Since the command is
	// re-parsed by a shell, we pipe stdin in-shell: base64-encode the bytes
	// (binary-safe, and the base64 alphabet needs no shell quoting) and decode
	// them back through a pipe INTO the command. The pipe is prepended AFTER the
	// runuser wrap so the bytes flow into runuser's stdin, which forwards them to
	// the target process -- a single, flat shell pipeline with no nested
	// `bash -c` quoting (nesting broke shell parsing: "unexpected EOF").
	if len(req.Stdin) > 0 {
		b64 := base64.StdEncoding.EncodeToString(req.Stdin)
		command = fmt.Sprintf("printf %%s '%s' | base64 -d | %s", b64, command)
	}

	body := runCommandRequest{
		Command: command,
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

// thawIfPaused resumes a paused sandbox (by opensandbox id) and waits for it to
// report Running before Exec opens the execd stream. execd in a Paused container
// silently swallows commands (the process is frozen), so an exec against a
// paused sandbox would hang/return empty -- the on-demand allocation model
// (ADR-0015) pauses idle sandboxes between turns, so this is the common case for
// any follow-up turn. A failed status read is non-fatal: let the exec path
// surface the authoritative error rather than masking it here.
func (p *Provider) thawIfPaused(ctx context.Context, osbID string) error {
	var info sandboxInfo
	if err := p.do(ctx, http.MethodGet, "/sandboxes/"+osbID, nil, &info); err != nil {
		return nil // inspect failure is not fatal; exec produces the real error
	}
	switch info.Status.State {
	case "Running":
		return nil
	case "Paused", "Pausing":
		// resume below
	default:
		// Pending/Stopping/Terminated/Failed: not our case to fix; let exec
		// surface the authoritative error.
		return nil
	}
	if err := p.do(ctx, http.MethodPost, "/sandboxes/"+osbID+"/resume", nil, nil); err != nil {
		return fmt.Errorf("opensandbox: resume before exec: %w", err)
	}
	// Wait (bounded) for Running -- resume may be asynchronous.
	deadline := time.Now().Add(resumeWaitTimeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(resumePollInterval):
		}
		var st sandboxInfo
		if err := p.do(ctx, http.MethodGet, "/sandboxes/"+osbID, nil, &st); err != nil {
			continue
		}
		if st.Status.State == "Running" {
			return nil
		}
	}
	// Timed out waiting; proceed anyway and let exec report the truth.
	return nil
}

// waitExecdReady probes execd with an idempotent no-op command until it answers
// with a success status, bounding the wait by execReadyTimeout. It closes the
// readiness race window described at execReadyTimeout: a container can report
// Running before execd binds its socket, in which case the server proxy returns
// 500 "Server disconnected without sending a response" (or the dial fails). Both
// are treated as not-yet-ready and retried. A 2xx means execd is accepting
// commands; the probe body is drained and discarded. Any non-5xx, non-success
// status is returned as a real error (execd is up but rejecting). On timeout the
// last observed error is returned so the caller surfaces the true cause.
func (p *Provider) waitExecdReady(ctx context.Context, execdURL string, headers map[string]string) error {
	// `true` is a shell builtin that always exits 0 with no output -- the
	// cheapest possible idempotent probe.
	b, _ := json.Marshal(runCommandRequest{Command: "true"})
	deadline := time.Now().Add(execReadyTimeout)
	var lastErr error
	for {
		probeCtx, cancel := context.WithTimeout(ctx, execReadyPollInterval*4)
		req, err := http.NewRequestWithContext(probeCtx, http.MethodPost, execdURL+"/command", bytes.NewReader(b))
		if err != nil {
			cancel()
			return fmt.Errorf("opensandbox: execd readiness probe: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "text/event-stream")
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := p.stream.Do(req)
		if err != nil {
			lastErr = err
		} else {
			status := resp.StatusCode
			io.Copy(io.Discard, io.LimitReader(resp.Body, 64*1024))
			resp.Body.Close()
			switch {
			case status >= 200 && status < 300:
				cancel()
				return nil
			case status >= 500:
				// execd not yet listening: proxy reports the upstream as down.
				lastErr = fmt.Errorf("opensandbox: execd not ready: status %d", status)
			default:
				// 4xx etc: execd is up but rejecting -- a real error.
				cancel()
				return fmt.Errorf("opensandbox: execd readiness probe: status %d", status)
			}
		}
		cancel()
		if !time.Now().Before(deadline) {
			if lastErr == nil {
				lastErr = fmt.Errorf("opensandbox: execd not ready after %s", execReadyTimeout)
			}
			return fmt.Errorf("opensandbox: wait execd ready: %w", lastErr)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(execReadyPollInterval):
		}
	}
}

// fileMetadata is the JSON metadata part of an execd POST /files/upload request
// (spec: FileMetadata). owner/group are omitted when empty so the upload keeps
// execd's default ownership; mode is the octal permission integer (0o644 -> 644).
type fileMetadata struct {
	Path  string `json:"path"`
	Owner string `json:"owner,omitempty"`
	Group string `json:"group,omitempty"`
	Mode  int    `json:"mode,omitempty"`
}

// WriteFile uploads data to path inside the sandbox via execd's multipart
// POST /files/upload. The request carries an ordered metadata part (JSON
// FileMetadata, contentType application/json) followed by the file bytes
// (octet-stream). Ownership is set to the Exec user (default "cocola") so files
// written by the control plane are readable by the in-sandbox claude process,
// which runs as that same non-root user; an empty execUser leaves ownership at
// execd's default. Mirrors the docker provider's CopyToContainer semantics
// (whole-file write by absolute path). The endpoint, auth headers, and a thaw
// of a paused sandbox are resolved through the same chain Exec uses.
func (p *Provider) WriteFile(ctx context.Context, sid, path string, data []byte) error {
	osbID, err := p.resolve(sid)
	if err != nil {
		return err
	}
	if err := p.thawIfPaused(ctx, osbID); err != nil {
		return err
	}
	execdURL, headers, err := p.resolveExecd(ctx, osbID)
	if err != nil {
		return fmt.Errorf("opensandbox: resolve execd: %w", err)
	}

	meta := fileMetadata{Path: path, Mode: 0o644}
	if p.execUser != "" {
		meta.Owner = p.execUser
		meta.Group = p.execUser
	}
	metaJSON, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("opensandbox: marshal file metadata: %w", err)
	}

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	// metadata part: execd reads it via multipart FormFile("metadata"), so the
	// part MUST carry a filename (a nameless part is parsed as a plain form
	// value and FormFile then reports "metadata file is missing"). We also pin
	// application/json; CreateFormFile would force octet-stream.
	metaHdr := textproto.MIMEHeader{}
	metaHdr.Set("Content-Disposition", `form-data; name="metadata"; filename="metadata.json"`)
	metaHdr.Set("Content-Type", "application/json")
	mp, err := mw.CreatePart(metaHdr)
	if err != nil {
		return fmt.Errorf("opensandbox: create metadata part: %w", err)
	}
	if _, err := mp.Write(metaJSON); err != nil {
		return fmt.Errorf("opensandbox: write metadata part: %w", err)
	}
	// file part: the bytes themselves, as octet-stream.
	fileHdr := textproto.MIMEHeader{}
	fileHdr.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename=%q`, filepath.Base(path)))
	fileHdr.Set("Content-Type", "application/octet-stream")
	fp, err := mw.CreatePart(fileHdr)
	if err != nil {
		return fmt.Errorf("opensandbox: create file part: %w", err)
	}
	if _, err := fp.Write(data); err != nil {
		return fmt.Errorf("opensandbox: write file part: %w", err)
	}
	if err := mw.Close(); err != nil {
		return fmt.Errorf("opensandbox: close multipart writer: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, execdURL+"/files/upload", &body)
	if err != nil {
		return fmt.Errorf("opensandbox: new upload request: %w", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := p.stream.Do(req)
	if err != nil {
		return fmt.Errorf("opensandbox: upload %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		rb, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("opensandbox: upload %s: status %d: %s", path, resp.StatusCode, strings.TrimSpace(string(rb)))
	}
	return nil
}

// ReadFile downloads the whole file at path from inside the sandbox via execd's
// GET /files/download?path=. It reads the full object (no Range / offset-limit
// line reads), mirroring the docker provider's CopyFromContainer semantics. A
// 404 is wrapped as fs.ErrNotExist so callers can distinguish "missing" from a
// transport error. The endpoint, auth headers, and a thaw of a paused sandbox
// are resolved through the same chain Exec uses.
func (p *Provider) ReadFile(ctx context.Context, sid, path string) ([]byte, error) {
	osbID, err := p.resolve(sid)
	if err != nil {
		return nil, err
	}
	if err := p.thawIfPaused(ctx, osbID); err != nil {
		return nil, err
	}
	execdURL, headers, err := p.resolveExecd(ctx, osbID)
	if err != nil {
		return nil, fmt.Errorf("opensandbox: resolve execd: %w", err)
	}

	q := neturl.Values{}
	q.Set("path", path)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, execdURL+"/files/download?"+q.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("opensandbox: new download request: %w", err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := p.stream.Do(req)
	if err != nil {
		return nil, fmt.Errorf("opensandbox: download %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("opensandbox: download %s: %w", path, fs.ErrNotExist)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		rb, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("opensandbox: download %s: status %d: %s", path, resp.StatusCode, strings.TrimSpace(string(rb)))
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("opensandbox: read download body %s: %w", path, err)
	}
	return data, nil
}

// --- helpers -----------------------------------------------------------------

// mapResources converts cocola Resources to OpenSandbox resourceLimits.
// CPU cores -> milli-cpu string (e.g. 0.5 -> "500m"); memory MiB -> "<n>Mi".
// shellJoin renders an argv as a single POSIX-shell command string by
// single-quoting each element, so a shell re-parsing the string reproduces
// the original argv exactly. Used to feed execd's string-based /command API
// without double-shelling.
func shellJoin(argv []string) string {
	parts := make([]string, len(argv))
	for i, a := range argv {
		parts[i] = shellQuote(a)
	}
	return strings.Join(parts, " ")
}

// chownEntrypoint builds the sandbox entry process. It is a no-op idle-blocker
// (sleep infinity), optionally prefixed by a one-time chown of the mounted,
// root-owned session mounts to execUser so the non-root Exec user can write
// them (see the Create call site for the full rationale). When execUser is
// empty, Exec runs as root, so no chown is needed and we return a bare blocker.
func chownEntrypoint(execUser string) []string {
	if execUser == "" {
		return []string{"sleep", "infinity"}
	}
	owner := shellQuote(execUser) + ":" + shellQuote(execUser)
	paths := shellJoin([]string{
		guestWorkspace,
		guestClaudeConfig,
	})
	script := "mkdir -p " + shellJoin([]string{guestWorkspace, guestClaudeConfig}) +
		" && chown -R " + owner + " " + paths +
		" || true; exec sleep infinity"
	return []string{"/bin/sh", "-c", script}
}

// shellQuote wraps s in single quotes, escaping any embedded single quotes
// via the standard '\” idiom. The result is safe to paste into a POSIX shell
// as one word. An empty string becomes ” so it is not dropped.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

// envTruthy reports whether an env var is set to a truthy value (1/true/yes/on,
// case-insensitive). Empty/unset is false.
func envTruthy(key string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// execUserFromEnv resolves the Exec privilege-drop user from the environment.
// Unset -> the default brain user (sandboxExecUser). Set-but-empty (or the
// literal "root") -> "" so Exec runs as execd's default user; any other value
// is used verbatim. This lets operators disable the drop for non-cocola images
// without recompiling, while keeping the secure default for the brain image.
func execUserFromEnv() string {
	v, ok := os.LookupEnv("COCOLA_OPENSANDBOX_EXEC_USER")
	if !ok {
		return sandboxExecUser
	}
	v = strings.TrimSpace(v)
	if v == "" || strings.EqualFold(v, "root") {
		return ""
	}
	return v
}

func httpTimeoutFromEnv() time.Duration {
	raw := strings.TrimSpace(os.Getenv("COCOLA_OPENSANDBOX_HTTP_TIMEOUT"))
	if raw == "" {
		return defaultHTTPTimeout
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return defaultHTTPTimeout
	}
	return d
}

func volumeBackendFromEnv() string {
	raw := strings.TrimSpace(strings.ToLower(os.Getenv("COCOLA_SANDBOX_VOLUME_BACKEND")))
	if raw == "" {
		return volumeBackendPVC
	}
	return raw
}

// mapVolumes translates cocola's filesystem model into the OpenSandbox volumes
// list. It mirrors the docker provider's mount points so both backends present
// an identical filesystem to the same brain image. PVC mode keeps the historical
// request shape:
//
//   - workspace:       pvc cocola-session-<sid>, subPath workspace -> /workspace
//   - Claude state:    pvc cocola-session-<sid>, subPath claude    -> /home/cocola/.claude
//   - platform skills: pvc cocola-plugins (shared)                 -> /data/plugins (RO)
//
// Claude Code state is mounted outside /workspace so file listings remain user
// focused, while still being session-scoped and checkpoint-friendly. No per-user
// writable volume is declared, so sessions cannot accidentally share Claude
// config or files.
//
// Host mode maps the same guest paths to directories under:
//
//	<root>/users/<user>/sessions/<session>/{workspace,claude}
//
// where <root> may be an NFS/NAS mount. CreateIfNotExists lets the server
// provision PVC volumes lazily; host mode creates directories locally before
// sending the create request. Neither backend deletes storage on sandbox
// termination; explicit conversation deletion calls CleanupSessionStorage.
func (p *Provider) mapVolumes(userID, sessionID string) ([]volumeSpec, error) {
	if p.volumeBackend == volumeBackendHost {
		return p.mapHostVolumes(userID, sessionID)
	}
	return mapPVCVolumes(sessionID), nil
}

func mapPVCVolumes(sessionID string) []volumeSpec {
	sid := safe(sessionID)
	sessionClaim := "cocola-session-" + sid
	return []volumeSpec{
		{
			Name:      "session",
			PVC:       &pvcBackend{ClaimName: sessionClaim, CreateIfNotExists: true},
			MountPath: guestWorkspace,
			SubPath:   workspaceSubPath,
		},
		{
			Name:      "claude",
			PVC:       &pvcBackend{ClaimName: sessionClaim, CreateIfNotExists: true},
			MountPath: guestClaudeConfig,
			SubPath:   claudeSubPath,
		},
		{
			Name:      "plugins",
			PVC:       &pvcBackend{ClaimName: pluginsClaimName},
			MountPath: guestPlugins,
			ReadOnly:  true,
		},
	}
}

func (p *Provider) mapHostVolumes(userID, sessionID string) ([]volumeSpec, error) {
	sessionRoot := p.sessionRoot(userID, sessionID)
	workspaceDir := filepath.Join(sessionRoot, workspaceSubPath)
	claudeDir := filepath.Join(sessionRoot, claudeSubPath)
	pluginDir := filepath.Join(p.root, "plugins")
	for _, d := range []string{workspaceDir, claudeDir, pluginDir} {
		if !isSubpath(p.root, d) {
			return nil, fmt.Errorf("opensandbox: volume path outside root: %s", d)
		}
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, fmt.Errorf("opensandbox: mkdir %s: %w", d, err)
		}
	}
	for _, d := range []string{workspaceDir, claudeDir} {
		if err := os.Chown(d, 10001, 10001); err != nil {
			// Best effort: a pre-created NFS directory may already have the right
			// ownership or may reject chown due to export options.
			continue
		}
	}
	return []volumeSpec{
		{
			Name:      "workspace",
			Host:      &hostBackend{Path: workspaceDir},
			MountPath: guestWorkspace,
		},
		{
			Name:      "claude",
			Host:      &hostBackend{Path: claudeDir},
			MountPath: guestClaudeConfig,
		},
		{
			Name:      "plugins",
			Host:      &hostBackend{Path: pluginDir},
			MountPath: guestPlugins,
			ReadOnly:  true,
		},
	}, nil
}

func (p *Provider) sessionRoot(userID, sessionID string) string {
	return filepath.Join(
		p.root,
		"users",
		safePathSegment(userID),
		"sessions",
		safePathSegment(sessionID),
	)
}

// safe sanitises a cocola identifier into an OpenSandbox-legal volume/path
// segment. OpenSandbox claim names must match ^[a-z0-9]([-a-z0-9]*[a-z0-9])?$
// (<=253). We lower-case, replace every illegal char with '-', collapse runs,
// trim leading/trailing '-', and fall back to "x" for an empty result so a
// claim name like "cocola-session-<sanitised>" is always valid. Mirrors the
// intent of the docker provider's safe() but targets DNS-label rules, not
// filesystem.
func safe(s string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(s) {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if ok {
			b.WriteRune(r)
			prevDash = false
			continue
		}
		if !prevDash {
			b.WriteByte('-')
			prevDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "x"
	}
	return out
}

// safePathSegment sanitises an identifier for host filesystem paths and appends
// a short hash so different raw ids cannot collide after sanitisation.
func safePathSegment(s string) string {
	h := sha256.Sum256([]byte(s))
	hash := hex.EncodeToString(h[:])[:12]
	base := safe(s)
	if len(base) > 80 {
		base = strings.Trim(base[:80], "-")
		if base == "" {
			base = "x"
		}
	}
	return base + "-" + hash
}

func isSubpath(root, path string) bool {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(absRoot, absPath)
	if err != nil {
		return false
	}
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

func mapResources(r provider.Resources) map[string]string {
	out := map[string]string{}
	if r.CPUCores > 0 {
		out["cpu"] = fmt.Sprintf("%dm", int64(r.CPUCores*1000))
	} else {
		out["cpu"] = envOr("COCOLA_OPENSANDBOX_DEFAULT_CPU", defaultCPU)
	}
	if r.MemoryMiB > 0 {
		out["memory"] = fmt.Sprintf("%dMi", r.MemoryMiB)
	} else {
		out["memory"] = envOr("COCOLA_OPENSANDBOX_DEFAULT_MEMORY", defaultMemory)
	}
	return out
}

// envOr returns the env var value if set & non-empty, else def.
func envOr(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

// resolveExecd asks the lifecycle endpoints API for the reachable execd URL of
// a sandbox and the headers to replay on execd calls. If the response omits an
// auth header, the provider API key is added as a fallback, matching the SDK's
// server-proxy behaviour.
func (p *Provider) resolveExecd(ctx context.Context, osbID string) (string, map[string]string, error) {
	var ep endpointInfo
	path := fmt.Sprintf("/sandboxes/%s/endpoints/%d", osbID, execdPort)
	if p.useServerProxy {
		// Ask for a server-proxied URL ({server}/sandboxes/{id}/proxy/{port}),
		// reachable by any client that can reach the server -- unlike the direct
		// endpoint, which resolves to an in-network host (e.g. host.docker.internal).
		path += "?use_server_proxy=true"
	}
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
		// execd line-buffers the child's stdout and emits one event per line
		// with the trailing newline stripped. The downstream consumer
		// (agent-runtime shim_provider) reassembles NDJSON by splitting on
		// "\n", exactly as it does for the docker provider whose raw
		// `docker exec` stream preserves newlines. Restore the newline so both
		// backends present an identical newline-delimited byte stream;
		// otherwise the shim's JSON objects concatenate with no delimiter and
		// none are ever parsed. We normalise to exactly one trailing newline
		// in case a future execd build stops stripping it.
		text := strings.TrimRight(ev.Text, "\n") + "\n"
		out <- provider.ExecEvent{Kind: provider.ExecEventStdout, Stdout: []byte(text)}
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
		if resp.StatusCode == http.StatusNotFound {
			return fmt.Errorf("opensandbox: %s %s: status %d: %s: %w", method, path, resp.StatusCode, strings.TrimSpace(string(b)), fs.ErrNotExist)
		}
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
