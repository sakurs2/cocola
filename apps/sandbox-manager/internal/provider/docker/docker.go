// Package docker implements SandboxProvider on top of the Docker Engine API.
//
// This is the M1 reference backend: it is the simplest provider that exercises
// the full lifecycle (create / exec / file IO / pause / resume / destroy /
// health) so the rest of the platform can be built and demoed on a single
// machine. The K8s+gVisor provider (later milestone) implements the SAME
// interface, so swapping backends never touches the service or agent layers.
//
// Three-tier directory model (mirrors Mira's convention):
//
//	/data/userdata/<user_id>/   -> host: <root>/userdata/<user_id>   (cross-session, RW)
//	/workspace/<session_id>/    -> host: <root>/workspace/<session>  (session-scoped, RW)
//	/data/plugins/              -> host: <root>/plugins              (platform skills, RO)
package docker

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/google/uuid"

	"github.com/cocola-project/cocola/apps/sandbox-manager/internal/provider"
)

// ProviderName is the registry key used in config to select this backend.
const ProviderName = "docker"

const (
	labelManaged   = "cocola.managed"
	labelSandboxID = "cocola.sandbox_id"
	labelUserID    = "cocola.user_id"
	labelSessionID = "cocola.session_id"
	// labelExecUser records the user every Exec must run as. Empty = the image
	// default (legacy alpine/M1, runs as its own default user). Set to the
	// non-root sandbox user when the egress firewall is active (the container's
	// main process is root, so exec must be pinned back). Stored as a label so
	// the pin survives a record rebuilt from Docker on another replica.
	labelExecUser = "cocola.exec_user"

	defaultImage       = "alpine:3.20"
	defaultExecTimeout = 60 * time.Second

	guestUserData  = "/data/userdata"
	guestWorkspace = "/workspace"
	guestPlugins   = "/data/plugins"
	// guestClaudeConfig is CLAUDE_CONFIG_DIR inside the brain image
	// (deploy/sandbox-runtime/Dockerfile): ~/.claude holds Claude Code's
	// on-disk session files (projects/<proj>/<uuid>.jsonl). Binding it onto a
	// per-user host dir is the SUFFICIENT condition for --resume to survive a
	// sandbox recreation (ADR-0008 T2, cross-session).
	guestClaudeConfig = "/home/cocola/.claude"
	// sandboxUID is the non-root uid the brain image runs as (Dockerfile:
	// useradd -u 10001 cocola). A fresh bind-mount is root-owned, so we chown it
	// to this uid or the in-sandbox claude CLI cannot write its session files.
	sandboxUID = 10001
	// sandboxUser is the non-root user every Exec runs as. The container's main
	// process runs as root (so the entrypoint can install the egress firewall,
	// which needs NET_ADMIN), so we MUST pin exec back to this user -- otherwise
	// user/agent code would inherit root. Matches the Dockerfile's cocola user.
	sandboxUser = "cocola"
	// firewallEntrypoint is the in-image script that installs the egress firewall
	// as root then keep-alives the container. Used only when an egress policy is
	// configured (see Create). Defined in deploy/sandbox-runtime/Dockerfile.
	firewallEntrypoint = "/opt/cocola/firewall-entrypoint.sh"
)

// Provider is a Docker-backed SandboxProvider.
type Provider struct {
	cli  *client.Client
	root string // host directory holding all sandbox volumes

	mu        sync.RWMutex
	sandboxes map[string]*record // sandbox_id -> container record
}

type record struct {
	containerID string
	userID      string
	sessionID   string
	execUser    string // user to pin Exec to; empty = image default
}

// Option configures the Provider.
type Option func(*Provider)

// WithRoot overrides the host volume root (default: $HOME/.cocola/sandboxes).
func WithRoot(root string) Option {
	return func(p *Provider) { p.root = root }
}

// New constructs a Docker provider and pings the daemon.
func New(opts ...Option) (*Provider, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("docker: new client: %w", err)
	}
	home, _ := os.UserHomeDir()
	p := &Provider{
		cli:       cli,
		root:      filepath.Join(home, ".cocola", "sandboxes"),
		sandboxes: map[string]*record{},
	}
	// COCOLA_SANDBOX_ROOT pins the host volume root to an explicit absolute path.
	// This is essential under DooD (sandbox-manager itself in a container talking
	// to the host Docker daemon): bind-mount Source paths are resolved by the host
	// daemon, so the root must be a path that is identical inside the container and
	// on the host (path isomorphism). It is also the seam for pointing the root at
	// an NFS/NAS mount in production. Empty -> fall back to $HOME/.cocola/sandboxes.
	if r := os.Getenv("COCOLA_SANDBOX_ROOT"); r != "" {
		p.root = r
	}
	for _, o := range opts {
		o(p)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := cli.Ping(ctx); err != nil {
		return nil, fmt.Errorf("docker: ping daemon: %w", err)
	}
	return p, nil
}

