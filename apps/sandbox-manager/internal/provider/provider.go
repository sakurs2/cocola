// Package provider declares the SandboxProvider abstraction.
//
// Every concrete implementation (Docker, K8s+gVisor, E2B, CubeSandbox) must
// satisfy this interface. The orchestrator code MUST NOT import any concrete
// provider directly; instead, providers register themselves via Register().
package provider

import (
	"context"
	"sync"
)

// SandboxSpec describes the desired sandbox.
type SandboxSpec struct {
	UserID    string            // owner — used to mount per-user persistent volume
	SessionID string            // session — used to scope ephemeral workspace
	Image     string            // OCI image reference
	Env       map[string]string // extra env to inject
	Resources Resources         // CPU/mem/disk caps
	Networking Networking       // egress policy
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
	Healthy bool
	Detail  string
}

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

// Registry is the global provider registry. Providers self-register in their
// package init() so the orchestrator can pick one by name from config.
var (
	registryMu sync.RWMutex
	registry   = map[string]SandboxProvider{}
)

// Register a provider under the given name. Panics on duplicate keys.
func Register(name string, p SandboxProvider) {
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, exists := registry[name]; exists {
		panic("sandbox provider already registered: " + name)
	}
	registry[name] = p
}

// Get looks up a registered provider. Returns nil if absent.
func Get(name string) SandboxProvider {
	registryMu.RLock()
	defer registryMu.RUnlock()
	return registry[name]
}
