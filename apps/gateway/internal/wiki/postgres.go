package wiki

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Postgres struct {
	pool *pgxpool.Pool
}

var _ Store = (*Postgres)(nil)

func NewPostgres(ctx context.Context, dsn string) (*Postgres, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &Postgres{pool: pool}, nil
}

func (p *Postgres) Close() { p.pool.Close() }

const nodeColumns = `n.id::text, COALESCE(n.parent_id::text, ''), n.kind, n.name,
	n.extension, n.mime_type, COALESCE(n.current_version_id::text, ''),
	COALESCE(v.revision, 0), COALESCE(v.size_bytes, 0), COALESCE(v.sha256, ''),
	n.sort_order, n.created_at, n.updated_at`

func scanNode(row pgx.Row) (Node, error) {
	var node Node
	err := row.Scan(
		&node.ID, &node.ParentID, &node.Kind, &node.Name, &node.Extension,
		&node.MimeType, &node.CurrentVersionID, &node.Revision, &node.SizeBytes,
		&node.SHA256, &node.SortOrder, &node.CreatedAt, &node.UpdatedAt,
	)
	return node, err
}

func (p *Postgres) List(ctx context.Context, identity Identity) ([]Node, error) {
	query := `SELECT ` + nodeColumns + `
		FROM wiki_nodes n
		LEFT JOIN wiki_versions v ON v.id=n.current_version_id
		WHERE n.tenant_id=$1 AND n.user_id=$2 AND n.deleted_at IS NULL
		ORDER BY n.sort_order, LOWER(n.name), n.id`
	rows, err := p.pool.Query(ctx, query, identity.TenantID, identity.UserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	nodes := make([]Node, 0)
	for rows.Next() {
		node, scanErr := scanNode(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		nodes = append(nodes, node)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return PopulateLogicalPaths(nodes), nil
}

func (p *Postgres) CreateFolder(
	ctx context.Context,
	identity Identity,
	node Node,
) (Node, error) {
	query := `INSERT INTO wiki_nodes
		(id, tenant_id, user_id, parent_id, kind, name, sort_order, created_at, updated_at)
		SELECT $1::uuid,$2,$3,NULLIF($4,'')::uuid,'folder',$5,$6,$7,$7
		WHERE $4='' OR EXISTS (
			SELECT 1 FROM wiki_nodes
			WHERE id=$4::uuid AND tenant_id=$2 AND user_id=$3
				AND kind='folder' AND deleted_at IS NULL
		)
		RETURNING id::text, COALESCE(parent_id::text, ''), kind, name, extension,
			mime_type, '', 0::bigint, 0::bigint, '', sort_order, created_at, updated_at`
	created, err := scanNode(p.pool.QueryRow(
		ctx, query, node.ID, identity.TenantID, identity.UserID, node.ParentID,
		node.Name, node.SortOrder, node.CreatedAt,
	))
	if err == pgx.ErrNoRows {
		return Node{}, ErrInvalidParent
	}
	return created, mapPostgresError(err)
}

func (p *Postgres) CreateFile(
	ctx context.Context,
	identity Identity,
	input CreateFileInput,
) (Node, error) {
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Node{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	node := input.Node
	version := input.Version
	tag, err := tx.Exec(ctx, `INSERT INTO wiki_nodes
		(id, tenant_id, user_id, parent_id, kind, name, extension, mime_type, sort_order, created_at, updated_at)
		SELECT $1::uuid,$2,$3,NULLIF($4,'')::uuid,'file',$5,$6,$7,$8,$9,$9
		WHERE $4='' OR EXISTS (
			SELECT 1 FROM wiki_nodes
			WHERE id=$4::uuid AND tenant_id=$2 AND user_id=$3
				AND kind='folder' AND deleted_at IS NULL
		)`,
		node.ID, identity.TenantID, identity.UserID, node.ParentID, node.Name,
		node.Extension, node.MimeType, node.SortOrder, node.CreatedAt,
	)
	if err != nil {
		return Node{}, mapPostgresError(err)
	}
	if tag.RowsAffected() == 0 {
		return Node{}, ErrInvalidParent
	}
	_, err = tx.Exec(ctx, `INSERT INTO wiki_versions
		(id,node_id,revision,object_key,size_bytes,sha256,mime_type,created_at)
		VALUES ($1::uuid,$2::uuid,1,$3,$4,$5,$6,$7)`,
		version.ID, node.ID, version.ObjectKey, version.SizeBytes, version.SHA256,
		version.MimeType, version.CreatedAt,
	)
	if err != nil {
		return Node{}, mapPostgresError(err)
	}
	_, err = tx.Exec(ctx, `UPDATE wiki_nodes SET current_version_id=$2::uuid WHERE id=$1::uuid`,
		node.ID, version.ID)
	if err != nil {
		return Node{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Node{}, err
	}
	node.CurrentVersionID = version.ID
	node.Revision = 1
	node.SizeBytes = version.SizeBytes
	node.SHA256 = version.SHA256
	node.MimeType = version.MimeType
	return node, nil
}

func (p *Postgres) currentNode(ctx context.Context, identity Identity, nodeID string) (Node, error) {
	query := `SELECT ` + nodeColumns + `
		FROM wiki_nodes n
		LEFT JOIN wiki_versions v ON v.id=n.current_version_id
		WHERE n.id=$3::uuid AND n.tenant_id=$1 AND n.user_id=$2 AND n.deleted_at IS NULL`
	node, err := scanNode(p.pool.QueryRow(ctx, query, identity.TenantID, identity.UserID, nodeID))
	if err == pgx.ErrNoRows {
		return Node{}, ErrNotFound
	}
	return node, err
}

func (p *Postgres) GetCurrent(
	ctx context.Context,
	identity Identity,
	nodeID string,
) (Node, Version, error) {
	node, err := p.currentNode(ctx, identity, nodeID)
	if err != nil {
		return Node{}, Version{}, err
	}
	if node.Kind != "file" || node.CurrentVersionID == "" {
		return Node{}, Version{}, ErrNotFound
	}
	resolvedNode, version, err := p.GetVersion(ctx, identity, node.CurrentVersionID)
	return resolvedNode, version, err
}

func (p *Postgres) GetVersion(
	ctx context.Context,
	identity Identity,
	versionID string,
) (Node, Version, error) {
	query := `SELECT ` + nodeColumns + `,
		v.id::text, v.node_id::text, v.revision, v.object_key, v.size_bytes,
		v.sha256, v.mime_type, v.created_at
		FROM wiki_versions v
		JOIN wiki_nodes n ON n.id=v.node_id
		WHERE v.id=$3::uuid AND n.tenant_id=$1 AND n.user_id=$2`
	row := p.pool.QueryRow(ctx, query, identity.TenantID, identity.UserID, versionID)
	var node Node
	var version Version
	err := row.Scan(
		&node.ID, &node.ParentID, &node.Kind, &node.Name, &node.Extension,
		&node.MimeType, &node.CurrentVersionID, &node.Revision, &node.SizeBytes,
		&node.SHA256, &node.SortOrder, &node.CreatedAt, &node.UpdatedAt,
		&version.ID, &version.NodeID, &version.Revision, &version.ObjectKey,
		&version.SizeBytes, &version.SHA256, &version.MimeType, &version.CreatedAt,
	)
	if err == pgx.ErrNoRows {
		return Node{}, Version{}, ErrNotFound
	}
	if err != nil {
		return Node{}, Version{}, err
	}
	node = PopulateLogicalPathsForNode(ctx, p, identity, node)
	version.NodeName = node.Name
	version.Extension = node.Extension
	version.LogicalPath = node.LogicalPath
	return node, version, nil
}

func PopulateLogicalPathsForNode(
	ctx context.Context,
	store *Postgres,
	identity Identity,
	node Node,
) Node {
	nodes, err := store.List(ctx, identity)
	if err != nil {
		node.LogicalPath = node.Name
		return node
	}
	for _, item := range nodes {
		if item.ID == node.ID {
			return item
		}
	}
	node.LogicalPath = node.Name
	return node
}

func (p *Postgres) SaveVersion(
	ctx context.Context,
	identity Identity,
	nodeID string,
	expectedRevision int64,
	version Version,
	updatedAt time.Time,
) (Node, error) {
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Node{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	current, err := scanNode(tx.QueryRow(ctx, `SELECT `+nodeColumns+`
		FROM wiki_nodes n JOIN wiki_versions v ON v.id=n.current_version_id
		WHERE n.id=$3::uuid AND n.tenant_id=$1 AND n.user_id=$2
			AND n.deleted_at IS NULL AND n.kind='file' AND n.extension='.md'
		FOR UPDATE OF n`,
		identity.TenantID, identity.UserID, nodeID,
	))
	if err == pgx.ErrNoRows {
		return Node{}, ErrNotMarkdown
	}
	if err != nil {
		return Node{}, err
	}
	if current.Revision != expectedRevision {
		return Node{}, ErrRevisionConflict
	}
	version.Revision = current.Revision + 1
	_, err = tx.Exec(ctx, `INSERT INTO wiki_versions
		(id,node_id,revision,object_key,size_bytes,sha256,mime_type,created_at)
		VALUES ($1::uuid,$2::uuid,$3,$4,$5,$6,$7,$8)`,
		version.ID, nodeID, version.Revision, version.ObjectKey, version.SizeBytes,
		version.SHA256, version.MimeType, version.CreatedAt,
	)
	if err != nil {
		return Node{}, mapPostgresError(err)
	}
	_, err = tx.Exec(ctx, `UPDATE wiki_nodes
		SET current_version_id=$4::uuid, mime_type=$5, updated_at=$6
		WHERE id=$3::uuid AND tenant_id=$1 AND user_id=$2`,
		identity.TenantID, identity.UserID, nodeID, version.ID, version.MimeType, updatedAt,
	)
	if err != nil {
		return Node{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Node{}, err
	}
	current.CurrentVersionID = version.ID
	current.Revision = version.Revision
	current.SizeBytes = version.SizeBytes
	current.SHA256 = version.SHA256
	current.MimeType = version.MimeType
	current.UpdatedAt = updatedAt
	return current, nil
}

func (p *Postgres) Rename(
	ctx context.Context,
	identity Identity,
	nodeID, name string,
	updatedAt time.Time,
) (Node, error) {
	tag, err := p.pool.Exec(ctx, `UPDATE wiki_nodes
		SET name=$4, updated_at=$5
		WHERE id=$3::uuid AND tenant_id=$1 AND user_id=$2 AND deleted_at IS NULL`,
		identity.TenantID, identity.UserID, nodeID, name, updatedAt,
	)
	if err != nil {
		return Node{}, mapPostgresError(err)
	}
	if tag.RowsAffected() == 0 {
		return Node{}, ErrNotFound
	}
	return p.currentNode(ctx, identity, nodeID)
}

func (p *Postgres) Move(
	ctx context.Context,
	identity Identity,
	nodeID, parentID string,
	updatedAt time.Time,
) (Node, error) {
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Node{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(
		ctx,
		`SELECT pg_advisory_xact_lock(hashtext($1), hashtext($2))`,
		identity.TenantID,
		identity.UserID,
	); err != nil {
		return Node{}, err
	}
	if parentID != "" {
		var exists bool
		err = tx.QueryRow(ctx, `SELECT EXISTS(
			SELECT 1 FROM wiki_nodes
			WHERE id=$3::uuid AND tenant_id=$1 AND user_id=$2
				AND kind='folder' AND deleted_at IS NULL
		)`, identity.TenantID, identity.UserID, parentID).Scan(&exists)
		if err != nil {
			return Node{}, err
		}
		if !exists {
			return Node{}, ErrInvalidParent
		}
		var cycle bool
		err = tx.QueryRow(ctx, `WITH RECURSIVE descendants AS (
			SELECT id FROM wiki_nodes
			WHERE id=$3::uuid AND tenant_id=$1 AND user_id=$2 AND deleted_at IS NULL
			UNION
			SELECT child.id FROM wiki_nodes child
			JOIN descendants d ON child.parent_id=d.id
			WHERE child.tenant_id=$1 AND child.user_id=$2 AND child.deleted_at IS NULL
		)
		SELECT EXISTS(SELECT 1 FROM descendants WHERE id=$4::uuid)`,
			identity.TenantID, identity.UserID, nodeID, parentID,
		).Scan(&cycle)
		if err != nil {
			return Node{}, err
		}
		if cycle {
			return Node{}, ErrMoveCycle
		}
	}
	tag, err := tx.Exec(ctx, `UPDATE wiki_nodes
		SET parent_id=NULLIF($4,'')::uuid, updated_at=$5
		WHERE id=$3::uuid AND tenant_id=$1 AND user_id=$2 AND deleted_at IS NULL`,
		identity.TenantID, identity.UserID, nodeID, parentID, updatedAt,
	)
	if err != nil {
		return Node{}, mapPostgresError(err)
	}
	if tag.RowsAffected() == 0 {
		return Node{}, ErrNotFound
	}
	if err := tx.Commit(ctx); err != nil {
		return Node{}, err
	}
	return p.currentNode(ctx, identity, nodeID)
}

func (p *Postgres) Delete(
	ctx context.Context,
	identity Identity,
	nodeID string,
	deletedAt time.Time,
) error {
	tag, err := p.pool.Exec(ctx, `WITH RECURSIVE subtree AS (
		SELECT id FROM wiki_nodes
		WHERE id=$3::uuid AND tenant_id=$1 AND user_id=$2 AND deleted_at IS NULL
		UNION
		SELECT child.id FROM wiki_nodes child
		JOIN subtree parent ON child.parent_id=parent.id
		WHERE child.tenant_id=$1 AND child.user_id=$2 AND child.deleted_at IS NULL
	)
	UPDATE wiki_nodes SET deleted_at=$4, updated_at=$4
	WHERE id IN (SELECT id FROM subtree)`,
		identity.TenantID, identity.UserID, nodeID, deletedAt,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *Postgres) ResolveCurrent(
	ctx context.Context,
	identity Identity,
	nodeIDs []string,
) ([]Node, []Version, error) {
	nodes := make([]Node, 0, len(nodeIDs))
	versions := make([]Version, 0, len(nodeIDs))
	for _, nodeID := range nodeIDs {
		node, version, err := p.GetCurrent(ctx, identity, nodeID)
		if err != nil {
			return nil, nil, err
		}
		nodes = append(nodes, node)
		versions = append(versions, version)
	}
	return nodes, versions, nil
}

func mapPostgresError(err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrNameConflict
	}
	return err
}
