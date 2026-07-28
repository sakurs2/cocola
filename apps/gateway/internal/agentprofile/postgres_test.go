package agentprofile

import (
	"context"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresArchiveRejectsActiveRegistration(t *testing.T) {
	dsn := os.Getenv("COCOLA_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("COCOLA_TEST_PG_DSN not set")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("open admin Postgres connection: %v", err)
	}
	if err := admin.Ping(ctx); err != nil {
		admin.Close()
		t.Fatalf("ping Postgres: %v", err)
	}
	schema := "agent_archive_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	quotedSchema := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+quotedSchema); err != nil {
		admin.Close()
		t.Fatalf("create test schema: %v", err)
	}
	var store *Postgres
	t.Cleanup(func() {
		if store != nil {
			store.Close()
		}
		if _, err := admin.Exec(ctx, "DROP SCHEMA "+quotedSchema+" CASCADE"); err != nil {
			t.Errorf("drop test schema: %v", err)
		}
		admin.Close()
	})
	testURL, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse test DSN: %v", err)
	}
	query := testURL.Query()
	query.Set("search_path", schema)
	testURL.RawQuery = query.Encode()
	store, err = NewPostgres(ctx, testURL.String())
	if err != nil {
		t.Fatalf("NewPostgres: %v", err)
	}
	for _, ddl := range []string{
		`CREATE TABLE agents (
			id UUID PRIMARY KEY,
			tenant_id TEXT NOT NULL,
			owner_user_id TEXT NOT NULL,
			name TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			instructions TEXT NOT NULL DEFAULT '',
			avatar_key TEXT NOT NULL,
			avatar_color TEXT NOT NULL,
			runtime_id TEXT NOT NULL,
			model_route_id TEXT NOT NULL,
			model_alias TEXT NOT NULL,
			skill_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
			knowledge_sources JSONB NOT NULL DEFAULT '[]'::jsonb,
			knowledge_revision BIGINT NOT NULL DEFAULT 1,
			status TEXT NOT NULL,
			version BIGINT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL,
			updated_at TIMESTAMPTZ NOT NULL,
			archived_at TIMESTAMPTZ
		)`,
		`CREATE TABLE channel_connectors (
			id UUID PRIMARY KEY,
			agent_id UUID NOT NULL
		)`,
		`CREATE TABLE channel_registration_flows (
			id UUID PRIMARY KEY,
			agent_id UUID NOT NULL,
			status TEXT NOT NULL
		)`,
	} {
		if _, err := store.pool.Exec(ctx, ddl); err != nil {
			t.Fatalf("create test table: %v", err)
		}
	}

	now := time.Now().UTC().Truncate(time.Microsecond)
	identity := Identity{TenantID: "tenant-1", UserID: "user-1"}
	agent, err := store.Create(ctx, Agent{
		ID: uuid.NewString(), TenantID: identity.TenantID, OwnerUserID: identity.UserID,
		Name: "Research", AvatarKey: "sparkle", AvatarColor: "blue",
		RuntimeID: "claude-code", ModelRouteID: "default", ModelAlias: "default",
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("Create Agent: %v", err)
	}
	if _, err := store.pool.Exec(ctx, `INSERT INTO channel_registration_flows
		(id, agent_id, status) VALUES ($1::uuid,$2::uuid,'awaiting_user')`,
		uuid.NewString(), agent.ID); err != nil {
		t.Fatalf("insert active registration: %v", err)
	}
	if _, err := store.Archive(
		ctx,
		identity,
		agent.ID,
		agent.Version,
		now.Add(time.Second),
	); !errors.Is(err, ErrInUse) {
		t.Fatalf("Archive with active registration = %v, want ErrInUse", err)
	}
	if _, err := store.pool.Exec(ctx, `UPDATE channel_registration_flows
		SET status='ready' WHERE agent_id=$1::uuid`, agent.ID); err != nil {
		t.Fatalf("complete registration: %v", err)
	}
	archived, err := store.Archive(
		ctx,
		identity,
		agent.ID,
		agent.Version,
		now.Add(2*time.Second),
	)
	if err != nil {
		t.Fatalf("Archive after registration = %v", err)
	}
	if archived.Status != StatusArchived || archived.Version != agent.Version+1 {
		t.Fatalf("archived Agent = %+v", archived)
	}
}
