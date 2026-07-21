package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"time"

	"github.com/cocola-project/cocola/apps/sandbox-manager/internal/provider"
	rds "github.com/cocola-project/cocola/packages/go-common/redis"
)

// Config tunes the binder's lifecycle behaviour. Zero values fall back to the
// package Default* constants, so callers can set only what they care about.
type Config struct {
	LeaseTTL    time.Duration
	LockTTL     time.Duration
	ReaperEvery time.Duration
	LockRetry   time.Duration // spin interval while waiting on a held lock
}

func (c Config) withDefaults() Config {
	if c.LeaseTTL == 0 {
		c.LeaseTTL = DefaultLeaseTTL
	}
	if c.LockTTL == 0 {
		c.LockTTL = DefaultLockTTL
	}
	if c.ReaperEvery == 0 {
		c.ReaperEvery = DefaultReaperEvery
	}
	if c.LockRetry == 0 {
		c.LockRetry = 50 * time.Millisecond
	}
	return c
}

// ConfigFromEnv reads COCOLA_SANDBOX_* lifecycle overrides (seconds). Any unset
// or invalid var falls back to the package default via withDefaults().
func ConfigFromEnv() Config {
	return Config{
		LeaseTTL:    envSecs("COCOLA_SANDBOX_LEASE_TTL_SECS", DefaultLeaseTTL),
		LockTTL:     envSecs("COCOLA_SANDBOX_LOCK_TTL_SECS", DefaultLockTTL),
		ReaperEvery: envSecs("COCOLA_SANDBOX_REAPER_SECS", DefaultReaperEvery),
	}
}

func envSecs(k string, def time.Duration) time.Duration {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return time.Duration(n) * time.Second
		}
	}
	return def
}

// Binder maps sessions to sandboxes over a KV store, creating/reusing/resuming
// sandboxes through the provider. It is safe for concurrent use and holds no
// per-session state in memory — everything authoritative lives in the KV.
type Binder struct {
	kv  rds.KV
	p   provider.SandboxProvider
	cfg Config

	// net is the egress policy injected into every provider.Create. The zero
	// value (nil allowlist) leaves each provider on its own default; callers
	// tighten it via WithNetworking (see NetworkingFromEnv).
	net provider.Networking

	// metrics is optional; nil means "don't record".
	metrics *Metrics

	// cap is optional; nil means fresh sandbox creation is not capacity-gated.
	cap CapacityGuard

	// storage is configured only for managed k3s. It owns the durable
	// session->node/PVC binding; Redis continues to own running sandboxes only.
	storage SessionStorageManager
}

// ErrSessionOwnerMismatch prevents a caller-controlled session id from
// reusing a sandbox that belongs to another user.
var ErrSessionOwnerMismatch = errors.New("orchestrator: session owner mismatch")

// ErrSessionLockLost means a slow create outlived its Session lock and no
// concurrently-bound winner exists. The candidate Sandbox is destroyed before
// this error is returned, so an intervening Release cannot be undone.
var ErrSessionLockLost = errors.New("orchestrator: session lock lost during create")

// NewBinder constructs a Binder. The provider is the same abstraction the gRPC
// server uses, so the binder works identically against Docker today and
// K8s+gVisor later with no changes here.
func NewBinder(kv rds.KV, p provider.SandboxProvider, cfg Config) *Binder {
	return &Binder{kv: kv, p: p, cfg: cfg.withDefaults()}
}

// EffectiveConfig returns the binder's config after defaults have been applied.
func (b *Binder) EffectiveConfig() Config { return b.cfg }

// WithMetrics attaches a metrics sink. Returns the binder for chaining.
func (b *Binder) WithMetrics(m *Metrics) *Binder {
	b.metrics = m
	return b
}

// WithNetworking sets the egress policy injected into every sandbox the binder
// creates. Returns the binder for chaining.
func (b *Binder) WithNetworking(n provider.Networking) *Binder {
	b.net = n
	return b
}

// WithCapacityGuard gates fresh sandbox creation. Existing session bindings are
// still reused so a full cluster does not strand already-running conversations.
func (b *Binder) WithCapacityGuard(g CapacityGuard) *Binder {
	b.cap = g
	return b
}

