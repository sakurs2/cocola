package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"k8s.io/apimachinery/pkg/api/resource"
)

const (
	defaultSessionVolumeSize   = "2Gi"
	defaultSessionStorageClass = "cocola-local-session"
	sessionVolumeSettingKey    = "storage.session_volume_default_size"
)

var ErrSessionStorageOwnerMismatch = errors.New("session storage owner mismatch")

type SessionStorageBinding struct {
	StorageID       string
	SessionID       string
	UserID          string
	PVCNamespace    string
	PVCName         string
	NodeName        string
	Generation      int64
	RequestedBytes  int64
	LastResetReason string
	LastResetAt     *time.Time
}

type SessionStorageManager interface {
	Get(ctx context.Context, userID, sessionID string) (SessionStorageBinding, bool, error)
	Create(ctx context.Context, userID, sessionID, nodeName string) (SessionStorageBinding, error)
	PrepareReset(ctx context.Context, current SessionStorageBinding, nodeName, reason string) (SessionStorageBinding, error)
	CommitReset(ctx context.Context, current, next SessionStorageBinding) (SessionStorageBinding, error)
	DiscardReset(ctx context.Context, next SessionStorageBinding) error
	EnsurePVC(ctx context.Context, binding SessionStorageBinding) error
	NodeRequestedBytes(ctx context.Context) (map[string]int64, error)
	Delete(ctx context.Context, userID, sessionID string) error
	Close()
}

type sessionStorageKube interface {
	ensureSessionPVC(ctx context.Context, namespace, name, storageClass, size string, labels map[string]string) error
	deleteSessionPVC(ctx context.Context, namespace, claimName string) error
}

type postgresSessionStorage struct {
	pool         *pgxpool.Pool
	kube         sessionStorageKube
	namespace    string
	storageClass string
	now          func() time.Time
}

