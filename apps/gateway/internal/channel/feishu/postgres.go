package feishu

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Postgres struct{ pool *pgxpool.Pool }

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

func lockActiveAgent(
	ctx context.Context,
	tx pgx.Tx,
	id Identity,
	agentID string,
) error {
	var status string
	err := tx.QueryRow(ctx, `SELECT status FROM agents
		WHERE id=$1::uuid AND tenant_id=$2 AND owner_user_id=$3
		FOR SHARE`, agentID, id.TenantID, id.UserID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if status != "active" {
		return ErrAgentArchived
	}
	return nil
}

const connectorColumns = `id::text, tenant_id, user_id, agent_id::text, provider, domain, app_id,
	app_secret_ciphertext, owner_open_id, bot_open_id, bot_name,
	desired_enabled, status, bind_code_hash, bind_expires_at,
	last_connected_at, last_error_code, lease_owner, lease_expires_at, version,
	created_at, updated_at`

func scanConnector(row pgx.Row) (Connector, error) {
	var value Connector
	err := row.Scan(
		&value.ID, &value.TenantID, &value.UserID, &value.AgentID, &value.Provider, &value.Domain,
		&value.AppID, &value.AppSecretCiphertext, &value.OwnerOpenID,
		&value.BotOpenID, &value.BotName,
		&value.DesiredEnabled, &value.Status, &value.BindCodeHash,
		&value.BindExpiresAt, &value.LastConnectedAt, &value.LastErrorCode,
		&value.LeaseOwner, &value.LeaseExpiresAt, &value.Version,
		&value.CreatedAt, &value.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Connector{}, ErrNotFound
	}
	return value, err
}

func (p *Postgres) GetConnector(
	ctx context.Context,
	id Identity,
	agentID string,
) (Connector, error) {
	return scanConnector(p.pool.QueryRow(ctx, `SELECT `+connectorColumns+`
		FROM channel_connectors
		WHERE tenant_id=$1 AND user_id=$2 AND agent_id=$3::uuid AND provider='feishu'`,
		id.TenantID, id.UserID, agentID))
}

func (p *Postgres) GetConnectorByID(ctx context.Context, id string) (Connector, error) {
	return scanConnector(p.pool.QueryRow(ctx, `SELECT `+connectorColumns+`
		FROM channel_connectors WHERE id=$1::uuid`, id))
}

func (p *Postgres) UpsertConnector(ctx context.Context, value Connector) (Connector, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return Connector{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockActiveAgent(ctx, tx, Identity{
		TenantID: value.TenantID,
		UserID:   value.UserID,
	}, value.AgentID); err != nil {
		return Connector{}, err
	}
	const query = `INSERT INTO channel_connectors (
		id, tenant_id, user_id, agent_id, provider, domain, app_id, app_secret_ciphertext,
		owner_open_id, bot_open_id, bot_name,
		desired_enabled, status, bind_code_hash, bind_expires_at,
		last_connected_at, last_error_code, lease_owner, lease_expires_at,
		version, created_at, updated_at
	) VALUES (
		$1::uuid,$2,$3,$4::uuid,'feishu',$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,'',NULL,1,$17,$18
	)
	ON CONFLICT (agent_id) DO UPDATE SET
		domain=EXCLUDED.domain,
		app_id=EXCLUDED.app_id,
		app_secret_ciphertext=EXCLUDED.app_secret_ciphertext,
		owner_open_id=EXCLUDED.owner_open_id,
		bot_open_id=EXCLUDED.bot_open_id,
		bot_name=EXCLUDED.bot_name,
		desired_enabled=EXCLUDED.desired_enabled,
		status=EXCLUDED.status,
		bind_code_hash=EXCLUDED.bind_code_hash,
		bind_expires_at=EXCLUDED.bind_expires_at,
		last_connected_at=EXCLUDED.last_connected_at,
		last_error_code=EXCLUDED.last_error_code,
		lease_owner='',
		lease_expires_at=NULL,
		version=channel_connectors.version+1,
		updated_at=EXCLUDED.updated_at
	RETURNING ` + connectorColumns
	result, err := scanConnector(tx.QueryRow(ctx, query,
		value.ID, value.TenantID, value.UserID, value.AgentID, value.Domain, value.AppID,
		value.AppSecretCiphertext, value.OwnerOpenID, value.BotOpenID,
		value.BotName,
		value.DesiredEnabled, value.Status, value.BindCodeHash,
		value.BindExpiresAt, value.LastConnectedAt, value.LastErrorCode,
		value.CreatedAt, value.UpdatedAt,
	))
	if postgresCode(err, "23505") {
		return Connector{}, ErrConflict
	}
	if err != nil {
		return Connector{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Connector{}, err
	}
	return result, nil
}

func (p *Postgres) DeleteConnector(ctx context.Context, id Identity, agentID string) error {
	tag, err := p.pool.Exec(ctx, `DELETE FROM channel_connectors
		WHERE tenant_id=$1 AND user_id=$2 AND agent_id=$3::uuid AND provider='feishu'`,
		id.TenantID, id.UserID, agentID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *Postgres) SetConnectorEnabled(
	ctx context.Context,
	id Identity,
	agentID string,
	enabled bool,
	now time.Time,
) (Connector, error) {
	return scanConnector(p.pool.QueryRow(ctx, `UPDATE channel_connectors SET
		desired_enabled=$4,
		status=CASE
			WHEN $4=FALSE THEN 'disabled'
			WHEN owner_open_id='' THEN 'awaiting_bind'
			ELSE 'connecting'
		END,
		last_error_code='',
		lease_owner='', lease_expires_at=NULL, version=version+1, updated_at=$5
		WHERE tenant_id=$1 AND user_id=$2 AND agent_id=$3::uuid AND provider='feishu'
		RETURNING `+connectorColumns, id.TenantID, id.UserID, agentID, enabled, now))
}

func (p *Postgres) UpdateConnectorState(
	ctx context.Context,
	id string,
	leaseOwner string,
	status string,
	botOpenID string,
	botName string,
	errorCode string,
	connectedAt *time.Time,
	now time.Time,
) error {
	tag, err := p.pool.Exec(ctx, `UPDATE channel_connectors SET
		status=$3,
		bot_open_id=CASE WHEN $4='' THEN bot_open_id ELSE $4 END,
		bot_name=CASE WHEN $5='' THEN bot_name ELSE $5 END,
		last_error_code=$6,
		last_connected_at=COALESCE($7,last_connected_at),
		updated_at=$8
		WHERE id=$1::uuid AND lease_owner=$2 AND desired_enabled=TRUE`,
		id, leaseOwner, status, botOpenID, botName, errorCode, connectedAt, now)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrLeaseLost
	}
	return nil
}

func (p *Postgres) BindConnectorOwner(
	ctx context.Context,
	id string,
	codeHash string,
	ownerOpenID string,
	now time.Time,
) (Connector, error) {
	return scanConnector(p.pool.QueryRow(ctx, `UPDATE channel_connectors SET
		owner_open_id=$3, bind_code_hash='', bind_expires_at=NULL,
		status='connecting', lease_owner='', lease_expires_at=NULL,
		version=version+1, updated_at=$4
		WHERE id=$1::uuid AND bind_code_hash=$2
			AND bind_expires_at >= $4 AND status='awaiting_bind'
		RETURNING `+connectorColumns, id, codeHash, ownerOpenID, now))
}

func (p *Postgres) ClaimConnectors(
	ctx context.Context,
	owner string,
	now time.Time,
	expiresAt time.Time,
	limit int,
) ([]Connector, error) {
	if limit <= 0 {
		return []Connector{}, nil
	}
	rows, err := p.pool.Query(ctx, `WITH candidates AS (
		SELECT id AS candidate_id FROM channel_connectors
		WHERE desired_enabled=TRUE
			AND status <> 'error'
			AND (lease_expires_at IS NULL OR lease_expires_at < $2)
		ORDER BY updated_at, id
		FOR UPDATE SKIP LOCKED
		LIMIT $4
	)
	UPDATE channel_connectors c SET
		lease_owner=$1, lease_expires_at=$3,
		status=CASE WHEN c.status='awaiting_bind' THEN c.status ELSE 'connecting' END,
		updated_at=$2
	FROM candidates
	WHERE c.id=candidates.candidate_id
	RETURNING `+connectorColumns, owner, now, expiresAt, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Connector
	for rows.Next() {
		value, scanErr := scanConnector(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, value)
	}
	return result, rows.Err()
}

func (p *Postgres) OwnedConnectors(ctx context.Context, owner string) ([]Connector, error) {
	rows, err := p.pool.Query(ctx, `SELECT `+connectorColumns+`
		FROM channel_connectors WHERE lease_owner=$1`, owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Connector
	for rows.Next() {
		value, scanErr := scanConnector(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, value)
	}
	return result, rows.Err()
}

func (p *Postgres) RenewConnectorLease(
	ctx context.Context,
	id string,
	owner string,
	now time.Time,
	expiresAt time.Time,
) error {
	tag, err := p.pool.Exec(ctx, `UPDATE channel_connectors
		SET lease_expires_at=$4, updated_at=$3
		WHERE id=$1::uuid AND lease_owner=$2 AND desired_enabled=TRUE
			AND lease_expires_at >= $3`, id, owner, now, expiresAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrLeaseLost
	}
	return nil
}

func (p *Postgres) ReleaseConnectorLease(
	ctx context.Context,
	id string,
	owner string,
	now time.Time,
) error {
	_, err := p.pool.Exec(ctx, `UPDATE channel_connectors
		SET lease_owner='', lease_expires_at=NULL, updated_at=$3
		WHERE id=$1::uuid AND lease_owner=$2`, id, owner, now)
	return err
}

func (p *Postgres) CreateRegistrationFlow(ctx context.Context, flow RegistrationFlow) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockActiveAgent(ctx, tx, Identity{
		TenantID: flow.TenantID,
		UserID:   flow.UserID,
	}, flow.AgentID); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO channel_registration_flows (
		id, tenant_id, user_id, agent_id, provider, status, verification_url,
		expires_at, error_code, created_at, updated_at
	) VALUES ($1::uuid,$2,$3,$4::uuid,'feishu',$5,$6,$7,$8,$9,$10)`,
		flow.ID, flow.TenantID, flow.UserID, flow.AgentID, flow.Status, flow.VerificationURL,
		flow.ExpiresAt, flow.ErrorCode, flow.CreatedAt, flow.UpdatedAt)
	if postgresCode(err, "23505") {
		return ErrConflict
	}
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

const flowColumns = `id::text, tenant_id, user_id, agent_id::text, provider, status,
	verification_url, expires_at, error_code, created_at, updated_at`

func scanFlow(row pgx.Row) (RegistrationFlow, error) {
	var flow RegistrationFlow
	err := row.Scan(
		&flow.ID, &flow.TenantID, &flow.UserID, &flow.AgentID, &flow.Provider, &flow.Status,
		&flow.VerificationURL, &flow.ExpiresAt, &flow.ErrorCode,
		&flow.CreatedAt, &flow.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return RegistrationFlow{}, ErrNotFound
	}
	return flow, err
}

func (p *Postgres) GetRegistrationFlow(
	ctx context.Context,
	id Identity,
	agentID string,
	flowID string,
) (RegistrationFlow, error) {
	return scanFlow(p.pool.QueryRow(ctx, `SELECT `+flowColumns+`
		FROM channel_registration_flows
		WHERE id=$1::uuid AND tenant_id=$2 AND user_id=$3
			AND agent_id=$4::uuid AND provider='feishu'`,
		flowID, id.TenantID, id.UserID, agentID))
}

func (p *Postgres) GetActiveRegistrationFlow(
	ctx context.Context,
	id Identity,
	agentID string,
) (RegistrationFlow, error) {
	return scanFlow(p.pool.QueryRow(ctx, `SELECT `+flowColumns+`
		FROM channel_registration_flows
		WHERE tenant_id=$1 AND user_id=$2 AND agent_id=$3::uuid AND provider='feishu'
			AND status IN ('starting','awaiting_user','authorizing')
		ORDER BY created_at DESC
		LIMIT 1`, id.TenantID, id.UserID, agentID))
}

func (p *Postgres) UpdateRegistrationFlow(
	ctx context.Context,
	id string,
	status string,
	verificationURL string,
	errorCode string,
	expiresAt time.Time,
	now time.Time,
) error {
	tag, err := p.pool.Exec(ctx, `UPDATE channel_registration_flows SET
		status=$2,
		verification_url=CASE WHEN $3='' THEN verification_url ELSE $3 END,
		error_code=$4,
		expires_at=$5,
		updated_at=$6
		WHERE id=$1::uuid
			AND status IN ('starting','awaiting_user','authorizing')`,
		id, status, verificationURL, errorCode, expiresAt, now)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrFlowTerminated
	}
	return nil
}

func (p *Postgres) CancelRegistrationFlow(
	ctx context.Context,
	id Identity,
	agentID string,
	flowID string,
	now time.Time,
) error {
	tag, err := p.pool.Exec(ctx, `UPDATE channel_registration_flows SET
		status='cancelled', updated_at=$5
		WHERE id=$1::uuid AND tenant_id=$2 AND user_id=$3
			AND agent_id=$4::uuid
			AND status IN ('starting','awaiting_user','authorizing')`,
		flowID, id.TenantID, id.UserID, agentID, now)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *Postgres) CompleteRegistration(
	ctx context.Context,
	id Identity,
	flowID string,
	connector Connector,
	now time.Time,
) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var status, agentID string
	err = tx.QueryRow(ctx, `SELECT status, agent_id::text FROM channel_registration_flows
		WHERE id=$1::uuid AND tenant_id=$2 AND user_id=$3
		FOR UPDATE`, flowID, id.TenantID, id.UserID).Scan(&status, &agentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if status != FlowStarting && status != FlowAwaitingUser && status != FlowAuthorizing {
		return ErrFlowTerminated
	}
	if connector.AppID == "" || connector.AppSecretCiphertext == "" || connector.OwnerOpenID == "" {
		return ErrInvalid
	}
	if connector.AgentID != agentID {
		return ErrInvalid
	}
	if err := lockActiveAgent(ctx, tx, id, agentID); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO channel_connectors (
		id, tenant_id, user_id, agent_id, provider, domain, app_id, app_secret_ciphertext,
		owner_open_id, desired_enabled, status, created_at, updated_at
	) VALUES ($1::uuid,$2,$3,$4::uuid,'feishu',$5,$6,$7,$8,TRUE,'connecting',$9,$9)
	ON CONFLICT (agent_id) DO UPDATE SET
		domain=EXCLUDED.domain,
		app_id=EXCLUDED.app_id,
		app_secret_ciphertext=EXCLUDED.app_secret_ciphertext,
		owner_open_id=EXCLUDED.owner_open_id,
		bot_open_id='',
		bot_name='',
		desired_enabled=TRUE,
		status='connecting',
		bind_code_hash='',
		bind_expires_at=NULL,
		last_error_code='',
		lease_owner='',
		lease_expires_at=NULL,
		version=channel_connectors.version+1,
		updated_at=EXCLUDED.updated_at`,
		connector.ID, id.TenantID, id.UserID, connector.AgentID, connector.Domain,
		connector.AppID, connector.AppSecretCiphertext, connector.OwnerOpenID, now)
	if postgresCode(err, "23505") {
		return ErrConflict
	}
	if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `UPDATE channel_registration_flows
		SET status='ready', verification_url='', error_code='', updated_at=$2
		WHERE id=$1::uuid`, flowID, now)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrFlowTerminated
	}
	return tx.Commit(ctx)
}

func (p *Postgres) InterruptRegistrationFlows(
	ctx context.Context,
	staleBefore time.Time,
	now time.Time,
) error {
	_, err := p.pool.Exec(ctx, `UPDATE channel_registration_flows
		SET status='interrupted', error_code='gateway_restarted', updated_at=$2
		WHERE status IN ('starting','awaiting_user','authorizing')
			AND updated_at < $1`, staleBefore, now)
	return err
}

func (p *Postgres) GetSession(
	ctx context.Context,
	connectorID string,
	externalChatID string,
) (Session, error) {
	var value Session
	var optionsJSON []byte
	err := p.pool.QueryRow(ctx, `SELECT connector_id::text, external_chat_id,
		conversation_id, COALESCE(pending_question_id::text,''),
		COALESCE(pending_question_version,0),
		COALESCE(pending_options_json,'[]'::jsonb), updated_at
		FROM channel_sessions
		WHERE connector_id=$1::uuid AND external_chat_id=$2`,
		connectorID, externalChatID).Scan(
		&value.ConnectorID, &value.ExternalChatID, &value.ConversationID,
		&value.PendingQuestionID, &value.PendingQuestionVersion,
		&optionsJSON, &value.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	if err != nil {
		return Session{}, err
	}
	if err := json.Unmarshal(optionsJSON, &value.PendingOptions); err != nil {
		return Session{}, err
	}
	return value, nil
}

func (p *Postgres) UpsertSession(ctx context.Context, value Session) error {
	var optionsJSON []byte
	var err error
	if value.PendingQuestionID != "" {
		optionsJSON, err = json.Marshal(value.PendingOptions)
		if err != nil {
			return err
		}
	}
	_, err = p.pool.Exec(ctx, `INSERT INTO channel_sessions (
		connector_id, external_chat_id, conversation_id, pending_question_id,
		pending_question_version, pending_options_json, updated_at
	) VALUES (
		$1::uuid,$2,$3,NULLIF($4,'')::uuid,NULLIF($5,0),$6,$7
	)
	ON CONFLICT (connector_id, external_chat_id) DO UPDATE SET
		conversation_id=EXCLUDED.conversation_id,
		pending_question_id=EXCLUDED.pending_question_id,
		pending_question_version=EXCLUDED.pending_question_version,
		pending_options_json=EXCLUDED.pending_options_json,
		updated_at=EXCLUDED.updated_at`,
		value.ConnectorID, value.ExternalChatID, value.ConversationID,
		value.PendingQuestionID, value.PendingQuestionVersion, optionsJSON,
		value.UpdatedAt)
	return err
}

func (p *Postgres) DeleteSessions(ctx context.Context, connectorID string) error {
	_, err := p.pool.Exec(ctx, `DELETE FROM channel_sessions WHERE connector_id=$1::uuid`, connectorID)
	return err
}

func (p *Postgres) EnqueueInbox(
	ctx context.Context,
	item InboxItem,
	maxPending int,
) (bool, error) {
	payload, err := json.Marshal(item.Payload)
	if err != nil {
		return false, err
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var connectorExists bool
	if err := tx.QueryRow(ctx, `SELECT TRUE FROM channel_connectors
		WHERE id=$1::uuid FOR UPDATE`, item.ConnectorID).Scan(&connectorExists); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, ErrNotFound
		}
		return false, err
	}
	var pending int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM channel_inbox
		WHERE connector_id=$1::uuid AND status IN ('pending','processing','retry')`,
		item.ConnectorID).Scan(&pending); err != nil {
		return false, err
	}
	if pending >= maxPending && item.Priority <= 0 {
		return false, ErrQueueFull
	}
	tag, err := tx.Exec(ctx, `INSERT INTO channel_inbox (
		id, connector_id, event_id, external_message_id, external_chat_id,
		normalized_payload, priority, status, attempts, next_attempt_at,
		created_at, updated_at
	) VALUES ($1::uuid,$2::uuid,$3,$4,$5,$6,$7,'pending',0,$8,$9,$9)
	ON CONFLICT (connector_id, event_id) DO NOTHING`,
		item.ID, item.ConnectorID, item.EventID, item.ExternalMessageID,
		item.ExternalChatID, payload, item.Priority, item.NextAttemptAt,
		item.CreatedAt)
	if err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func (p *Postgres) ClaimNextInbox(
	ctx context.Context,
	connectorID string,
	owner string,
	now time.Time,
	expiresAt time.Time,
) (InboxItem, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return InboxItem{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var id string
	err = tx.QueryRow(ctx, `SELECT id::text FROM channel_inbox
		WHERE connector_id=$1::uuid
			AND priority > 0
			AND (
				(status IN ('pending','retry') AND next_attempt_at <= $2)
				OR
				(status='processing' AND lease_expires_at <= $2)
			)
		ORDER BY priority DESC, created_at
		FOR UPDATE SKIP LOCKED
		LIMIT 1`, connectorID, now).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		var status string
		var nextAttemptAt time.Time
		var leaseExpiresAt *time.Time
		err = tx.QueryRow(ctx, `SELECT id::text, status, next_attempt_at, lease_expires_at
			FROM channel_inbox
			WHERE connector_id=$1::uuid
				AND priority <= 0
				AND status IN ('pending','retry','processing')
			ORDER BY created_at
			FOR UPDATE SKIP LOCKED
			LIMIT 1`, connectorID).Scan(
			&id,
			&status,
			&nextAttemptAt,
			&leaseExpiresAt,
		)
		if errors.Is(err, pgx.ErrNoRows) {
			return InboxItem{}, ErrNotFound
		}
		if err != nil {
			return InboxItem{}, err
		}
		eligible := (status == InboxPending || status == InboxRetry) &&
			!nextAttemptAt.After(now)
		if status == InboxProcessing {
			eligible = leaseExpiresAt != nil && !leaseExpiresAt.After(now)
		}
		if !eligible {
			return InboxItem{}, ErrNotFound
		}
	}
	if err != nil {
		return InboxItem{}, err
	}
	var item InboxItem
	var payload []byte
	err = tx.QueryRow(ctx, `UPDATE channel_inbox SET
		status='processing', attempts=attempts+1,
		lease_owner=$2, lease_expires_at=$3, updated_at=$4
		WHERE id=$1::uuid
		RETURNING id::text, connector_id::text, event_id, external_message_id,
			external_chat_id, normalized_payload, priority, status, attempts,
			next_attempt_at, lease_owner, lease_expires_at, error_code,
			created_at, updated_at`,
		id, owner, expiresAt, now).Scan(
		&item.ID, &item.ConnectorID, &item.EventID, &item.ExternalMessageID,
		&item.ExternalChatID, &payload, &item.Priority, &item.Status,
		&item.Attempts, &item.NextAttemptAt, &item.LeaseOwner,
		&item.LeaseExpiresAt, &item.ErrorCode, &item.CreatedAt, &item.UpdatedAt,
	)
	if err != nil {
		return InboxItem{}, err
	}
	if err := json.Unmarshal(payload, &item.Payload); err != nil {
		return InboxItem{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return InboxItem{}, err
	}
	return item, nil
}

func (p *Postgres) NextInboxAttempt(
	ctx context.Context,
	connectorID string,
) (time.Time, error) {
	var next *time.Time
	err := p.pool.QueryRow(ctx, `WITH normal_head AS (
		SELECT status, next_attempt_at, lease_expires_at
		FROM channel_inbox
		WHERE connector_id=$1::uuid
			AND priority <= 0
			AND status IN ('pending','retry','processing')
		ORDER BY created_at
		LIMIT 1
	)
	SELECT MIN(available_at) FROM (
		SELECT CASE
			WHEN status='processing' THEN lease_expires_at
			ELSE next_attempt_at
		END AS available_at
		FROM channel_inbox
		WHERE connector_id=$1::uuid
			AND priority > 0
			AND status IN ('pending','retry','processing')
		UNION ALL
		SELECT CASE
			WHEN status='processing' THEN lease_expires_at
			ELSE next_attempt_at
		END AS available_at
		FROM normal_head
	) queued`, connectorID).Scan(&next)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, ErrNotFound
	}
	if err != nil {
		return time.Time{}, err
	}
	if next == nil || next.IsZero() {
		return time.Time{}, ErrNotFound
	}
	return *next, nil
}

func (p *Postgres) FinishInbox(
	ctx context.Context,
	id string,
	owner string,
	status string,
	now time.Time,
) error {
	tag, err := p.pool.Exec(ctx, `UPDATE channel_inbox SET
		status=$3, normalized_payload=NULL, lease_owner='',
		lease_expires_at=NULL, error_code='', updated_at=$4
		WHERE id=$1::uuid AND lease_owner=$2 AND status='processing'`,
		id, owner, status, now)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrLeaseLost
	}
	return nil
}

func (p *Postgres) RetryInbox(
	ctx context.Context,
	id string,
	owner string,
	attempts int,
	nextAttemptAt time.Time,
	errorCode string,
	now time.Time,
) error {
	status := InboxRetry
	clearPayload := false
	if attempts >= 5 {
		status = InboxFailed
		clearPayload = true
	}
	tag, err := p.pool.Exec(ctx, `UPDATE channel_inbox SET
		status=$3,
		normalized_payload=CASE WHEN $4 THEN NULL ELSE normalized_payload END,
		next_attempt_at=$5, lease_owner='', lease_expires_at=NULL,
		error_code=$6, updated_at=$7
		WHERE id=$1::uuid AND lease_owner=$2 AND status='processing'`,
		id, owner, status, clearPayload, nextAttemptAt, errorCode, now)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrLeaseLost
	}
	return nil
}

func (p *Postgres) CleanupInbox(ctx context.Context, before time.Time) error {
	_, err := p.pool.Exec(ctx, `DELETE FROM channel_inbox
		WHERE status IN ('done','rejected','failed') AND updated_at < $1`, before)
	return err
}

func postgresCode(err error, code string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == code
}