// applyEgressPolicy translates the sandbox egress policy into Docker HostConfig
// mutations and the container command, returning the command to run.
//
// Semantics (ADR-0009; see docs/plan/hardening-sandbox-egress-allowlist.md):
//   - nil allowlist -> "no policy configured": legacy wide-open keep-alive
//     (the alpine default image / M1 demos). HostConfig untouched.
//   - non-nil allowlist (the production path; the orchestrator always folds in
//     the llm-gateway host) -> firewall entrypoint: grant NET_ADMIN, map
//     host.docker.internal so the in-container firewall can resolve+allow the
//     gateway, and pass the allowlist down via COCOLA_EGRESS_ALLOWLIST. An empty
//     (non-nil) allowlist installs the DNS-only baseline -- secure by default,
//     never wide-open, never NetworkMode=none (which would cut the gateway off).
//
// Returns the container command plus the user every Exec must be pinned to
// (empty = image default; the non-root sandbox user when the firewall is on,
// since the container main process then runs as root).
func applyEgressPolicy(hostCfg *container.HostConfig, env *[]string, net provider.Networking) (cmd []string, execUser string) {
	keepAlive := []string{"sh", "-c", "trap : TERM INT; sleep infinity & wait"}
	if net.EgressAllowlist == nil {
		return keepAlive, ""
	}
	hostCfg.CapAdd = append(hostCfg.CapAdd, "NET_ADMIN")
	hostCfg.ExtraHosts = append(hostCfg.ExtraHosts, "host.docker.internal:host-gateway")
	*env = append(*env, "COCOLA_EGRESS_ALLOWLIST="+strings.Join(net.EgressAllowlist, ","))
	return []string{firewallEntrypoint}, sandboxUser
}

// Create pulls the image (if absent), provisions the three-tier host dirs, and
// starts a long-lived container the agent can exec into.
func (p *Provider) Create(ctx context.Context, spec provider.SandboxSpec) (*provider.Sandbox, error) {
	img := spec.Image
	if img == "" {
		img = defaultImage
	}
	if err := p.ensureImage(ctx, img); err != nil {
		return nil, err
	}

	sid := "sbx-" + uuid.NewString()
	userDir := filepath.Join(p.root, "userdata", safe(spec.UserID))
	sessDir := filepath.Join(p.root, "workspace", safe(spec.SessionID))
	pluginDir := filepath.Join(p.root, "plugins")
	// Per-user ~/.claude (cross-session): persists Claude Code's on-disk session
	// files so a follow-up turn can --resume even after the sandbox is recreated.
	claudeDir := filepath.Join(p.root, "claude", safe(spec.UserID))
	for _, d := range []string{userDir, sessDir, pluginDir, claudeDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, fmt.Errorf("docker: mkdir %s: %w", d, err)
		}
	}
	// The brain runs as the non-root cocola user; a fresh bind-mount is
	// root-owned, so chown ~/.claude or the claude CLI cannot persist sessions.
	// Best-effort: a pre-existing dir owned by the right uid is the common case.
	if err := os.Chown(claudeDir, sandboxUID, sandboxUID); err != nil {
		slog.Warn("docker: chown claude dir", "dir", claudeDir, "err", err)
	}

	env := make([]string, 0, len(spec.Env))
	for k, v := range spec.Env {
		env = append(env, k+"="+v)
	}

	hostCfg := &container.HostConfig{
		Mounts: []mount.Mount{
			{Type: mount.TypeBind, Source: userDir, Target: guestUserData + "/" + safe(spec.UserID)},
			{Type: mount.TypeBind, Source: sessDir, Target: guestWorkspace + "/" + safe(spec.SessionID)},
			{Type: mount.TypeBind, Source: pluginDir, Target: guestPlugins, ReadOnly: true},
			{Type: mount.TypeBind, Source: claudeDir, Target: guestClaudeConfig},
		},
		Resources: container.Resources{
			NanoCPUs: int64(spec.Resources.CPUCores * 1e9),
			Memory:   spec.Resources.MemoryMiB * 1024 * 1024,
		},
	}

	// Egress enforcement (ADR-0009): mutate hostCfg + env + pick the container
	// command based on the configured policy. Extracted into a pure helper so the
	// decision is unit-testable without a live daemon.
	cmd, execUser := applyEgressPolicy(hostCfg, &env, spec.Networking)

	cfg := &container.Config{
		Image:      img,
		Env:        env,
		Cmd:        cmd,
		WorkingDir: guestWorkspace + "/" + safe(spec.SessionID),
		Labels: map[string]string{
			labelManaged:   "true",
			labelSandboxID: sid,
			labelUserID:    spec.UserID,
			labelSessionID: spec.SessionID,
			labelExecUser:  execUser,
		},
	}

	created, err := p.cli.ContainerCreate(ctx, cfg, hostCfg, nil, nil, "cocola_"+sid)
	if err != nil {
		return nil, fmt.Errorf("docker: container create: %w", err)
	}
	if err := p.cli.ContainerStart(ctx, created.ID, container.StartOptions{}); err != nil {
		_ = p.cli.ContainerRemove(ctx, created.ID, container.RemoveOptions{Force: true})
		return nil, fmt.Errorf("docker: container start: %w", err)
	}

	p.mu.Lock()
	p.sandboxes[sid] = &record{containerID: created.ID, userID: spec.UserID, sessionID: spec.SessionID, execUser: execUser}
	p.mu.Unlock()

	return &provider.Sandbox{
		ID:        sid,
		UserID:    spec.UserID,
		SessionID: spec.SessionID,
		Endpoint:  "docker://" + created.ID,
	}, nil
}

