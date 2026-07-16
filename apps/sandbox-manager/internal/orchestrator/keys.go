// Package orchestrator owns the session<->sandbox binding lifecycle: it maps a
// logical session to exactly one live sandbox, guarantees that concurrent
// requests for the same session converge on a single sandbox (distributed
// lock), and destroys idle sandboxes after their heartbeat lease expires.
//
// All binding state lives in Redis (behind the go-common KV interface) so that
// sandbox-manager can run as multiple stateless replicas behind a load
// balancer — any replica can serve any session.
package orchestrator

import "time"

// Key namespace. Everything the orchestrator writes is prefixed so a shared
// Redis can host other tenants/components without collision.
const keyPrefix = "cocola:sb:"

// Default lifecycle timings:
//   - leaseTTL:       a sandbox whose lease is not renewed within this window is
//     considered idle and becomes eligible for reclaim.
//   - lockTTL:        safety cap on the per-session create lock so a crashed
//     holder cannot wedge a session forever.
const (
	DefaultLeaseTTL = 10 * time.Minute
	// OpenSandbox create calls may spend up to four minutes pulling an image and
	// waiting for Kubernetes. CAS binding remains the final fencing boundary, but
	// a five-minute lock avoids routinely creating a loser during a cold start.
	DefaultLockTTL     = 5 * time.Minute
	DefaultReaperEvery = 10 * time.Second
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

// warmPrefix is the scan root for the pre-warmed sandbox inventory. A warm key
// records one session-agnostic sandbox that was created ahead of demand and is
// waiting to be claimed by the next cold-start. Warm keys carry NO TTL: the
// refill loop is the source of truth for how many warm sandboxes exist, and a
// claim is an atomic DEL of the key (exactly one replica wins).
//
//	cocola:sb:warm:{sandbox} -> JSON(warmMeta)
const warmPrefix = keyPrefix + "warm:"

func warmKey(sandboxID string) string { return warmPrefix + sandboxID }

func warmScanPattern() string { return warmPrefix + "*" }

// warmConfigKey is the runtime delivery cache written by admin-api from the
// durable system setting. It only contains sizing; provisioning remains local.
// warmRefillLockKey serialises the refill loop across replicas so only one
// replica creates warm sandboxes per tick (avoids N replicas each creating a
// full pool). Short TTL so a crashed holder frees it quickly.
const warmRefillLockKey = keyPrefix + "warmrefill:lock"

// State is the lifecycle phase of a bound sandbox, persisted in meta.
type State string

const (
	// StateActive: sandbox is running and serving; lease is being renewed.
	StateActive State = "active"
)

// meta is the durable registry record for one bound sandbox.
type meta struct {
	SandboxID         string `json:"sandbox_id"`
	SessionID         string `json:"session_id"`
	UserID            string `json:"user_id"`
	Image             string `json:"image"`
	State             State  `json:"state"`
	CreatedUnix       int64  `json:"created_unix"`
	NodeName          string `json:"node_name,omitempty"`
	StorageID         string `json:"storage_id,omitempty"`
	PVCNamespace      string `json:"pvc_namespace,omitempty"`
	SessionClaim      string `json:"session_claim,omitempty"`
	StorageGeneration int64  `json:"storage_generation,omitempty"`
}

// warmMeta is the durable registry record for one pre-warmed sandbox.
type warmMeta struct {
	SandboxID   string `json:"sandbox_id"`
	Image       string `json:"image"`
	NodeName    string `json:"node_name,omitempty"`
	CreatedUnix int64  `json:"created_unix"`
}