// WithSessionStorage enables request-driven node-local PVC persistence.
func (b *Binder) WithSessionStorage(storage SessionStorageManager) *Binder {
	b.storage = storage
	return b
}

// AcquireSpec is what a caller needs to bind a session to a sandbox.
type AcquireSpec struct {
	SessionID                 string
	UserID                    string
	Image                     string
	Env                       map[string]string
	AllowWorkspaceReset       bool
	AdditionalEgressAllowlist []string
}

// Outcome reports the result of an Acquire: the bound sandbox and whether it
// was reused (hit) or freshly created (miss).
type Outcome struct {
	Sandbox               *provider.Sandbox
	Reused                bool
	WorkspaceState        provider.WorkspaceState
	WorkspaceNode         string
	PreviousWorkspaceNode string
}

type preparedSessionReset struct {
	current SessionStorageBinding
	next    SessionStorageBinding
}

// Acquire returns the sandbox bound to spec.SessionID, creating one if none
// exists. Convenience wrapper over AcquireWithOutcome for callers that don't
// care about the hit/miss signal.
func (b *Binder) Acquire(ctx context.Context, spec AcquireSpec) (*provider.Sandbox, error) {
	out, err := b.AcquireWithOutcome(ctx, spec)
	if err != nil {
		return nil, err
	}
	return out.Sandbox, nil
}

// LookupBinding returns the sandbox currently bound to a session without
// creating one. It is a read-only path for callers (e.g. the Preview Proxy)
// that need a session's live sandbox id but must never provision. Returns
// ok=false when no healthy sandbox is bound. Unlike Acquire it takes no
// per-session lock: a concurrent Release may race, in which case the caller
// simply gets ok=false or a subsequently-invalid id, which the downstream
// resolve tolerates.
func (b *Binder) LookupBinding(ctx context.Context, sessionID, userID string) (*provider.Sandbox, bool, error) {
	if sessionID == "" {
		return nil, false, errors.New("orchestrator: session id required")
	}
	if userID == "" {
		return nil, false, errors.New("orchestrator: user id required")
	}
	return b.lookup(ctx, sessionID, userID)
}