// containerThawer is the narrow slice of the Docker client that thawIfPaused
// needs. Defining it here (rather than depending on *client.Client) keeps the
// self-heal logic unit-testable with a tiny fake and no live daemon.
type containerThawer interface {
	ContainerInspect(ctx context.Context, containerID string) (types.ContainerJSON, error)
	ContainerUnpause(ctx context.Context, containerID string) error
}

// thawIfPaused unfreezes a sandbox container that the reaper Paused during
// stage-1 idle reclaim. Exec against a frozen (cgroup-freezer) container blocks
// the new process until the caller's deadline, surfacing as a misleading
// "context deadline exceeded"; reusing a paused sandbox in a later turn is
// legitimate, so we thaw first. A non-paused (or vanished) container is a no-op
// here -- an already-destroyed sandbox is reported by the downstream exec call.
func thawIfPaused(ctx context.Context, cli containerThawer, containerID, sid string) error {
	insp, err := cli.ContainerInspect(ctx, containerID)
	if err != nil {
		// Inspect failures are not fatal to exec: let the exec path produce the
		// authoritative error (e.g. no-such-container) instead of masking it.
		return nil
	}
	if insp.State == nil || !insp.State.Paused {
		return nil
	}
	if err := cli.ContainerUnpause(ctx, containerID); err != nil {
		return fmt.Errorf("docker: unpause before exec: %w", err)
	}
	slog.Info("docker: thawed paused sandbox before exec", "sandbox_id", sid)
	return nil
}

