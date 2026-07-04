// Package orchestrator owns the session<->sandbox binding lifecycle: it maps a
// logical session to exactly one live sandbox, guarantees that concurrent
// requests for the same session converge on a single sandbox (distributed
// lock), and reclaims idle sandboxes via a lease/heartbeat + two-stage
// (Pause-then-Destroy) garbage collector.
//
// All binding state lives in Redis (behind the go-common KV interface) so that
// sandbox-manager can run as multiple stateless replicas behind a load
// balancer — any replica can serve any session.
package orchestrator

import "time"

// Key namespace. Everything the orchestrator writes is prefixed so a shared
// Redis can host other tenants/components without collision.
const keyPrefix = "cocola:sb:"

// Default lifecycle timings. The user picked these:
//   - leaseTTL:       a sandbox whose lease is not renewed within this window is
//     considered idle and becomes eligible for stage-1 reclaim.
//   - heartbeatEvery: how often the heartbeat worker renews live leases. Must be
//     comfortably less than leaseTTL so missed ticks do not expire a healthy
//     lease.
//   - destroyGrace:   after a sandbox is Paused (stage 1), how long it lingers
//     before it is Destroyed (stage 2). A re-acquire during this
//     window resurrects it (Resume) instead of paying a cold
//     create.
//   - lockTTL:        safety cap on the per-session create lock so a crashed
//     holder cannot wedge a session forever.
const (
	DefaultLeaseTTL       = 10 * time.Minute
	DefaultHeartbeatEvery = 20 * time.Second
	DefaultDestroyGrace   = 120 * time.Second
	DefaultLockTTL        = 30 * time.Second
	DefaultReaperEvery    = 10 * time.Second
)

// convKey maps a session id to its bound sandbox id (forward lookup).
//
//	cocola:sb:conv:{session} -> sandbox_id
func convKey(sessionID string) string { return keyPrefix + "conv:" + sessionID }

// revKey maps a sandbox id back to its session id (reverse lookup, used on
// reclaim to clean the forward entry without a scan).
//
//	cocola:sb:rev:{sandbox} -> session_id
func revKey(sandboxID string) string { return keyPrefix + "rev:" + sandboxID }

// lockKey guards the create-and-bind critical section for a session.
//
//	cocola:sb:lock:{session} -> lock token
func lockKey(sessionID string) string { return keyPrefix + "lock:" + sessionID }

// leaseKey is the heartbeat-renewed liveness marker for a sandbox. It carries a
// TTL; when it disappears the reaper treats the sandbox as idle.
//
//	cocola:sb:lease:{sandbox} -> "1"  (EX leaseTTL)
func leaseKey(sandboxID string) string { return keyPrefix + "lease:" + sandboxID }

// metaPrefix is the scan root for the durable per-sandbox registry record. Meta
// keys deliberately carry NO TTL: they are the reaper's source of truth for
// what exists. The lease (which does expire) is what signals idleness.
//
//	cocola:sb:meta:{sandbox} -> JSON(meta)
const metaPrefix = keyPrefix + "meta:"

func metaKey(sandboxID string) string { return metaPrefix + sandboxID }

func metaScanPattern() string { return metaPrefix + "*" }

// State is the lifecycle phase of a bound sandbox, persisted in meta.
type State string

const (
	// StateActive: sandbox is running and serving; lease is being renewed.
	StateActive State = "active"
	// StatePaused: stage-1 reclaim done (provider.Pause called); awaiting
	// either resurrection (Resume on re-acquire) or stage-2 Destroy.
	StatePaused State = "paused"
)

// meta is the durable registry record for one bound sandbox.
type meta struct {
	SandboxID   string `json:"sandbox_id"`
	SessionID   string `json:"session_id"`
	UserID      string `json:"user_id"`
	Image       string `json:"image"`
	State       State  `json:"state"`
	CreatedUnix int64  `json:"created_unix"`
	PausedUnix  int64  `json:"paused_unix"` // 0 unless StatePaused
}
