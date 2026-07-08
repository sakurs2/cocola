package service

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	rds "github.com/cocola-project/cocola/packages/go-common/redis"
)

var ErrSandboxRuntimeNotConfigured = errors.New("service: sandbox runtime manager not configured")

// SandboxRuntimeManager is the read-only operations surface for currently
// bound sandboxes. Redis binding metadata is the source of truth; Kubernetes pod
// data is optional enrichment when admin-api has cluster access.
type SandboxRuntimeManager interface {
	ListSandboxes(ctx context.Context) (SandboxRuntimeList, error)
}

type SandboxRuntimeList struct {
	Sandboxes []SandboxRuntime `json:"sandboxes"`
}

type SandboxRuntime struct {
	SandboxID      string    `json:"sandbox_id"`
	SessionID      string    `json:"session_id"`
	UserID         string    `json:"user_id"`
	Username       string    `json:"username,omitempty"`
	Status         string    `json:"status"`
	LifecycleState string    `json:"lifecycle_state"`
	Image          string    `json:"image,omitempty"`
	CreatedAt      time.Time `json:"created_at,omitempty"`
	PausedAt       time.Time `json:"paused_at,omitempty"`
	PodName        string    `json:"pod_name,omitempty"`
	PodPhase       string    `json:"pod_phase,omitempty"`
	NodeName       string    `json:"node_name,omitempty"`
}

type SandboxPodReader interface {
	ListSandboxPods(ctx context.Context) ([]kubePod, error)
}

// SandboxPodDeleter is the optional teardown surface: an implementation can
// delete the BatchSandbox CRD that owns a sandbox pod. A pod reader that also
// implements this enables the admin manual-delete flow (warm + orphan cleanup).
type SandboxPodDeleter interface {
	DeleteSandboxObject(ctx context.Context, sandboxID string) error
}

type KubeSandboxPodReader struct {
	client    *kubeClient
	namespace string
}

func NewKubeSandboxPodReader(cfg kubeConfig) *KubeSandboxPodReader {
	return &KubeSandboxPodReader{client: newKubeClient(cfg), namespace: cfg.SandboxNamespace}
}

func (r *KubeSandboxPodReader) ListSandboxPods(ctx context.Context) ([]kubePod, error) {
	return r.client.listSandboxPods(ctx)
}

// DeleteSandboxObject deletes the BatchSandbox CRD named after the sandbox id;
// Kubernetes garbage-collects the owned pod. A missing object returns
// ErrNotFound, which the caller treats as already-gone.
func (r *KubeSandboxPodReader) DeleteSandboxObject(ctx context.Context, sandboxID string) error {
	return r.client.deleteBatchSandbox(ctx, r.namespace, sandboxID)
}

type RedisSandboxRuntimeManager struct {
	kv       rds.KV
	pods     SandboxPodReader
	username func(context.Context, string) string
}

type SandboxRuntimeOption func(*RedisSandboxRuntimeManager)

func WithSandboxPodReader(pods SandboxPodReader) SandboxRuntimeOption {
	return func(m *RedisSandboxRuntimeManager) { m.pods = pods }
}