// Exec runs a command inside the sandbox and streams stdout/stderr, terminating
// with an Exit (or Error) event.
func (p *Provider) Exec(ctx context.Context, sid string, req provider.ExecRequest) (<-chan provider.ExecEvent, error) {
	rec, err := p.resolve(ctx, sid)
	if err != nil {
		return nil, err
	}
	if len(req.Cmd) == 0 {
		return nil, fmt.Errorf("docker: empty command")
	}

	// Self-heal: the reaper Pauses idle sandboxes (stage-1 reclaim), but exec
	// against a frozen container hangs until the caller's deadline because the
	// cgroup freezer blocks the new process. A later turn legitimately reuses
	// the same sandbox, so thaw it here before exec instead of dying with a
	// context-deadline-exceeded.
	if err := thawIfPaused(ctx, p.cli, rec.containerID, sid); err != nil {
		return nil, err
	}

	env := make([]string, 0, len(req.Env))
	for k, v := range req.Env {
		env = append(env, k+"="+v)
	}
	execCfg := container.ExecOptions{
		// Pin to the sandbox's exec user. When the egress firewall is active the
		// container main process runs as root, so this is the non-root cocola
		// user (else exec'd code would inherit root). Empty for legacy images
		// (alpine/M1) -> Docker uses the image's own default user.
		User:         rec.execUser,
		Cmd:          req.Cmd,
		Env:          env,
		WorkingDir:   req.Cwd,
		AttachStdout: true,
		AttachStderr: true,
		AttachStdin:  len(req.Stdin) > 0,
	}

	idResp, err := p.cli.ContainerExecCreate(ctx, rec.containerID, execCfg)
	if err != nil {
		return nil, fmt.Errorf("docker: exec create: %w", err)
	}
	att, err := p.cli.ContainerExecAttach(ctx, idResp.ID, container.ExecAttachOptions{})
	if err != nil {
		return nil, fmt.Errorf("docker: exec attach: %w", err)
	}

	timeout := defaultExecTimeout
	if req.Timeout > 0 {
		timeout = time.Duration(req.Timeout) * time.Second
	}

	out := make(chan provider.ExecEvent, 32)
	go func() {
		defer close(out)
		defer att.Close()

		runCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		if len(req.Stdin) > 0 {
			_, _ = att.Conn.Write(req.Stdin)
			_ = att.CloseWrite()
		}

		// Demux the multiplexed docker stream into stdout/stderr events.
		stdoutW := &chanWriter{kind: provider.ExecEventStdout, out: out}
		stderrW := &chanWriter{kind: provider.ExecEventStderr, out: out}
		copyDone := make(chan error, 1)
		go func() {
			_, e := stdcopy.StdCopy(stdoutW, stderrW, att.Reader)
			copyDone <- e
		}()

		select {
		case <-runCtx.Done():
			out <- provider.ExecEvent{Kind: provider.ExecEventError, Err: runCtx.Err()}
			return
		case e := <-copyDone:
			if e != nil && e != io.EOF {
				out <- provider.ExecEvent{Kind: provider.ExecEventError, Err: e}
				return
			}
		}

		insp, e := p.cli.ContainerExecInspect(runCtx, idResp.ID)
		if e != nil {
			out <- provider.ExecEvent{Kind: provider.ExecEventError, Err: e}
			return
		}
		out <- provider.ExecEvent{Kind: provider.ExecEventExit, Exit: int32(insp.ExitCode)}
	}()
	return out, nil
}

// WriteFile copies a single file into the sandbox via a tar stream.
func (p *Provider) WriteFile(ctx context.Context, sid, path string, data []byte) error {
	rec, err := p.resolve(ctx, sid)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	base := filepath.Base(path)

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	hdr := &tar.Header{Name: base, Mode: 0o644, Size: int64(len(data))}
	if err := tw.WriteHeader(hdr); err != nil {
		return fmt.Errorf("docker: tar header: %w", err)
	}
	if _, err := tw.Write(data); err != nil {
		return fmt.Errorf("docker: tar write: %w", err)
	}
	if err := tw.Close(); err != nil {
		return fmt.Errorf("docker: tar close: %w", err)
	}
	if err := p.cli.CopyToContainer(ctx, rec.containerID, dir, &buf, container.CopyToContainerOptions{}); err != nil {
		return fmt.Errorf("docker: copy to container: %w", err)
	}
	return nil
}

// ReadFile copies a single file out of the sandbox via a tar stream.
func (p *Provider) ReadFile(ctx context.Context, sid, path string) ([]byte, error) {
	rec, err := p.resolve(ctx, sid)
	if err != nil {
		return nil, err
	}
	rc, _, err := p.cli.CopyFromContainer(ctx, rec.containerID, path)
	if err != nil {
		return nil, fmt.Errorf("docker: copy from container: %w", err)
	}
	defer rc.Close()

	tr := tar.NewReader(rc)
	if _, err := tr.Next(); err != nil {
		return nil, fmt.Errorf("docker: tar next: %w", err)
	}
	data, err := io.ReadAll(tr)
	if err != nil {
		return nil, fmt.Errorf("docker: tar read: %w", err)
	}
	return data, nil
}

// Pause freezes all processes in the sandbox (cgroup freezer).
func (p *Provider) Pause(ctx context.Context, sid string) error {
	rec, err := p.resolve(ctx, sid)
	if err != nil {
		return err
	}
	return p.cli.ContainerPause(ctx, rec.containerID)
}

