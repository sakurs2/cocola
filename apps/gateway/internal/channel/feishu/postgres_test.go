package feishu

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

func TestPostgresConnectorClaimAndInboxFIFO(t *testing.T) {
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
	schema := "feishu_fifo_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
	ddls := []string{
		`CREATE TABLE agents (
			id UUID PRIMARY KEY,
			tenant_id TEXT NOT NULL,
			owner_user_id TEXT NOT NULL,
			status TEXT NOT NULL
		)`,
		`CREATE TABLE channel_connectors (
			id UUID PRIMARY KEY,
			tenant_id TEXT NOT NULL,
			user_id TEXT NOT NULL,
			agent_id UUID NOT NULL,
			provider TEXT NOT NULL,
			domain TEXT NOT NULL,
			app_id TEXT NOT NULL,
			app_secret_ciphertext TEXT NOT NULL,
			owner_open_id TEXT NOT NULL DEFAULT '',
			bot_open_id TEXT NOT NULL DEFAULT '',
			bot_name TEXT NOT NULL DEFAULT '',
			desired_enabled BOOLEAN NOT NULL,
			status TEXT NOT NULL,
			bind_code_hash TEXT NOT NULL DEFAULT '',
			bind_expires_at TIMESTAMPTZ,
			last_connected_at TIMESTAMPTZ,
			last_error_code TEXT NOT NULL DEFAULT '',
			lease_owner TEXT NOT NULL DEFAULT '',
			lease_expires_at TIMESTAMPTZ,
			version BIGINT NOT NULL DEFAULT 1,
			created_at TIMESTAMPTZ NOT NULL,
			updated_at TIMESTAMPTZ NOT NULL,
			UNIQUE (agent_id),
			UNIQUE (provider, domain, app_id)
		)`,
		`CREATE TABLE channel_registration_flows (
			id UUID PRIMARY KEY,
			tenant_id TEXT NOT NULL,
			user_id TEXT NOT NULL,
			agent_id UUID NOT NULL,
			provider TEXT NOT NULL,
			status TEXT NOT NULL,
			verification_url TEXT NOT NULL DEFAULT '',
			expires_at TIMESTAMPTZ NOT NULL,
			error_code TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL,
			updated_at TIMESTAMPTZ NOT NULL
		)`,
		`CREATE TABLE channel_inbox (
			id UUID PRIMARY KEY,
			connector_id UUID NOT NULL REFERENCES channel_connectors(id) ON DELETE CASCADE,
			event_id TEXT NOT NULL,
			external_message_id TEXT NOT NULL,
			external_chat_id TEXT NOT NULL,
			normalized_payload JSONB,
			priority INTEGER NOT NULL DEFAULT 0,
			status TEXT NOT NULL,
			attempts INTEGER NOT NULL DEFAULT 0,
			next_attempt_at TIMESTAMPTZ NOT NULL,
			lease_owner TEXT NOT NULL DEFAULT '',
			lease_expires_at TIMESTAMPTZ,
			error_code TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL,
			updated_at TIMESTAMPTZ NOT NULL,
			UNIQUE (connector_id, event_id)
		)`,
	}
	for _, ddl := range ddls {
		if _, err := store.pool.Exec(ctx, ddl); err != nil {
			t.Fatalf("create test tables: %v", err)
		}
	}

	now := time.Now().UTC().Truncate(time.Microsecond)
	tenantID := uuid.NewString()
	userID := uuid.NewString()
	agentID := uuid.NewString()
	if _, err := store.pool.Exec(ctx, `INSERT INTO agents
		(id, tenant_id, owner_user_id, status) VALUES ($1,$2,$3,'active')`,
		agentID, tenantID, userID); err != nil {
		t.Fatalf("insert Agent: %v", err)
	}
	connector, err := store.UpsertConnector(ctx, Connector{
		ID: uuid.NewString(), TenantID: tenantID, UserID: userID,
		AgentID: agentID,
		Domain:  DomainFeishu, AppID: uuid.NewString(),
		AppSecretCiphertext: "test-only", OwnerOpenID: "owner-open-id",
		DesiredEnabled: false,
		Status:         StatusDisabled, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("UpsertConnector: %v", err)
	}
	defer func() {
		if err := store.DeleteConnector(ctx, Identity{
			TenantID: connector.TenantID,
			UserID:   connector.UserID,
		}, connector.AgentID); err != nil && !errors.Is(err, ErrNotFound) {
			t.Errorf("DeleteConnector: %v", err)
		}
	}()

	connector, err = store.SetConnectorEnabled(
		ctx,
		Identity{TenantID: connector.TenantID, UserID: connector.UserID},
		connector.AgentID,
		true,
		now.Add(time.Second),
	)
	if err != nil {
		t.Fatalf("SetConnectorEnabled: %v", err)
	}
	storedConnector, err := store.GetConnectorByID(ctx, connector.ID)
	if err != nil {
		t.Fatalf("GetConnectorByID: %v", err)
	}
	if storedConnector.AgentID != connector.AgentID {
		t.Fatalf("stored Agent = %q, want %q", storedConnector.AgentID, connector.AgentID)
	}
	leaseOwner := "gateway-1"
	claimed, err := store.ClaimConnectors(
		ctx,
		leaseOwner,
		now.Add(2*time.Second),
		now.Add(time.Minute),
		10,
	)
	if err != nil {
		t.Fatalf("ClaimConnectors: %v", err)
	}
	if len(claimed) != 1 ||
		claimed[0].ID != connector.ID ||
		claimed[0].LeaseOwner != leaseOwner ||
		claimed[0].Status != StatusConnecting {
		t.Fatalf("ClaimConnectors returned %+v", claimed)
	}
	if err := store.ReleaseConnectorLease(
		ctx,
		connector.ID,
		leaseOwner,
		now.Add(3*time.Second),
	); err != nil {
		t.Fatalf("ReleaseConnectorLease: %v", err)
	}

	future := now.Add(time.Minute)
	enqueue := func(eventID string, priority int, next time.Time, created time.Time) {
		t.Helper()
		_, err := store.EnqueueInbox(ctx, InboxItem{
			ID: uuid.NewString(), ConnectorID: connector.ID,
			EventID: eventID, ExternalMessageID: eventID,
			ExternalChatID: "chat-1", Priority: priority,
			Payload:       InboxPayload{EventID: eventID, Text: eventID},
			NextAttemptAt: next, CreatedAt: created, UpdatedAt: created,
		}, 10)
		if err != nil {
			t.Fatalf("EnqueueInbox(%s): %v", eventID, err)
		}
	}
	enqueue("oldest-delayed", 0, future, now)
	enqueue("newer-ready", 0, now, now.Add(time.Second))

	if _, err := store.ClaimNextInbox(
		ctx,
		connector.ID,
		"owner-1",
		now,
		now.Add(time.Minute),
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("normal message bypassed delayed head: %v", err)
	}

	enqueue("stop", 100, now, now.Add(2*time.Second))
	stop, err := store.ClaimNextInbox(
		ctx,
		connector.ID,
		"owner-1",
		now,
		now.Add(time.Minute),
	)
	if err != nil || stop.EventID != "stop" {
		t.Fatalf("priority stop claim = %+v, %v", stop, err)
	}
	if err := store.FinishInbox(ctx, stop.ID, "owner-1", InboxDone, now); err != nil {
		t.Fatalf("FinishInbox(stop): %v", err)
	}

	next, err := store.NextInboxAttempt(ctx, connector.ID)
	if err != nil || !next.Equal(future) {
		t.Fatalf(
			"NextInboxAttempt = %v, %v; want %v (delta %v)",
			next,
			err,
			future,
			next.Sub(future),
		)
	}
	oldest, err := store.ClaimNextInbox(
		ctx,
		connector.ID,
		"owner-1",
		future,
		future.Add(time.Minute),
	)
	if err != nil || oldest.EventID != "oldest-delayed" {
		t.Fatalf("oldest claim = %+v, %v", oldest, err)
	}
	if err := store.FinishInbox(
		ctx,
		oldest.ID,
		"owner-1",
		InboxDone,
		future,
	); err != nil {
		t.Fatalf("FinishInbox(oldest): %v", err)
	}
	newer, err := store.ClaimNextInbox(
		ctx,
		connector.ID,
		"owner-1",
		future,
		future.Add(time.Minute),
	)
	if err != nil || newer.EventID != "newer-ready" {
		t.Fatalf("newer claim = %+v, %v", newer, err)
	}
	if err := store.FinishInbox(ctx, newer.ID, "owner-1", InboxDone, future); err != nil {
		t.Fatalf("FinishInbox(newer): %v", err)
	}
	if err := store.DeleteConnector(
		ctx,
		Identity{TenantID: tenantID, UserID: userID},
		agentID,
	); err != nil {
		t.Fatalf("DeleteConnector before archive: %v", err)
	}
	if _, err := store.pool.Exec(
		ctx,
		`UPDATE agents SET status='archived' WHERE id=$1`,
		agentID,
	); err != nil {
		t.Fatalf("archive Agent: %v", err)
	}
	connector.ID = uuid.NewString()
	connector.AppID = uuid.NewString()
	if _, err := store.UpsertConnector(ctx, connector); !errors.Is(err, ErrAgentArchived) {
		t.Fatalf("UpsertConnector for archived Agent = %v, want ErrAgentArchived", err)
	}
	if err := store.CreateRegistrationFlow(ctx, RegistrationFlow{
		ID: uuid.NewString(), TenantID: tenantID, UserID: userID, AgentID: agentID,
		Provider: ProviderFeishu, Status: FlowStarting,
		ExpiresAt: now.Add(time.Minute), CreatedAt: now, UpdatedAt: now,
	}); !errors.Is(err, ErrAgentArchived) {
		t.Fatalf("CreateRegistrationFlow for archived Agent = %v, want ErrAgentArchived", err)
	}
	flowID := uuid.NewString()
	if _, err := store.pool.Exec(ctx, `INSERT INTO channel_registration_flows (
		id, tenant_id, user_id, agent_id, provider, status,
		verification_url, expires_at, error_code, created_at, updated_at
	) VALUES ($1,$2,$3,$4,'feishu','authorizing','',$5,'',$6,$6)`,
		flowID, tenantID, userID, agentID, now.Add(time.Minute), now); err != nil {
		t.Fatalf("insert registration for completion guard: %v", err)
	}
	connector.OwnerOpenID = "owner-open-id"
	if err := store.CompleteRegistration(
		ctx,
		Identity{TenantID: tenantID, UserID: userID},
		flowID,
		connector,
		now,
	); !errors.Is(err, ErrAgentArchived) {
		t.Fatalf("CompleteRegistration for archived Agent = %v, want ErrAgentArchived", err)
	}
}