// AcquireWithOutcome is the heart of M2's "same session reuses same sandbox"
// guarantee.
//
// Every Acquire shares the per-session lock with Release. It then reuses the
// current sandbox or creates and CAS-binds a fresh one. Reused=false only when
// this call actually won the binding.
func (b *Binder) AcquireWithOutcome(ctx context.Context, spec AcquireSpec) (Outcome, error) {
	if spec.SessionID == "" {
		return Outcome{}, errors.New("orchestrator: session id required")
	}
	if spec.UserID == "" {
		return Outcome{}, errors.New("orchestrator: user id required")
	}

	// The extra Redis round trip keeps deletion and reuse mutually exclusive: a
	// successful Acquire cannot return a sandbox that Release just destroyed.
	lock, err := acquireLock(ctx, b.kv, spec.SessionID, b.cfg.LockTTL, b.cfg.LockRetry)
	if err != nil {
		return Outcome{}, fmt.Errorf("acquire lock: %w", err)
	}
	defer func() { _ = lock.release(ctx) }()

	// Resolve only after the lock is held; another replica may have bound while
	// this request waited.
	if sb, ok, err := b.lookup(ctx, spec.SessionID, spec.UserID); err != nil {
		return Outcome{}, err
	} else if ok {
		reuse, resetRequired, err := b.runningSandboxReusable(ctx, sb, spec)
		if err != nil {
			return Outcome{}, err
		}
		if reuse {
			if err := b.renewLease(ctx, sb.ID); err != nil {
				return Outcome{}, err
			}
			b.recordHit()
			return Outcome{
				Sandbox: sb, Reused: true, WorkspaceState: provider.WorkspacePreserved,
				WorkspaceNode: sb.NodeName,
			}, nil
		}
		if resetRequired {
			// The user explicitly accepted abandoning the unavailable node. Its
			// compute binding must no longer shadow the new generation.
			if err := b.p.Destroy(ctx, sb.ID); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return Outcome{}, fmt.Errorf("destroy sandbox before workspace reset: %w", err)
			}
			if err := b.unbind(ctx, spec.SessionID, sb.ID); err != nil {
				return Outcome{}, err
			}
		}
	}

	start := time.Now()

	targetNode, claimName := "", ""
	var selectedBinding SessionStorageBinding
	workspaceState := provider.WorkspaceFresh
	previousNode := ""
	var preparedReset *preparedSessionReset
	if b.storage != nil {
		if b.cap == nil {
			return Outcome{}, errors.New("orchestrator: session storage requires capacity guard")
		}
		binding, found, err := b.storage.Get(ctx, spec.UserID, spec.SessionID)
		if err != nil {
			return Outcome{}, err
		}
		if found {
			workspaceState = provider.WorkspacePreserved
			targetNode, err = b.cap.SelectNode(ctx, binding.NodeName, "", nil)
			if errors.Is(err, ErrWorkspaceNodeUnavailable) && spec.AllowWorkspaceReset {
				previousNode = binding.NodeName
				requestedByNode, usageErr := b.storage.NodeRequestedBytes(ctx)
				if usageErr != nil {
					return Outcome{}, usageErr
				}
				targetNode, err = b.cap.SelectNode(ctx, "", binding.NodeName, requestedByNode)
				if err == nil {
					current := binding
					binding, err = b.storage.PrepareReset(ctx, current, targetNode, "previous workspace node unavailable")
					if err == nil {
						preparedReset = &preparedSessionReset{current: current, next: binding}
						workspaceState = provider.WorkspaceReset
					}
				}
			}
			if err != nil {
				return Outcome{}, err
			}
			if err := b.storage.EnsurePVC(ctx, binding); err != nil {
				return Outcome{}, err
			}
			selectedBinding = binding
			claimName = binding.PVCName
		} else {
			requestedByNode, usageErr := b.storage.NodeRequestedBytes(ctx)
			if usageErr != nil {
				return Outcome{}, usageErr
			}
			targetNode, err = b.cap.SelectNode(ctx, "", "", requestedByNode)
			if err != nil {
				return Outcome{}, err
			}
			binding, err = b.storage.Create(ctx, spec.UserID, spec.SessionID, targetNode)
			if err != nil {
				return Outcome{}, err
			}
			selectedBinding = binding
			claimName = binding.PVCName
		}
	} else if b.cap != nil {
		var err error
		targetNode, err = b.cap.SelectNode(ctx, "", "", nil)
		if err != nil {
			return Outcome{}, err
		}
	}

	sb, err := b.p.Create(ctx, provider.SandboxSpec{
		UserID:         spec.UserID,
		SessionID:      spec.SessionID,
		Image:          spec.Image,
		Env:            spec.Env,
		Networking:     mergeSessionNetworking(b.net, spec.AdditionalEgressAllowlist),
		TargetNodeName: targetNode,
		SessionClaim:   claimName,
	})
	if err != nil {
		if preparedReset != nil {
			err = errors.Join(err, b.discardPreparedReset(ctx, preparedReset.next))
		}
		return Outcome{}, fmt.Errorf("provider create: %w", err)
	}
	sb.NodeName = targetNode
	sb.StorageID = selectedBinding.StorageID
	sb.PVCNamespace = selectedBinding.PVCNamespace
	sb.SessionClaim = selectedBinding.PVCName
	sb.StorageGeneration = selectedBinding.Generation

	m := meta{
		SandboxID:         sb.ID,
		SessionID:         spec.SessionID,
		UserID:            spec.UserID,
		Image:             spec.Image,
		State:             StateActive,
		CreatedUnix:       time.Now().Unix(),
		NodeName:          targetNode,
		StorageID:         selectedBinding.StorageID,
		PVCNamespace:      selectedBinding.PVCNamespace,
		SessionClaim:      selectedBinding.PVCName,
		StorageGeneration: selectedBinding.Generation,
	}
	bindStatus, err := b.bind(ctx, m, lock)
	if err != nil {
		// Roll back the orphaned sandbox so a failed bind never leaks a container.
		err = errors.Join(err, b.cleanupCreatedSandbox(ctx, sb.ID, "", preparedReset))
		return Outcome{}, fmt.Errorf("bind: %w", err)
	}
	if bindStatus != bindWon {
		// The create lock may expire during a long image pull. Redis CAS decides
		// the sole winner; a late creator is destroyed before any Agent task can
		// execute in it, then returns the already-bound sandbox to its caller.
		if err := b.cleanupCreatedSandbox(ctx, sb.ID, "", preparedReset); err != nil {
			return Outcome{}, fmt.Errorf("cleanup concurrent create loser: %w", err)
		}
		winner, ok, err := b.lookup(ctx, spec.SessionID, spec.UserID)
		if err != nil {
			return Outcome{}, err
		}
		if !ok && bindStatus == bindLockLost {
			return Outcome{}, ErrSessionLockLost
		}
		if !ok {
			return Outcome{}, errors.New("concurrent sandbox winner is unavailable")
		}
		if b.storage != nil {
			binding, found, storageErr := b.storage.Get(ctx, spec.UserID, spec.SessionID)
			if storageErr != nil {
				return Outcome{}, storageErr
			}
			// A reset winner writes Redis before its PostgreSQL CAS. Do not expose
			// that Sandbox to a loser until the mounted claim is authoritative.
			if !found || !sandboxMatchesStorage(winner, binding) {
				return Outcome{}, ErrSessionLockLost
			}
		}
		if err := b.renewLease(ctx, winner.ID); err != nil {
			return Outcome{}, err
		}
		b.recordHit()
		return Outcome{
			Sandbox: winner, Reused: true, WorkspaceState: provider.WorkspacePreserved,
			WorkspaceNode: winner.NodeName,
		}, nil
	}

	if preparedReset != nil {
		if _, err := b.storage.CommitReset(ctx, preparedReset.current, preparedReset.next); err != nil {
			cleanupErr := b.cleanupCreatedSandbox(ctx, sb.ID, spec.SessionID, preparedReset)
			return Outcome{}, fmt.Errorf("commit workspace reset: %w", errors.Join(err, cleanupErr))
		}
	}

	b.recordMiss(time.Since(start))
	return Outcome{
		Sandbox: sb, Reused: false, WorkspaceState: workspaceState,
		WorkspaceNode: targetNode, PreviousWorkspaceNode: previousNode,
	}, nil
}