// Resume thaws a paused sandbox.
func (p *Provider) Resume(ctx context.Context, sid string) error {
	rec, err := p.resolve(ctx, sid)
	if err != nil {
		return err
	}
	return p.cli.ContainerUnpause(ctx, rec.containerID)
}

// Destroy force-removes the sandbox container. Host volumes are intentionally
// retained (cross-session userdata persistence is the whole point).
func (p *Provider) Destroy(ctx context.Context, sid string) error {
	rec, err := p.resolve(ctx, sid)
	if err != nil {
		return err
	}
	if err := p.cli.ContainerRemove(ctx, rec.containerID, container.RemoveOptions{Force: true}); err != nil {
		return fmt.Errorf("docker: container remove: %w", err)
	}
	p.mu.Lock()
	delete(p.sandboxes, sid)
	p.mu.Unlock()
	return nil
}

// Health inspects the underlying container.
func (p *Provider) Health(ctx context.Context, sid string) (*provider.HealthStatus, error) {
	rec, err := p.resolve(ctx, sid)
	if err != nil {
		return nil, err
	}
	insp, err := p.cli.ContainerInspect(ctx, rec.containerID)
	if err != nil {
		return nil, fmt.Errorf("docker: inspect: %w", err)
	}
	healthy := insp.State != nil && insp.State.Running && !insp.State.Paused
	detail := "unknown"
	if insp.State != nil {
		detail = insp.State.Status
	}
	return &provider.HealthStatus{Healthy: healthy, Detail: detail}, nil
}

// --- helpers ---------------------------------------------------------------

// resolve maps a sandbox id to its container record. It checks the in-process
// cache first (fast path for sandboxes this replica created), then falls back to
// a Docker label query (cocola.sandbox_id). The fallback is what makes
// sandbox-manager horizontally scalable and restart-safe: any replica can
// Pause/Resume/Destroy/Health a sandbox created by any other replica, because
// the container itself carries the binding labels.
func (p *Provider) resolve(ctx context.Context, sid string) (*record, error) {
	p.mu.RLock()
	rec, ok := p.sandboxes[sid]
	p.mu.RUnlock()
	if ok {
		return rec, nil
	}

	cs, err := p.cli.ContainerList(ctx, container.ListOptions{
		All: true,
		Filters: filters.NewArgs(
			filters.Arg("label", labelManaged+"=true"),
			filters.Arg("label", labelSandboxID+"="+sid),
		),
	})
	if err != nil {
		return nil, fmt.Errorf("docker: container list: %w", err)
	}
	if len(cs) == 0 {
		return nil, fmt.Errorf("docker: sandbox not found: %s", sid)
	}
	c := cs[0]
	rec = &record{
		containerID: c.ID,
		userID:      c.Labels[labelUserID],
		sessionID:   c.Labels[labelSessionID],
		execUser:    c.Labels[labelExecUser],
	}
	// Re-populate the cache so subsequent ops on this replica hit the fast path.
	p.mu.Lock()
	p.sandboxes[sid] = rec
	p.mu.Unlock()
	return rec, nil
}

func (p *Provider) ensureImage(ctx context.Context, ref string) error {
	imgs, err := p.cli.ImageList(ctx, image.ListOptions{
		Filters: filters.NewArgs(filters.Arg("reference", ref)),
	})
	if err == nil && len(imgs) > 0 {
		return nil
	}
	rc, err := p.cli.ImagePull(ctx, ref, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("docker: image pull %s: %w", ref, err)
	}
	defer rc.Close()
	_, _ = io.Copy(io.Discard, rc) // drain to completion
	return nil
}

// safe sanitises an identifier for use as a filesystem path segment.
func safe(s string) string {
	if s == "" {
		return "_"
	}
	r := strings.NewReplacer("/", "_", "..", "_", " ", "_")
	return r.Replace(s)
}

// chanWriter adapts an io.Writer onto the ExecEvent channel.
type chanWriter struct {
	kind provider.ExecEventKind
	out  chan<- provider.ExecEvent
}

func (w *chanWriter) Write(p []byte) (int, error) {
	cp := make([]byte, len(p))
	copy(cp, p)
	ev := provider.ExecEvent{Kind: w.kind}
	if w.kind == provider.ExecEventStdout {
		ev.Stdout = cp
	} else {
		ev.Stderr = cp
	}
	w.out <- ev
	return len(p), nil
}
