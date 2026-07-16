package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SessionStorageMonitor is request-driven administration for node-local
// session volumes. It intentionally has no worker, retry queue or timer.
type SessionStorageMonitor interface {
	List(ctx context.Context) ([]SessionStorageView, error)
	NodeUsage(ctx context.Context) (map[string]NodeStorageUsage, error)
	NodeFilesystems(ctx context.Context) ([]NodeStorageFilesystem, error)
	Measure(ctx context.Context, storageID, pvcName string) (SessionStorageMeasurement, error)
	DeleteOrphan(ctx context.Context, storageID, pvcName string) error
	Close()
}

type NodeStorageUsage struct {
	SessionCount   int   `json:"session_count"`
	RequestedBytes int64 `json:"requested_bytes"`
	ResetCount     int   `json:"reset_count"`
}

type NodeStorageFilesystem struct {
	NodeName       string    `json:"node_name"`
	Available      bool      `json:"available"`
	TotalBytes     int64     `json:"total_bytes"`
	UsedBytes      int64     `json:"used_bytes"`
	AvailableBytes int64     `json:"available_bytes"`
	MeasuredAt     time.Time `json:"measured_at,omitempty"`
	Error          string    `json:"error,omitempty"`
}

type SessionStorageMeasurement struct {
	StorageID      string    `json:"storage_id"`
	PVCName        string    `json:"pvc_name"`
	NodeName       string    `json:"node_name"`
	AllocatedBytes int64     `json:"allocated_bytes"`
	FileCount      int64     `json:"file_count"`
	DirectoryCount int64     `json:"directory_count"`
	MeasuredAt     time.Time `json:"measured_at"`
}

