package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	pathpkg "path"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"k8s.io/apimachinery/pkg/api/resource"
)

type hostSessionStorageMonitor struct {
	pool           *pgxpool.Pool
	host           *hostAgentClient
	nodeName       string
	requestedBytes int64
}

type hostConversation struct {
	SessionID string
	UserID    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

func newHostSessionStorageMonitor(
	ctx context.Context,
	dsn string,
	host *hostAgentClient,
) (*hostSessionStorageMonitor, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	requestedBytes := int64(2 << 30)
	if quantity, err := resource.ParseQuantity(strings.TrimSpace(getenvFirst("COCOLA_SESSION_VOLUME_SIZE"))); err == nil && quantity.Value() > 0 {
		requestedBytes = quantity.Value()
	}
	nodeName := strings.TrimSpace(getenvFirst("COCOLA_NODE_NAME"))
	if nodeName == "" {
		nodeName = "local"
	}
	return &hostSessionStorageMonitor{
		pool: pool, host: host, nodeName: nodeName, requestedBytes: requestedBytes,
	}, nil
}

func (m *hostSessionStorageMonitor) Close() { m.pool.Close() }

func (m *hostSessionStorageMonitor) List(ctx context.Context) ([]SessionStorageView, error) {
	roots, err := m.host.SessionRoots(ctx)
	if err != nil {
		return nil, fmt.Errorf("list host session roots: %w", err)
	}
	conversations, err := m.conversations(ctx)
	if err != nil {
		return nil, fmt.Errorf("query session conversations: %w", err)
	}
	rootByPath := make(map[string]hostAgentSessionRoot, len(roots))
	for _, root := range roots {
		rootByPath[pathpkg.Clean(root.Path)] = root
	}
	out := make([]SessionStorageView, 0, len(roots))
	for _, conversation := range conversations {
		path := hostSessionPath(conversation.UserID, conversation.SessionID)
		_, ok := rootByPath[path]
		if !ok {
			continue
		}
		delete(rootByPath, path)
		out = append(out, SessionStorageView{
			StorageID: hostStorageID(path), SessionID: conversation.SessionID, UserID: conversation.UserID,
			PVCName: path, PVCPhase: "Mounted", NodeName: m.nodeName, Generation: 1,
			RequestedBytes: m.requestedBytes, SoftCapacity: true, ConversationExists: true,
			DeleteAllowed: false, CreatedAt: conversation.CreatedAt, UpdatedAt: conversation.UpdatedAt,
			LastResetAt: nil,
		})
	}
	for path, root := range rootByPath {
		out = append(out, SessionStorageView{
			StorageID: hostStorageID(path), PVCName: path, PVCPhase: "Orphaned",
			NodeName: m.nodeName, Generation: 1, RequestedBytes: m.requestedBytes,
			SoftCapacity: true, ConversationExists: false, DeleteAllowed: true,
			CreatedAt: root.ModifiedAt, UpdatedAt: root.ModifiedAt,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out, nil
}

func (m *hostSessionStorageMonitor) NodeUsage(ctx context.Context) (map[string]NodeStorageUsage, error) {
	items, err := m.List(ctx)
	if err != nil {
		return nil, err
	}
	usage := NodeStorageUsage{}
	for _, item := range items {
		usage.SessionCount++
		usage.RequestedBytes += item.RequestedBytes
	}
	return map[string]NodeStorageUsage{m.nodeName: usage}, nil
}

func (m *hostSessionStorageMonitor) NodeFilesystems(ctx context.Context) ([]NodeStorageFilesystem, error) {
	filesystem, err := m.host.Filesystem(ctx)
	if err != nil {
		return []NodeStorageFilesystem{{NodeName: m.nodeName, Error: "host storage probe is unavailable"}}, nil
	}
	name := strings.TrimSpace(filesystem.NodeName)
	if name == "" {
		name = m.nodeName
	}
	return []NodeStorageFilesystem{{
		NodeName: name, Available: true, TotalBytes: filesystem.TotalBytes,
		UsedBytes: filesystem.UsedBytes, AvailableBytes: filesystem.AvailableBytes,
		MeasuredAt: filesystem.MeasuredAt,
	}}, nil
}

func (m *hostSessionStorageMonitor) Measure(
	ctx context.Context,
	storageID, path string,
) (SessionStorageMeasurement, error) {
	path = pathpkg.Clean(strings.TrimSpace(path))
	if !validHostSessionPath(path) || hostStorageID(path) != strings.TrimSpace(storageID) {
		return SessionStorageMeasurement{}, ErrInvalidArg
	}
	usage, err := m.host.Usage(ctx, path)
	if err != nil {
		return SessionStorageMeasurement{}, mapHostStorageError(err)
	}
	return SessionStorageMeasurement{
		StorageID: storageID, PVCName: path, NodeName: m.nodeName,
		AllocatedBytes: usage.AllocatedBytes, FileCount: usage.FileCount,
		DirectoryCount: usage.DirectoryCount, MeasuredAt: usage.MeasuredAt,
	}, nil
}

func (m *hostSessionStorageMonitor) DeleteOrphan(ctx context.Context, storageID, path string) error {
	path = pathpkg.Clean(strings.TrimSpace(path))
	if !validHostSessionPath(path) || hostStorageID(path) != strings.TrimSpace(storageID) {
		return ErrInvalidArg
	}
	conversations, err := m.conversations(ctx)
	if err != nil {
		return err
	}
	for _, conversation := range conversations {
		if hostSessionPath(conversation.UserID, conversation.SessionID) == path {
			return ErrConflict
		}
	}
	if err := m.host.DeleteSessionRoot(ctx, path); err != nil {
		return mapHostStorageError(err)
	}
	return nil
}

func (m *hostSessionStorageMonitor) ListWorkspaceEntries(
	ctx context.Context,
	userID, sessionID, relativePath, cursor string,
) (WorkspaceEntries, error) {
	cleanPath, err := cleanWorkspaceRequestPath(relativePath, true)
	if err != nil || len(cursor) > 2048 {
		return WorkspaceEntries{}, ErrInvalidArg
	}
	root, err := m.workspaceRoot(ctx, userID, sessionID)
	if err != nil {
		return WorkspaceEntries{}, err
	}
	result, err := m.host.WorkspaceEntries(ctx, root, cleanPath, strings.TrimSpace(cursor))
	if err != nil {
		return WorkspaceEntries{}, mapHostWorkspaceError(err)
	}
	entries := make([]WorkspaceEntry, 0, len(result.Entries))
	for _, entry := range result.Entries {
		entries = append(entries, WorkspaceEntry{
			Name: entry.Name, Path: entry.Path, Kind: entry.Kind, Size: entry.Size,
			ModifiedAt: entry.ModifiedAt, Previewable: entry.Previewable, PreviewKind: entry.PreviewKind,
		})
	}
	return WorkspaceEntries{Path: result.Path, Entries: entries, NextCursor: result.NextCursor}, nil
}

func (m *hostSessionStorageMonitor) ReadWorkspaceFile(
	ctx context.Context,
	userID, sessionID, relativePath string,
) (WorkspaceFile, error) {
	cleanPath, err := cleanWorkspaceRequestPath(relativePath, false)
	if err != nil {
		return WorkspaceFile{}, ErrInvalidArg
	}
	root, err := m.workspaceRoot(ctx, userID, sessionID)
	if err != nil {
		return WorkspaceFile{}, err
	}
	data, contentType, err := m.host.WorkspaceFile(ctx, root, cleanPath)
	if err != nil {
		return WorkspaceFile{}, mapHostWorkspaceError(err)
	}
	if strings.TrimSpace(contentType) == "" {
		contentType = "application/octet-stream"
	}
	return WorkspaceFile{Data: data, ContentType: contentType}, nil
}

func (m *hostSessionStorageMonitor) workspaceRoot(ctx context.Context, userID, sessionID string) (string, error) {
	userID = strings.TrimSpace(userID)
	sessionID = strings.TrimSpace(sessionID)
	if userID == "" || sessionID == "" {
		return "", ErrInvalidArg
	}
	var exists bool
	err := m.pool.QueryRow(ctx, `
SELECT EXISTS (SELECT 1 FROM conversations WHERE id=$1 AND user_id=$2)`, sessionID, userID).Scan(&exists)
	if err != nil {
		return "", err
	}
	if !exists {
		return "", ErrWorkspaceNotFound
	}
	return pathpkg.Join(hostSessionPath(userID, sessionID), "workspace"), nil
}

func (m *hostSessionStorageMonitor) conversations(ctx context.Context) ([]hostConversation, error) {
	rows, err := m.pool.Query(ctx, `
SELECT id, user_id, created_at, updated_at FROM conversations ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]hostConversation, 0)
	for rows.Next() {
		var item hostConversation
		if err := rows.Scan(&item.SessionID, &item.UserID, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func hostSessionPath(userID, sessionID string) string {
	return pathpkg.Join("users", hostSafePathSegment(userID), "sessions", hostSafePathSegment(sessionID))
}

func hostStorageID(path string) string {
	digest := sha256.Sum256([]byte("host:" + pathpkg.Clean(path)))
	return "host-" + hex.EncodeToString(digest[:])[:24]
}

// hostSafePathSegment mirrors the durable host-volume contract owned by the
// OpenSandbox provider. Changing it requires a storage migration.
func hostSafePathSegment(value string) string {
	digest := sha256.Sum256([]byte(value))
	hash := hex.EncodeToString(digest[:])[:12]
	var builder strings.Builder
	previousDash := false
	for _, char := range strings.ToLower(value) {
		valid := (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9')
		if valid {
			builder.WriteRune(char)
			previousDash = false
			continue
		}
		if !previousDash {
			builder.WriteByte('-')
			previousDash = true
		}
	}
	base := strings.Trim(builder.String(), "-")
	if base == "" {
		base = "x"
	}
	if len(base) > 80 {
		base = strings.Trim(base[:80], "-")
		if base == "" {
			base = "x"
		}
	}
	return base + "-" + hash
}

func validHostSessionPath(path string) bool {
	parts := strings.Split(pathpkg.Clean(path), "/")
	return len(parts) == 4 && parts[0] == "users" && parts[1] != "" &&
		parts[2] == "sessions" && parts[3] != "" && parts[1] != "." &&
		parts[1] != ".." && parts[3] != "." && parts[3] != ".."
}

func mapHostStorageError(err error) error {
	var statusErr *hostAgentStatusError
	if errors.As(err, &statusErr) {
		switch statusErr.StatusCode {
		case 400:
			return ErrInvalidArg
		case 404:
			return ErrNotFound
		case 409:
			return ErrConflict
		default:
			return fmt.Errorf("%w: host agent status %d", ErrStorageUnavailable, statusErr.StatusCode)
		}
	}
	return fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
}

func mapHostWorkspaceError(err error) error {
	var statusErr *hostAgentStatusError
	if errors.As(err, &statusErr) {
		switch statusErr.StatusCode {
		case 400:
			return ErrInvalidArg
		case 404:
			return ErrWorkspaceNotFound
		case 413:
			return ErrWorkspaceFileTooLarge
		case 415:
			return ErrWorkspacePreviewUnsupported
		case 422:
			return ErrWorkspaceDirectoryTooLarge
		case 429:
			return ErrTooManyRequests
		default:
			return ErrWorkspaceNodeUnavailable
		}
	}
	return ErrWorkspaceNodeUnavailable
}

var _ SessionStorageMonitor = (*hostSessionStorageMonitor)(nil)
var _ WorkspaceBrowser = (*hostSessionStorageMonitor)(nil)