func (b *Binder) discardPreparedReset(ctx context.Context, binding SessionStorageBinding) error {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	return b.storage.DiscardReset(cleanupCtx, binding)
}

func (b *Binder) cleanupCreatedSandbox(ctx context.Context, sandboxID, sessionID string, reset *preparedSessionReset) error {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	destroyErr := b.p.Destroy(cleanupCtx, sandboxID)
	if errors.Is(destroyErr, fs.ErrNotExist) {
		destroyErr = nil
	}
	if destroyErr != nil {
		return destroyErr
	}
	var unbindErr error
	if sessionID != "" {
		unbindErr = b.unbind(cleanupCtx, sessionID, sandboxID)
	}
	var discardErr error
	if reset != nil {
		discardErr = b.storage.DiscardReset(cleanupCtx, reset.next)
	}
	return errors.Join(unbindErr, discardErr)
}

func (b *Binder) runningSandboxReusable(ctx context.Context, sb *provider.Sandbox, spec AcquireSpec) (bool, bool, error) {
	if b.storage == nil || sb.NodeName == "" {
		return true, false, nil
	}
	binding, found, err := b.storage.Get(ctx, spec.UserID, spec.SessionID)
	if err != nil {
		return false, false, err
	}
	if !found || !sandboxMatchesStorage(sb, binding) {
		// Redis can outlive a crash between binding the compute sandbox and
		// committing a reset in PostgreSQL. Never reuse a sandbox whose mounted
		// claim is not the current durable binding.
		if err := b.p.Destroy(ctx, sb.ID); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return false, false, fmt.Errorf("destroy sandbox with stale storage binding: %w", err)
		}
		unbindErr := b.unbind(ctx, spec.SessionID, sb.ID)
		var discardErr error
		if sb.SessionClaim != "" && (!found || sb.SessionClaim != binding.PVCName) {
			stale := SessionStorageBinding{
				StorageID: sb.StorageID, PVCNamespace: sb.PVCNamespace,
				PVCName: sb.SessionClaim, Generation: sb.StorageGeneration,
			}
			discardErr = b.storage.DiscardReset(ctx, stale)
		}
		if err := errors.Join(unbindErr, discardErr); err != nil {
			return false, false, fmt.Errorf("clean stale storage binding: %w", err)
		}
		return false, false, nil
	}
	checker, ok := b.cap.(NodeAvailabilityChecker)
	if !ok {
		return true, false, nil
	}
	available, err := checker.NodeAvailable(ctx, sb.NodeName)
	if err != nil {
		return false, false, err
	}
	if available {
		return true, false, nil
	}
	if !spec.AllowWorkspaceReset {
		return false, false, ErrWorkspaceNodeUnavailable
	}
	return false, true, nil
}