func NewSessionStorageManagerFromEnv(ctx context.Context) (SessionStorageManager, error) {
	if strings.ToLower(strings.TrimSpace(os.Getenv("COCOLA_CLUSTER_MANAGER_MODE"))) != "k3s" {
		return nil, nil
	}
	dsn := strings.TrimSpace(os.Getenv("COCOLA_PG_DSN"))
	if dsn == "" {
		return nil, errors.New("session storage: COCOLA_PG_DSN is required in k3s mode")
	}
	kcfg, ok, err := kubeConfigFromEnv()
	if err != nil || !ok {
		if err == nil {
			err = errors.New("Kubernetes configuration is required in k3s mode")
		}
		return nil, fmt.Errorf("session storage: %w", err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("session storage postgres: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("session storage postgres ping: %w", err)
	}
	namespace := kcfg.SandboxNamespace
	storageClass := strings.TrimSpace(os.Getenv("COCOLA_SESSION_STORAGE_CLASS"))
	if storageClass == "" {
		storageClass = defaultSessionStorageClass
	}
	return &postgresSessionStorage{
		pool: pool, kube: newKubeClient(kcfg), namespace: namespace,
		storageClass: storageClass, now: time.Now,
	}, nil
}

func (m *postgresSessionStorage) Close() { m.pool.Close() }

func (m *postgresSessionStorage) Get(ctx context.Context, userID, sessionID string) (SessionStorageBinding, bool, error) {
	binding, err := m.get(ctx, sessionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return SessionStorageBinding{}, false, nil
	}
	if err != nil {
		return SessionStorageBinding{}, false, err
	}
	if binding.UserID != userID {
		return SessionStorageBinding{}, false, ErrSessionStorageOwnerMismatch
	}
	return binding, true, nil
}

func (m *postgresSessionStorage) Create(ctx context.Context, userID, sessionID, nodeName string) (SessionStorageBinding, error) {
	_, bytes, err := m.currentVolumeSize(ctx)
	if err != nil {
		return SessionStorageBinding{}, err
	}
	id := uuid.NewString()
	name := "cocola-sv-" + strings.ReplaceAll(id, "-", "")
	now := m.now().UTC()
	_, err = m.pool.Exec(ctx, `
INSERT INTO session_storage (
    storage_id, session_id, user_id, pvc_namespace, pvc_name, node_name,
    generation, requested_bytes, created_at, updated_at
) VALUES ($1::uuid,$2,$3,$4,$5,$6,1,$7,$8,$8)
ON CONFLICT (session_id) DO NOTHING`, id, sessionID, userID, m.namespace, name, nodeName, bytes, now)
	if err != nil {
		return SessionStorageBinding{}, fmt.Errorf("create session storage: %w", err)
	}
	binding, err := m.get(ctx, sessionID)
	if err != nil {
		return SessionStorageBinding{}, err
	}
	if binding.UserID != userID {
		return SessionStorageBinding{}, ErrSessionStorageOwnerMismatch
	}
	if err := m.EnsurePVC(ctx, binding); err != nil {
		return SessionStorageBinding{}, err
	}
	return binding, nil
}

func (m *postgresSessionStorage) PrepareReset(ctx context.Context, current SessionStorageBinding, nodeName, reason string) (SessionStorageBinding, error) {
	volumeID := uuid.NewString()
	name := "cocola-sv-" + strings.ReplaceAll(volumeID, "-", "")
	now := m.now().UTC()
	next := current
	next.PVCName = name
	next.NodeName = nodeName
	next.Generation++
	next.LastResetReason = reason
	next.LastResetAt = &now
	if err := m.EnsurePVC(ctx, next); err != nil {
		return SessionStorageBinding{}, err
	}
	return next, nil
}

func (m *postgresSessionStorage) CommitReset(ctx context.Context, current, next SessionStorageBinding) (SessionStorageBinding, error) {
	if current.StorageID != next.StorageID || current.SessionID != next.SessionID ||
		current.UserID != next.UserID || next.Generation != current.Generation+1 {
		return SessionStorageBinding{}, errors.New("invalid session storage reset")
	}
	resetAt := next.LastResetAt
	if resetAt == nil {
		now := m.now().UTC()
		resetAt = &now
	}
	row := m.pool.QueryRow(ctx, `
UPDATE session_storage
SET pvc_name=$2, node_name=$3, generation=$4,
	last_reset_reason=$5, last_reset_at=$6, updated_at=$6
WHERE storage_id=$1::uuid AND user_id=$7 AND generation=$8 AND pvc_name=$9
RETURNING storage_id::text, session_id, user_id, pvc_namespace, pvc_name,
          node_name, generation, requested_bytes, COALESCE(last_reset_reason,''), last_reset_at`,
		current.StorageID, next.PVCName, next.NodeName, next.Generation,
		next.LastResetReason, *resetAt, current.UserID, current.Generation, current.PVCName)
	updated, err := scanSessionStorage(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return SessionStorageBinding{}, errors.New("session storage changed concurrently")
	}
	if err != nil {
		return SessionStorageBinding{}, err
	}
	// Reset is explicit and irreversible. Cleanup is idempotent and must not
	// block use of the newly-created workspace if the old node is gone.
	_ = m.kube.deleteSessionPVC(ctx, current.PVCNamespace, current.PVCName)
	return updated, nil
}

func (m *postgresSessionStorage) DiscardReset(ctx context.Context, next SessionStorageBinding) error {
	return m.kube.deleteSessionPVC(ctx, next.PVCNamespace, next.PVCName)
}

func (m *postgresSessionStorage) EnsurePVC(ctx context.Context, binding SessionStorageBinding) error {
	quantity := *resource.NewQuantity(binding.RequestedBytes, resource.BinarySI)
	return m.ensurePVC(ctx, binding, quantity.String())
}

func (m *postgresSessionStorage) NodeRequestedBytes(ctx context.Context) (map[string]int64, error) {
	rows, err := m.pool.Query(ctx, `
SELECT node_name, COALESCE(SUM(requested_bytes),0)::bigint
FROM session_storage GROUP BY node_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var node string
		var bytes int64
		if err := rows.Scan(&node, &bytes); err != nil {
			return nil, err
		}
		out[node] = bytes
	}
	return out, rows.Err()
}

func (m *postgresSessionStorage) ensurePVC(ctx context.Context, binding SessionStorageBinding, size string) error {
	labels := map[string]string{
		"app.kubernetes.io/managed-by": "cocola",
		"cocola.dev/storage-id":        binding.StorageID,
		"cocola.dev/generation":        fmt.Sprintf("%d", binding.Generation),
		"cocola.dev/node-name":         binding.NodeName,
		"cocola.dev/requested-bytes":   fmt.Sprintf("%d", binding.RequestedBytes),
	}
	if err := m.kube.ensureSessionPVC(ctx, binding.PVCNamespace, binding.PVCName, m.storageClass, size, labels); err != nil {
		return fmt.Errorf("ensure session PVC: %w", err)
	}
	return nil
}

func (m *postgresSessionStorage) Delete(ctx context.Context, userID, sessionID string) error {
	binding, found, err := m.Get(ctx, userID, sessionID)
	if err != nil || !found {
		return err
	}
	if err := m.kube.deleteSessionPVC(ctx, binding.PVCNamespace, binding.PVCName); err != nil {
		return fmt.Errorf("delete session PVC: %w", err)
	}
	_, err = m.pool.Exec(ctx, `DELETE FROM session_storage WHERE storage_id=$1::uuid AND user_id=$2`, binding.StorageID, userID)
	return err
}

func (m *postgresSessionStorage) get(ctx context.Context, sessionID string) (SessionStorageBinding, error) {
	row := m.pool.QueryRow(ctx, `
SELECT storage_id::text, session_id, user_id, pvc_namespace, pvc_name,
       node_name, generation, requested_bytes, COALESCE(last_reset_reason,''), last_reset_at
FROM session_storage WHERE session_id=$1`, sessionID)
	return scanSessionStorage(row)
}

type rowScanner interface{ Scan(dest ...any) error }

func scanSessionStorage(row rowScanner) (SessionStorageBinding, error) {
	var binding SessionStorageBinding
	err := row.Scan(
		&binding.StorageID, &binding.SessionID, &binding.UserID,
		&binding.PVCNamespace, &binding.PVCName, &binding.NodeName,
		&binding.Generation, &binding.RequestedBytes, &binding.LastResetReason,
		&binding.LastResetAt,
	)
	return binding, err
}

func (m *postgresSessionStorage) currentVolumeSize(ctx context.Context) (resource.Quantity, int64, error) {
	raw := strings.TrimSpace(os.Getenv("COCOLA_SESSION_VOLUME_SIZE"))
	if raw == "" {
		raw = defaultSessionVolumeSize
	}
	var value json.RawMessage
	if err := m.pool.QueryRow(ctx, `SELECT value_json FROM system_settings WHERE key=$1`, sessionVolumeSettingKey).Scan(&value); err == nil {
		var override string
		if json.Unmarshal(value, &override) == nil && strings.TrimSpace(override) != "" {
			raw = override
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return resource.Quantity{}, 0, err
	}
	quantity, err := resource.ParseQuantity(raw)
	if err != nil || quantity.Sign() <= 0 || quantity.Value() <= 0 {
		return resource.Quantity{}, 0, fmt.Errorf("invalid session volume size %q", raw)
	}
	return quantity, quantity.Value(), nil
}
