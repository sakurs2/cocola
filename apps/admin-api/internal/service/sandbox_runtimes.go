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

type KubeSandboxPodReader struct {
	client *kubeClient
}

func NewKubeSandboxPodReader(cfg kubeConfig) *KubeSandboxPodReader {
	return &KubeSandboxPodReader{client: newKubeClient(cfg)}
}

func (r *KubeSandboxPodReader) ListSandboxPods(ctx context.Context) ([]kubePod, error) {
	return r.client.listSandboxPods(ctx)
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
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return SandboxRuntimeList{Sandboxes: out}, nil
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