func sandboxMatchesStorage(sb *provider.Sandbox, binding SessionStorageBinding) bool {
	return sb.StorageID == binding.StorageID &&
		sb.PVCNamespace == binding.PVCNamespace &&
		sb.SessionClaim == binding.PVCName &&
		sb.StorageGeneration == binding.Generation &&
		sb.NodeName == binding.NodeName
}

// lookup resolves the forward mapping to a provider.Sandbox. The second return
// is false when the session has no binding. A dangling forward key (sandbox
// meta gone) is treated as "no binding" and cleaned opportunistically.
func (b *Binder) lookup(ctx context.Context, sessionID, userID string) (*provider.Sandbox, bool, error) {
	sid, err := b.kv.Get(ctx, convKey(sessionID))
	if errors.Is(err, rds.ErrNil) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	raw, err := b.kv.Get(ctx, metaKey(sid))
	if errors.Is(err, rds.ErrNil) {
		// Forward pointer survived but the sandbox record is gone — stale. Drop it.
		_, _ = b.kv.Del(ctx, convKey(sessionID))
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var m meta
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return nil, false, err
	}
	if m.UserID == "" || m.UserID != userID {
		return nil, false, ErrSessionOwnerMismatch
	}
	health, err := b.p.Health(ctx, sid)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			// The binding is durable but the provider has already lost the
			// sandbox. Drop the stale mapping and let Acquire create a fresh
			// sandbox for this session.
			if unbindErr := b.unbind(ctx, m.SessionID, m.SandboxID); unbindErr != nil {
				return nil, false, unbindErr
			}
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("health sandbox: %w", err)
	}
	if health == nil || !health.Healthy {
		if health != nil && health.Transitional {
			return nil, false, fmt.Errorf("sandbox is not ready: %s", health.Detail)
		}
		// A provider can still resolve a terminal sandbox without a transport
		// error. Never return one as a reusable execution environment. Remove
		// the stale instance before dropping its binding so a failed destroy
		// remains retryable on the next Acquire.
		if err := b.p.Destroy(ctx, m.SandboxID); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, false, fmt.Errorf("destroy unhealthy sandbox: %w", err)
		}
		if err := b.unbind(ctx, m.SessionID, m.SandboxID); err != nil {
			return nil, false, err
		}
		return nil, false, nil
	}
	return &provider.Sandbox{
		ID:                m.SandboxID,
		UserID:            m.UserID,
		SessionID:         m.SessionID,
		NodeName:          m.NodeName,
		StorageID:         m.StorageID,
		PVCNamespace:      m.PVCNamespace,
		SessionClaim:      m.SessionClaim,
		StorageGeneration: m.StorageGeneration,
	}, true, nil
}

// bind atomically writes the bidirectional mapping + meta + lease for a freshly
// created sandbox. Uses a Lua script so a crash mid-write can't leave half a
// mapping (e.g. forward without reverse).
const (
	bindLockLost int64 = -1
	bindExists   int64 = 0
	bindWon      int64 = 1
)

func (b *Binder) bind(ctx context.Context, m meta, lock *distLock) (int64, error) {
	raw, err := json.Marshal(m)
	if err != nil {
		return bindExists, err
	}
	leaseSecs := int(b.cfg.LeaseTTL.Seconds())
	lockMillis := max(1, b.cfg.LockTTL.Milliseconds())
	// KEYS: conv, rev, meta, lease, lock
	// ARGV: sandboxID, sessionID, metaJSON, leaseTTL, lockToken, lockTTLMillis
	result, err := b.kv.Eval(ctx, luaBind,
		[]string{
			convKey(m.SessionID), revKey(m.SandboxID), metaKey(m.SandboxID),
			leaseKey(m.SandboxID), lock.key,
		},
		m.SandboxID, m.SessionID, string(raw), leaseSecs, lock.token, lockMillis,
	)
	if err != nil {
		return bindExists, err
	}
	status, ok := result.(int64)
	if !ok {
		return bindExists, fmt.Errorf("bind returned unexpected result %T", result)
	}
	return status, nil
}