func NewRedisSandboxRuntimeManager(kv rds.KV, opts ...SandboxRuntimeOption) *RedisSandboxRuntimeManager {
	m := &RedisSandboxRuntimeManager{kv: kv}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

func (a *Admin) ListSandboxes(ctx context.Context) (SandboxRuntimeList, error) {
	if a.sandboxRuntimes == nil {
		return SandboxRuntimeList{}, ErrSandboxRuntimeNotConfigured
	}
	return a.sandboxRuntimes.ListSandboxes(ctx)
}

func (m *RedisSandboxRuntimeManager) ListSandboxes(ctx context.Context) (SandboxRuntimeList, error) {
	if m == nil || m.kv == nil {
		return SandboxRuntimeList{}, ErrSandboxRuntimeNotConfigured
	}
	podsBySandbox := map[string]kubePod{}
	podStateAvailable := false
	if m.pods != nil {
		if pods, err := m.pods.ListSandboxPods(ctx); err == nil {
			podStateAvailable = true
			for _, p := range pods {
				if sid := podSandboxID(p); sid != "" {
					podsBySandbox[sid] = p
				}
			}
		}
	}

	var out []SandboxRuntime
	// accounted tracks every sandbox id that a meta or warm record explains, so
	// the orphan pass below can flag pods that no Redis record covers (B-class
	// orphans: a live pod with no binding and no warm entry).
	accounted := map[string]bool{}
	err := m.kv.ScanKeys(ctx, sandboxMetaScanPattern(), 100, func(keys []string) error {
		for _, key := range keys {
			raw, err := m.kv.Get(ctx, key)
			if errors.Is(err, rds.ErrNil) {
				continue
			}
			if err != nil {
				return err
			}
			var meta sandboxRuntimeMeta
			if err := json.Unmarshal([]byte(raw), &meta); err != nil {
				continue
			}
			if meta.SandboxID == "" {
				continue
			}
			accounted[meta.SandboxID] = true
			leasePresent := true
			if _, err := m.kv.Get(ctx, sandboxLeaseKey(meta.SandboxID)); errors.Is(err, rds.ErrNil) {
				leasePresent = false
			} else if err != nil {
				return err
			}
			pod, hasPod := podsBySandbox[meta.SandboxID]
			if podStateAvailable && !leasePresent && !hasPod {
				if err := m.unbindStaleRuntime(ctx, meta); err != nil {
					return err
				}
				continue
			}
			rt := runtimeFromMeta(meta, leasePresent, pod)
			if m.username != nil {
				rt.Username = m.username(ctx, rt.UserID)
			}
			out = append(out, rt)
		}
		return nil
	})
	if err != nil {
		return SandboxRuntimeList{}, err
	}

	// Warm pool: session-agnostic pre-warmed sandboxes waiting to be claimed.
	// They live under warm:* (not meta:*), so surface them here with a fixed
	// "ready" status. Once claimed they migrate to meta:* and show as running.
	if err := m.kv.ScanKeys(ctx, warmScanPattern(), 100, func(keys []string) error {
		for _, key := range keys {
			raw, err := m.kv.Get(ctx, key)
			if errors.Is(err, rds.ErrNil) {
				continue
			}
			if err != nil {
				return err
			}
			var wm warmRuntimeMeta
			if err := json.Unmarshal([]byte(raw), &wm); err != nil {
				continue
			}
			if wm.SandboxID == "" {
				continue
			}
			accounted[wm.SandboxID] = true
			pod := podsBySandbox[wm.SandboxID]
			out = append(out, warmRuntime(wm, pod))
		}
		return nil
	}); err != nil {
		return SandboxRuntimeList{}, err
	}

	// B-class orphans: a pod exists but no meta/warm record explains it (e.g. a
	// crashed bind, a leaked warm create, or manual kubectl tinkering). Only
	// detectable when pod state is available. Surfaced as "orphan" so an admin
	// can reclaim it from the UI.
	if podStateAvailable {
		for sid, pod := range podsBySandbox {
			if sid == "" || accounted[sid] {
				continue
			}
			out = append(out, orphanRuntime(sid, pod))
		}
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return SandboxRuntimeList{Sandboxes: out}, nil
}

// DeleteSandbox tears down a sandbox by deleting its BatchSandbox CRD (which
// garbage-collects the pod) and clearing every Redis record that could reference
// it (warm/meta/conv/rev/lease). It is the admin manual-reclaim path for warm
// and orphan entries. Requires a pod reader that also implements
// SandboxPodDeleter (i.e. Kubernetes access); otherwise returns
// ErrSandboxRuntimeNotConfigured. A CRD already gone is not an error — the Redis
// cleanup still runs so a stale record never lingers.
func (m *RedisSandboxRuntimeManager) DeleteSandbox(ctx context.Context, sandboxID string) error {
	if m == nil || m.kv == nil {
		return ErrSandboxRuntimeNotConfigured
	}
	sandboxID = strings.TrimSpace(sandboxID)
	if sandboxID == "" {
		return ErrInvalidArg
	}
	deleter, ok := m.pods.(SandboxPodDeleter)
	if !ok || deleter == nil {
		return ErrSandboxRuntimeNotConfigured
	}
	// Resolve the bound session (if any) before we wipe the reverse index, so we
	// can also clear the forward conv:{session} key for a live binding.
	sessionID := ""
	if raw, err := m.kv.Get(ctx, sandboxRevKey(sandboxID)); err == nil {
		sessionID = raw
	} else if !errors.Is(err, rds.ErrNil) {
		return err
	}
	if err := deleter.DeleteSandboxObject(ctx, sandboxID); err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	keys := []string{
		warmKey(sandboxID),
		sandboxMetaKey(sandboxID),
		sandboxRevKey(sandboxID),
		sandboxLeaseKey(sandboxID),
	}
	if sessionID != "" {
		keys = append(keys, sandboxConvKey(sessionID))
	}
	if _, err := m.kv.Del(ctx, keys...); err != nil {
		return err
	}
	return nil
}

func (a *Admin) DeleteSandbox(ctx context.Context, sandboxID string) error {
	if a.sandboxRuntimes == nil {
		return ErrSandboxRuntimeNotConfigured
	}
	deleter, ok := a.sandboxRuntimes.(interface {
		DeleteSandbox(context.Context, string) error
	})
	if !ok {
		return ErrSandboxRuntimeNotConfigured
	}
	return deleter.DeleteSandbox(ctx, sandboxID)
}

func (a *Admin) AttachSandboxRuntimeUsernames(m *RedisSandboxRuntimeManager) *RedisSandboxRuntimeManager {
	if m == nil {
		return nil
	}
	m.username = a.sandboxRuntimeUsername
	return m
}

func (a *Admin) sandboxRuntimeUsername(ctx context.Context, userID string) string {
	userID = strings.TrimSpace(userID)
	if userID == "" || a.store == nil {
		return ""
	}
	if u, err := a.store.GetAuthUserByIdentifier(ctx, normalizeIdentifier(userID)); err == nil && u.DeletedAt.IsZero() {
		return u.Username
	}
	if u, err := a.store.GetAuthUser(ctx, userID); err == nil && u.DeletedAt.IsZero() {
		return u.Username
	}
	return ""
}

func NewSandboxRuntimeManagerFromEnv(kv rds.KV) (SandboxRuntimeManager, error) {
	if kv == nil {
		return nil, nil
	}
	opts := []SandboxRuntimeOption{}
	cfg, ok, err := kubeConfigFromEnv()
	if err != nil {
		return nil, err
	}
	if ok {
		opts = append(opts, WithSandboxPodReader(NewKubeSandboxPodReader(cfg)))
	}
	return NewRedisSandboxRuntimeManager(kv, opts...), nil
}

type sandboxRuntimeMeta struct {
	SandboxID   string `json:"sandbox_id"`
	SessionID   string `json:"session_id"`
	UserID      string `json:"user_id"`
	Image       string `json:"image"`
	State       string `json:"state"`
	CreatedUnix int64  `json:"created_unix"`
	PausedUnix  int64  `json:"paused_unix"`
}

const sandboxRuntimeKeyPrefix = "cocola:sb:"

func sandboxMetaScanPattern() string { return sandboxRuntimeKeyPrefix + "meta:*" }
func sandboxMetaKey(sandboxID string) string { return sandboxRuntimeKeyPrefix + "meta:" + sandboxID }
func sandboxConvKey(sessionID string) string { return sandboxRuntimeKeyPrefix + "conv:" + sessionID }
func sandboxRevKey(sandboxID string) string  { return sandboxRuntimeKeyPrefix + "rev:" + sandboxID }
func sandboxLeaseKey(sandboxID string) string {
	return sandboxRuntimeKeyPrefix + "lease:" + sandboxID
}

// warm:{sandbox} holds one pre-warmed, session-agnostic sandbox awaiting a
// claim. Mirrors sandbox-manager's key layout so admin-api can read the pool.
func warmScanPattern() string       { return sandboxRuntimeKeyPrefix + "warm:*" }
func warmKey(sandboxID string) string { return sandboxRuntimeKeyPrefix + "warm:" + sandboxID }

// warmRuntimeMeta mirrors the JSON sandbox-manager writes under warm:{id}. Only
// the fields admin-api displays are decoded; unknown fields are ignored.
type warmRuntimeMeta struct {
	SandboxID   string `json:"sandbox_id"`
	Image       string `json:"image"`
	NodeName    string `json:"node_name"`
	CreatedUnix int64  `json:"created_unix"`
}

// warmRuntime renders a warm-pool entry as a "ready" runtime row. Pod fields are
// filled when cluster state is available so the admin can see where it landed.
func warmRuntime(wm warmRuntimeMeta, pod kubePod) SandboxRuntime {
	rt := SandboxRuntime{
		SandboxID:      wm.SandboxID,
		Status:         "ready",
		LifecycleState: "warm",
		Image:          wm.Image,
		NodeName:       wm.NodeName,
		PodName:        pod.Metadata.Name,
		PodPhase:       pod.Status.Phase,
	}
	if pod.Spec.NodeName != "" {
		rt.NodeName = pod.Spec.NodeName
	}
	if wm.CreatedUnix > 0 {
		rt.CreatedAt = time.Unix(wm.CreatedUnix, 0).UTC()
	}
	return rt
}

// orphanRuntime renders a pod that no Redis record explains as an "orphan" row
// so an admin can reclaim it from the UI. All identity comes from the pod.
func orphanRuntime(sandboxID string, pod kubePod) SandboxRuntime {
	return SandboxRuntime{
		SandboxID:      sandboxID,
		Status:         "orphan",
		LifecycleState: "orphan",
		PodName:        pod.Metadata.Name,
		PodPhase:       pod.Status.Phase,
		NodeName:       pod.Spec.NodeName,
	}
}

func (m *RedisSandboxRuntimeManager) unbindStaleRuntime(ctx context.Context, meta sandboxRuntimeMeta) error {
	_, err := m.kv.Del(ctx,
		sandboxConvKey(meta.SessionID),
		sandboxRevKey(meta.SandboxID),
		sandboxMetaKey(meta.SandboxID),
		sandboxLeaseKey(meta.SandboxID),
	)
	return err
}

func runtimeFromMeta(meta sandboxRuntimeMeta, leasePresent bool, pod kubePod) SandboxRuntime {
	rt := SandboxRuntime{
		SandboxID:      meta.SandboxID,
		SessionID:      meta.SessionID,
		UserID:         meta.UserID,
		Status:         sandboxRuntimeStatus(meta.State, leasePresent, pod.Status.Phase),
		LifecycleState: meta.State,
		Image:          meta.Image,
		PodName:        pod.Metadata.Name,
		PodPhase:       pod.Status.Phase,
		NodeName:       pod.Spec.NodeName,
	}
	if meta.CreatedUnix > 0 {
		rt.CreatedAt = time.Unix(meta.CreatedUnix, 0).UTC()
	}
	if meta.PausedUnix > 0 {
		rt.PausedAt = time.Unix(meta.PausedUnix, 0).UTC()
	}
	return rt
}

func sandboxRuntimeStatus(lifecycle string, leasePresent bool, podPhase string) string {
	switch podPhase {
	case "Pending":
		return "starting"
	case "Succeeded", "Failed":
		return "stopped"
	}
	switch lifecycle {
	case "paused":
		return "reclaiming"
	case "active":
		if !leasePresent {
			return "pending_reclaim"
		}
		return "running"
	default:
		return "unknown"
	}
}

func podSandboxID(p kubePod) string {
	for _, key := range []string{"opensandbox.io/id", "cocola.sandbox_id"} {
		if v := strings.TrimSpace(p.Metadata.Labels[key]); v != "" {
			return v
		}
	}
	return strings.TrimSpace(p.Metadata.Name)
}