type SessionStorageView struct {
	StorageID          string     `json:"storage_id"`
	SessionID          string     `json:"session_id"`
	UserID             string     `json:"user_id"`
	PVCNamespace       string     `json:"pvc_namespace"`
	PVCName            string     `json:"pvc_name"`
	PVCPhase           string     `json:"pvc_phase"`
	NodeName           string     `json:"node_name"`
	Generation         int64      `json:"generation"`
	RequestedBytes     int64      `json:"requested_bytes"`
	SoftCapacity       bool       `json:"soft_capacity"`
	LastResetReason    string     `json:"last_reset_reason,omitempty"`
	LastResetAt        *time.Time `json:"last_reset_at,omitempty"`
	ConversationExists bool       `json:"conversation_exists"`
	DeleteAllowed      bool       `json:"delete_allowed"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

type postgresSessionStorageMonitor struct {
	pool           *pgxpool.Pool
	kube           *kubeClient
	namespace      string
	storageClass   string
	storageRoot    string
	probeNamespace string
}

func NewSessionStorageMonitorFromEnv(ctx context.Context) (SessionStorageMonitor, error) {
	if strings.ToLower(strings.TrimSpace(os.Getenv("COCOLA_CLUSTER_MANAGER_MODE"))) != "k3s" {
		return nil, nil
	}
	dsn := strings.TrimSpace(os.Getenv("COCOLA_PG_DSN"))
	if dsn == "" {
		return nil, errors.New("session storage monitor: COCOLA_PG_DSN is required")
	}
	cfg, ok, err := kubeConfigFromEnv()
	if err != nil || !ok {
		if err == nil {
			err = errors.New("Kubernetes configuration is required")
		}
		return nil, err
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	namespace := cfg.SandboxNamespace
	storageClass := strings.TrimSpace(os.Getenv("COCOLA_SESSION_STORAGE_CLASS"))
	if storageClass == "" {
		storageClass = "cocola-local-session"
	}
	storageRoot := strings.TrimSpace(os.Getenv("COCOLA_SESSION_STORAGE_ROOT"))
	if storageRoot == "" {
		storageRoot = "/var/lib/cocola/storage"
	}
	probeNamespace := strings.TrimSpace(os.Getenv("COCOLA_STORAGE_PROBE_NAMESPACE"))
	if probeNamespace == "" {
		probeNamespace = namespace
	}
	return &postgresSessionStorageMonitor{
		pool: pool, kube: newKubeClient(cfg), namespace: namespace,
		storageClass: storageClass, storageRoot: storageRoot, probeNamespace: probeNamespace,
	}, nil
}

func (m *postgresSessionStorageMonitor) Close() { m.pool.Close() }

func (m *postgresSessionStorageMonitor) List(ctx context.Context) ([]SessionStorageView, error) {
	pvcs, err := m.kube.listSessionPVCs(ctx, m.namespace)
	if err != nil {
		return nil, err
	}
	pvcByName := make(map[string]sessionPVC, len(pvcs))
	for _, pvc := range pvcs {
		pvcByName[pvc.Name] = pvc
	}

	rows, err := m.pool.Query(ctx, `
SELECT s.storage_id::text, s.session_id, s.user_id, s.pvc_namespace, s.pvc_name,
       s.node_name, s.generation, s.requested_bytes,
       COALESCE(s.last_reset_reason,''), s.last_reset_at,
       EXISTS (SELECT 1 FROM conversations c WHERE c.id=s.session_id AND c.user_id=s.user_id),
       s.created_at, s.updated_at
FROM session_storage s
ORDER BY s.updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]SessionStorageView, 0)
	bindings := map[string]SessionStorageView{}
	for rows.Next() {
		var item SessionStorageView
		if err := rows.Scan(
			&item.StorageID, &item.SessionID, &item.UserID, &item.PVCNamespace,
			&item.PVCName, &item.NodeName, &item.Generation, &item.RequestedBytes,
			&item.LastResetReason, &item.LastResetAt, &item.ConversationExists,
			&item.CreatedAt, &item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if pvc, ok := pvcByName[item.PVCName]; ok {
			item.PVCPhase = pvc.Phase
			delete(pvcByName, item.PVCName)
		} else {
			item.PVCPhase = "Missing"
		}
		item.SoftCapacity = true
		item.DeleteAllowed = !item.ConversationExists
		bindings[item.StorageID] = item
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// A reset can switch the database to a new generation before Kubernetes
	// accepts deletion of the old local PVC. Surface every remaining managed PVC
	// as an explicit orphan so operators can retry cleanup without a worker.
	orphans := make([]SessionStorageView, 0, len(pvcByName))
	for _, pvc := range pvcByName {
		if pvc.StorageID == "" {
			continue
		}
		namespace := pvc.Namespace
		if namespace == "" {
			namespace = m.namespace
		}
		binding, found := bindings[pvc.StorageID]
		deleteAllowed := !found || !binding.ConversationExists ||
			(pvc.Generation > 0 && pvc.Generation < binding.Generation)
		orphans = append(orphans, SessionStorageView{
			StorageID: pvc.StorageID, PVCNamespace: namespace, PVCName: pvc.Name,
			PVCPhase: pvc.Phase, NodeName: pvc.NodeName, Generation: pvc.Generation,
			RequestedBytes: pvc.RequestedBytes, SoftCapacity: true,
			ConversationExists: found && binding.ConversationExists,
			DeleteAllowed:      deleteAllowed,
		})
	}
	sort.Slice(orphans, func(i, j int) bool { return orphans[i].PVCName < orphans[j].PVCName })
	return append(out, orphans...), nil
}

func (m *postgresSessionStorageMonitor) NodeUsage(ctx context.Context) (map[string]NodeStorageUsage, error) {
	rows, err := m.pool.Query(ctx, `
SELECT node_name, COUNT(*)::int, COALESCE(SUM(requested_bytes),0)::bigint,
	   COALESCE(SUM(GREATEST(generation - 1, 0)),0)::int
FROM session_storage GROUP BY node_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]NodeStorageUsage{}
	for rows.Next() {
		var node string
		var usage NodeStorageUsage
		if err := rows.Scan(&node, &usage.SessionCount, &usage.RequestedBytes, &usage.ResetCount); err != nil {
			return nil, err
		}
		out[node] = usage
	}
	return out, rows.Err()
}

const storageProbeSelector = "app.kubernetes.io/name=cocola-storage-probe"

const storageProbeFilesystemTimeout = 3 * time.Second

func (m *postgresSessionStorageMonitor) NodeFilesystems(ctx context.Context) ([]NodeStorageFilesystem, error) {
	nodes, err := m.kube.listNodes(ctx)
	if err != nil {
		return nil, err
	}
	pods, err := m.kube.listPods(ctx, m.probeNamespace, storageProbeSelector)
	if err != nil {
		return nil, err
	}
	probeByNode := make(map[string]string, len(pods))
	for _, pod := range pods {
		if pod.Spec.NodeName != "" && pod.Status.Phase == "Running" && pod.Metadata.DeletionTimestamp == nil {
			probeByNode[pod.Spec.NodeName] = pod.Metadata.Name
		}
	}
	out := make([]NodeStorageFilesystem, 0, len(nodes))
	for _, node := range nodes {
		item := NodeStorageFilesystem{NodeName: node.Metadata.Name}
		podName := probeByNode[item.NodeName]
		if podName == "" {
			item.Error = "storage probe is not ready"
			out = append(out, item)
			continue
		}
		probeCtx, cancel := context.WithTimeout(ctx, storageProbeFilesystemTimeout)
		measurement, measureErr := m.kube.storageProbeFilesystem(probeCtx, m.probeNamespace, podName)
		cancel()
		if measureErr != nil || (measurement.NodeName != "" && measurement.NodeName != item.NodeName) {
			item.Error = "storage probe measurement failed"
			out = append(out, item)
			continue
		}
		item.Available = true
		item.TotalBytes = measurement.TotalBytes
		item.UsedBytes = measurement.UsedBytes
		item.AvailableBytes = measurement.AvailableBytes
		item.MeasuredAt = measurement.MeasuredAt
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].NodeName < out[j].NodeName })
	return out, nil
}

func (m *postgresSessionStorageMonitor) Measure(ctx context.Context, storageID, pvcName string) (SessionStorageMeasurement, error) {
	storageID = strings.TrimSpace(storageID)
	if storageID == "" {
		return SessionStorageMeasurement{}, ErrInvalidArg
	}
	var namespace, boundPVC string
	err := m.pool.QueryRow(ctx, `
SELECT pvc_namespace, pvc_name FROM session_storage WHERE storage_id::text=$1`, storageID).Scan(&namespace, &boundPVC)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return SessionStorageMeasurement{}, err
	}
	targetPVC := strings.TrimSpace(pvcName)
	if targetPVC == "" {
		targetPVC = boundPVC
	}
	if targetPVC == "" {
		return SessionStorageMeasurement{}, ErrNotFound
	}
	if namespace == "" {
		namespace = m.namespace
	}
	pvc, exists, err := m.kube.getSessionPVC(ctx, namespace, targetPVC)
	if err != nil {
		return SessionStorageMeasurement{}, err
	}
	if !exists {
		return SessionStorageMeasurement{}, ErrNotFound
	}
	if pvc.StorageID != storageID {
		return SessionStorageMeasurement{}, ErrConflict
	}
	if pvc.Phase != "Bound" || pvc.VolumeName == "" || pvc.NodeName == "" {
		return SessionStorageMeasurement{}, fmt.Errorf("%w: session volume is not bound", ErrStorageUnavailable)
	}
	if pvc.StorageClass != m.storageClass {
		return SessionStorageMeasurement{}, fmt.Errorf("%w: storage class %q", ErrStorageUnsupported, pvc.StorageClass)
	}
	pv, err := m.kube.getSessionPV(ctx, pvc.VolumeName)
	if err != nil {
		return SessionStorageMeasurement{}, err
	}
	if pv.StorageClass != m.storageClass || strings.TrimSpace(pv.LocalPath) == "" {
		return SessionStorageMeasurement{}, fmt.Errorf("%w: persistent volume backend", ErrStorageUnsupported)
	}
	relativePath, err := relativeStoragePath(m.storageRoot, pv.LocalPath)
	if err != nil {
		return SessionStorageMeasurement{}, fmt.Errorf("%w: persistent volume path", ErrStorageUnsupported)
	}
	pods, err := m.kube.listPods(ctx, m.probeNamespace, storageProbeSelector)
	if err != nil {
		return SessionStorageMeasurement{}, err
	}
	probeName := ""
	for _, pod := range pods {
		if pod.Spec.NodeName == pvc.NodeName && pod.Status.Phase == "Running" && pod.Metadata.DeletionTimestamp == nil {
			probeName = pod.Metadata.Name
			break
		}
	}
	if probeName == "" {
		return SessionStorageMeasurement{}, fmt.Errorf("%w: node probe is not ready", ErrStorageUnavailable)
	}
	measurement, err := m.kube.storageProbeUsage(ctx, m.probeNamespace, probeName, relativePath)
	if err != nil {
		return SessionStorageMeasurement{}, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}
	if measurement.NodeName != "" && measurement.NodeName != pvc.NodeName {
		return SessionStorageMeasurement{}, fmt.Errorf("%w: node identity mismatch", ErrStorageUnavailable)
	}
	return SessionStorageMeasurement{
		StorageID: storageID, PVCName: targetPVC, NodeName: pvc.NodeName,
		AllocatedBytes: measurement.AllocatedBytes, FileCount: measurement.FileCount,
		DirectoryCount: measurement.DirectoryCount, MeasuredAt: measurement.MeasuredAt,
	}, nil
}

func relativeStoragePath(root, target string) (string, error) {
	root = filepath.Clean(strings.TrimSpace(root))
	target = filepath.Clean(strings.TrimSpace(target))
	if !filepath.IsAbs(root) || !filepath.IsAbs(target) {
		return "", ErrInvalidArg
	}
	relative, err := filepath.Rel(root, target)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", ErrInvalidArg
	}
	return relative, nil
}

func (m *postgresSessionStorageMonitor) DeleteOrphan(ctx context.Context, storageID, pvcName string) error {
	var namespace, name string
	var generation int64
	var conversationExists bool
	err := m.pool.QueryRow(ctx, `
SELECT s.pvc_namespace, s.pvc_name, s.generation,
       EXISTS (SELECT 1 FROM conversations c WHERE c.id=s.session_id AND c.user_id=s.user_id)
FROM session_storage s WHERE s.storage_id=$1::uuid`, storageID).Scan(
		&namespace, &name, &generation, &conversationExists,
	)
	foundBinding := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	targetName := strings.TrimSpace(pvcName)
	if targetName == "" {
		if !foundBinding {
			return ErrNotFound
		}
		targetName = name
	}
	if foundBinding && targetName == name && conversationExists {
		return ErrConflict
	}
	if namespace == "" {
		namespace = m.namespace
	}
	pvc, exists, err := m.kube.getSessionPVC(ctx, namespace, targetName)
	if err != nil {
		return err
	}
	if exists {
		if pvc.StorageID != storageID {
			return ErrConflict
		}
		if foundBinding && conversationExists && targetName != name &&
			(pvc.Generation <= 0 || pvc.Generation >= generation) {
			return ErrConflict
		}
		if err := m.kube.deletePVC(ctx, namespace, targetName); err != nil {
			return err
		}
	}
	if foundBinding && targetName == name {
		_, err = m.pool.Exec(ctx, `DELETE FROM session_storage WHERE storage_id=$1::uuid`, storageID)
		return err
	}
	return nil
}

func (a *Admin) WithSessionStorageMonitor(m SessionStorageMonitor) *Admin {
	a.sessionStorage = m
	return a
}

func (a *Admin) ListSessionStorage(ctx context.Context) ([]SessionStorageView, error) {
	if a.sessionStorage == nil {
		return nil, ErrNotConfigured
	}
	return a.sessionStorage.List(ctx)
}

func (a *Admin) ListNodeStorageFilesystems(ctx context.Context) ([]NodeStorageFilesystem, error) {
	if a.sessionStorage == nil {
		return nil, ErrNotConfigured
	}
	return a.sessionStorage.NodeFilesystems(ctx)
}

func (a *Admin) MeasureSessionStorage(ctx context.Context, storageID, pvcName string) (SessionStorageMeasurement, error) {
	if a.sessionStorage == nil {
		return SessionStorageMeasurement{}, ErrNotConfigured
	}
	return a.sessionStorage.Measure(ctx, storageID, pvcName)
}

func (a *Admin) DeleteOrphanSessionStorage(ctx context.Context, storageID, pvcName string) error {
	if a.sessionStorage == nil {
		return ErrNotConfigured
	}
	if strings.TrimSpace(storageID) == "" {
		return ErrInvalidArg
	}
	return a.sessionStorage.DeleteOrphan(ctx, storageID, pvcName)
}
