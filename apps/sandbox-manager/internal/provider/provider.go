// Package provider declares the SandboxProvider abstraction.
//
// Every concrete implementation must satisfy this interface. Production uses
// OpenSandbox directly; the interface remains the orchestrator's test seam.
package provider

import (
	"context"
	"errors"
)

// SandboxSpec describes the desired sandbox.
type SandboxSpec struct {
	UserID         string            // owner — used to mount per-user persistent volume
	SessionID      string            // session — used to scope ephemeral workspace
	Image          string            // OCI image reference
	Env            map[string]string // extra env to inject
	Resources      Resources         // CPU/mem/disk caps
	Networking     Networking        // egress policy
	TargetNodeName string            // optional node placement for schedulable backends
	// Warm marks a session-agnostic pre-warmed sandbox created ahead of demand
	// (see orchestrator.WarmPool). A warm sandbox mounts NO per-session volume
	// (OpenSandbox has no hot-mount API, ADR-0016), so its workspace is ephemeral
	// until a session claims it and its state is restored via checkpoint/restore.
	Warm bool
}

// Resources defines the resource quota.
type Resources struct {
	CPUCores  float64
	MemoryMiB int64
	DiskMiB   int64
}

// Networking defines egress and ingress rules.
type Networking struct {
	EgressAllowlist []string // domain whitelist; empty = no egress
}

// Sandbox identifies a running sandbox.
type Sandbox struct {
	ID        string
	UserID    string
	SessionID string
	Endpoint  string // provider-specific (e.g. unix socket, gRPC addr)
}

// ExecRequest describes a command to execute inside the sandbox.
type ExecRequest struct {
	Cmd     []string
	Cwd     string
	Env     map[string]string
	Stdin   []byte
	Timeout int // seconds; 0 = provider default
}

// ExecEvent is streamed back to the caller during command execution.
type ExecEvent struct {
	Kind   ExecEventKind
	Stdout []byte
	Stderr []byte
	Exit   int32
	Err    error
}

// ExecEventKind enumerates the streamed event types.
type ExecEventKind int

const (
	ExecEventStdout ExecEventKind = iota
	ExecEventStderr
	ExecEventExit
	ExecEventError
)

// HealthStatus is returned by Health().
type HealthStatus struct {
	Healthy      bool
	Transitional bool // may become healthy without recreating the sandbox
	Detail       string
}

// ErrSandboxNotResumable indicates a Resume was rejected because the sandbox is
// no longer in a resumable (Paused) state: it has reached a terminal phase
// (completed / failed / terminated) and its paused checkpoint is gone. The
// orchestrator treats this exactly like a missing sandbox — it drops the stale
// binding and cold-creates a fresh one (session state is restored from the
// durable checkpoint by agent-runtime).
var ErrSandboxNotResumable = errors.New("provider: sandbox not resumable")

// SandboxProvider is the contract every backend must implement.
type SandboxProvider interface {
	Create(ctx context.Context, spec SandboxSpec) (*Sandbox, error)
	Exec(ctx context.Context, sid string, req ExecRequest) (<-chan ExecEvent, error)
	WriteFile(ctx context.Context, sid, path string, data []byte) error
	ReadFile(ctx context.Context, sid, path string) ([]byte, error)
	Pause(ctx context.Context, sid string) error
	Resume(ctx context.Context, sid string) error
	Destroy(ctx context.Context, sid string) error
	Health(ctx context.Context, sid string) (*HealthStatus, error)
}

// SessionStorageCleaner is an optional extension implemented by providers that
// own host-visible session storage. The orchestrator calls it only for explicit
// conversation deletion; idle reaping still preserves session directories.
type SessionStorageCleaner interface {
	CleanupSessionStorage(ctx context.Context, userID, sessionID string) error
}

// SessionCheckpointer is an optional extension implemented by providers that
// can snapshot a live sandbox before the orchestrator reclaims it. Errors are
// intentionally best-effort at the call site so reclamation cannot be blocked
// forever by persistence failures.
type SessionCheckpointer interface {
	CheckpointSession(ctx context.Context, userID, sessionID, sandboxID string) error
}