// luaBind writes all four binding keys only when the Session has no winner.
// This CAS is the fencing boundary when a slow creator outlives its lock TTL.
const luaBind = `
if redis.call("GET", KEYS[5]) ~= ARGV[5] then
	return -1
end
if redis.call("EXISTS", KEYS[1]) == 1 then
	return 0
end
redis.call("SET", KEYS[1], ARGV[1])
redis.call("SET", KEYS[2], ARGV[2])
redis.call("SET", KEYS[3], ARGV[3])
redis.call("SET", KEYS[4], "1", "EX", tonumber(ARGV[4]))
redis.call("PEXPIRE", KEYS[5], tonumber(ARGV[6]))
return 1`

// putMeta overwrites the durable meta record (used on state transitions).
func (b *Binder) putMeta(ctx context.Context, m meta) error {
	raw, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return b.kv.Set(ctx, metaKey(m.SandboxID), string(raw), 0)
}

// renewLease pushes a sandbox's lease TTL forward. Called on every Acquire hit
// and by the heartbeat worker.
func (b *Binder) renewLease(ctx context.Context, sandboxID string) error {
	// Re-create rather than EXPIRE: a sandbox briefly past its lease (but not
	// yet reaped) must still be renewable on a fresh request.
	return b.kv.Set(ctx, leaseKey(sandboxID), "1", b.cfg.LeaseTTL)
}

// Release explicitly unbinds and destroys a session's sandbox immediately,
// bypassing the lease grace period. Used when a session ends cleanly.
func (b *Binder) Release(ctx context.Context, userID, sessionID string) error {
	lock, err := acquireLock(ctx, b.kv, sessionID, b.cfg.LockTTL, b.cfg.LockRetry)
	if err != nil {
		return fmt.Errorf("release lock: %w", err)
	}
	defer func() { _ = lock.release(ctx) }()

	sid, err := b.kv.Get(ctx, convKey(sessionID))
	if err != nil && !errors.Is(err, rds.ErrNil) {
		return err
	}
	var m meta
	if sid != "" {
		raw, metaErr := b.kv.Get(ctx, metaKey(sid))
		if metaErr == nil {
			_ = json.Unmarshal([]byte(raw), &m)
		}
		if m.UserID != "" && m.UserID != userID {
			return ErrSessionOwnerMismatch
		}
		if err := b.p.Destroy(ctx, sid); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("provider destroy: %w", err)
		}
		if err := b.unbind(ctx, sessionID, sid); err != nil {
			return err
		}
	}
	if b.storage != nil {
		return b.storage.Delete(ctx, userID, sessionID)
	}
	if cleaner, ok := b.p.(provider.SessionStorageCleaner); ok && userID != "" {
		if err := cleaner.CleanupSessionStorage(ctx, userID, sessionID); err != nil {
			return err
		}
	}
	return nil
}

// unbind removes the reverse/meta/lease keys and removes the forward pointer
// only when it still names this sandbox. A stale reaper must never erase a
// newer sandbox that won the Session binding meanwhile.
func (b *Binder) unbind(ctx context.Context, sessionID, sandboxID string) error {
	_, err := b.kv.Eval(ctx, luaUnbind,
		[]string{convKey(sessionID), revKey(sandboxID), metaKey(sandboxID), leaseKey(sandboxID)},
		sandboxID,
	)
	return err
}

const luaUnbind = `
if redis.call("GET", KEYS[1]) == ARGV[1] then
	redis.call("DEL", KEYS[1])
end
return redis.call("DEL", KEYS[2], KEYS[3], KEYS[4])`

func (b *Binder) recordHit() {
	if b.metrics != nil {
		b.metrics.recordHit()
	}
}
func (b *Binder) recordMiss(d time.Duration) {
	if b.metrics != nil {
		b.metrics.recordMiss(d)
	}
}
