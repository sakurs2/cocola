// Package wiki owns the durable, user-scoped Wiki tree and immutable file
// versions. File bytes live in the configured object store; PostgreSQL is the
// authoritative metadata and ownership index.
package wiki

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

var (
	ErrNotFound         = errors.New("wiki: not found")
	ErrNameConflict     = errors.New("wiki: sibling name conflict")
	ErrInvalidName      = errors.New("wiki: invalid name")
	ErrInvalidParent    = errors.New("wiki: invalid parent")
	ErrMoveCycle        = errors.New("wiki: move would create a cycle")
	ErrRevisionConflict = errors.New("wiki: revision conflict")
	ErrNotMarkdown      = errors.New("wiki: file is not editable markdown")
)

type Identity struct {
	TenantID string
	UserID   string
}

type Node struct {
	ID               string    `json:"id"`
	ParentID         string    `json:"parent_id,omitempty"`
	Kind             string    `json:"kind"`
	Name             string    `json:"name"`
	Extension        string    `json:"extension,omitempty"`
	MimeType         string    `json:"mime_type,omitempty"`
	CurrentVersionID string    `json:"current_version_id,omitempty"`
	Revision         int64     `json:"revision,omitempty"`
	SizeBytes        int64     `json:"size_bytes,omitempty"`
	SHA256           string    `json:"sha256,omitempty"`
	LogicalPath      string    `json:"logical_path,omitempty"`
	SortOrder        int64     `json:"sort_order"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type Version struct {
	ID          string    `json:"id"`
	NodeID      string    `json:"node_id"`
	Revision    int64     `json:"revision"`
	ObjectKey   string    `json:"-"`
	SizeBytes   int64     `json:"size_bytes"`
	SHA256      string    `json:"sha256"`
	MimeType    string    `json:"mime_type"`
	CreatedAt   time.Time `json:"created_at"`
	NodeName    string    `json:"filename,omitempty"`
	Extension   string    `json:"extension,omitempty"`
	LogicalPath string    `json:"logical_path,omitempty"`
}

type CreateFileInput struct {
	Node    Node
	Version Version
}

type Store interface {
	List(ctx context.Context, identity Identity) ([]Node, error)
	CreateFolder(ctx context.Context, identity Identity, node Node) (Node, error)
	CreateFile(ctx context.Context, identity Identity, input CreateFileInput) (Node, error)
	GetCurrent(ctx context.Context, identity Identity, nodeID string) (Node, Version, error)
	GetVersion(ctx context.Context, identity Identity, versionID string) (Node, Version, error)
	SaveVersion(
		ctx context.Context,
		identity Identity,
		nodeID string,
		expectedRevision int64,
		version Version,
		updatedAt time.Time,
	) (Node, error)
	Rename(ctx context.Context, identity Identity, nodeID, name string, updatedAt time.Time) (Node, error)
	Move(ctx context.Context, identity Identity, nodeID, parentID string, updatedAt time.Time) (Node, error)
	Delete(ctx context.Context, identity Identity, nodeID string, deletedAt time.Time) error
	ResolveCurrent(ctx context.Context, identity Identity, nodeIDs []string) ([]Node, []Version, error)
}

func NormalizeName(value string) (string, error) {
	name := strings.TrimSpace(value)
	if name == "" || name == "." || name == ".." || utf8.RuneCountInString(name) > 160 {
		return "", ErrInvalidName
	}
	for _, char := range name {
		if char == '/' || char == '\\' || char == 0 || unicode.IsControl(char) {
			return "", ErrInvalidName
		}
	}
	return name, nil
}

// PopulateLogicalPaths fills the stable display/materialization path for a
// flat owner-scoped tree. Invalid/cyclic parent data degrades to the node name
// rather than making the whole Wiki unreadable.
func PopulateLogicalPaths(nodes []Node) []Node {
	byID := make(map[string]Node, len(nodes))
	for _, node := range nodes {
		byID[node.ID] = node
	}
	memo := make(map[string]string, len(nodes))
	var build func(string, map[string]bool) string
	build = func(id string, seen map[string]bool) string {
		if cached, ok := memo[id]; ok {
			return cached
		}
		node, ok := byID[id]
		if !ok {
			return ""
		}
		if seen[id] {
			return node.Name
		}
		seen[id] = true
		path := node.Name
		if node.ParentID != "" {
			if parent := build(node.ParentID, seen); parent != "" {
				path = parent + "/" + node.Name
			}
		}
		delete(seen, id)
		memo[id] = path
		return path
	}
	out := append([]Node(nil), nodes...)
	for index := range out {
		out[index].LogicalPath = build(out[index].ID, make(map[string]bool))
	}
	return out
}
