package memory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

type captureJob struct {
	RunID                 string
	Identity              Identity
	ConversationID        string
	Epoch                 int64
	Status                string
	AttemptCount          int
	ProviderSessionID     string
	ProviderTaskID        string
	CancellationRequested bool
	RecalledURIs          []string
	CreatedAt             time.Time
}

func (s *Service) worker() {
	defer close(s.done)
	ticker := time.NewTicker(s.cfg.RecoveryScanInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.workerCtx.Done():
			return
		case <-s.wake:
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(s.workerCtx, 10*time.Second)
			if err := s.cleanupCaptureJobs(ctx); err != nil && s.log != nil {
				s.log.Warn("memory capture cleanup failed: " + err.Error())
			}
			cancel()
		}
		s.processAvailable(20)
	}
}

func (s *Service) processAvailable(limit int) {
	for attempt := 0; attempt < limit; attempt++ {
		if s.workerCtx.Err() != nil {
			return
		}
		ctx, cancel := context.WithTimeout(s.workerCtx, 45*time.Second)
		processed, err := s.processOne(ctx)
		cancel()
		if s.workerCtx.Err() != nil {
			return
		}
		if err != nil && s.log != nil {
			s.log.Warn("memory capture worker failed: " + err.Error())
		}
		if err != nil || !processed {
			return
		}
	}
}

func (s *Service) processOne(ctx context.Context) (bool, error) {
	job, err := s.claimJob(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if job.CancellationRequested || job.Status == "cancel_requested" {
		return true, s.cancelCaptureJob(ctx, job)
	}
	enabled, err := s.enabled(ctx)
	if errors.Is(err, ErrNotReady) {
		return false, nil
	}
	if err != nil || !enabled {
		return false, err
	}
	if job.ProviderTaskID != "" {
		return true, s.pollJob(ctx, job)
	}
	return true, s.submitJob(ctx, job)
}

func (s *Service) claimJob(ctx context.Context) (captureJob, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return captureJob{}, err
	}
	defer tx.Rollback(ctx)
	var job captureJob
	var recalled []byte
	err = tx.QueryRow(ctx, `SELECT j.run_id, j.tenant_id, j.user_id,
		j.conversation_id, j.epoch, j.status, j.attempt_count, j.provider_session_id,
		j.provider_task_id, j.cancellation_requested, j.recalled_uris, j.created_at
		FROM memory_capture_jobs j
		LEFT JOIN memory_user_settings s
			ON s.tenant_id=j.tenant_id AND s.user_id=j.user_id
		WHERE j.next_attempt_at<=now()
			AND (
				(j.status IN ('pending','processing','submitting')
					AND j.epoch=COALESCE(s.epoch, 0))
				OR j.status='cancel_requested'
				OR j.cancellation_requested
			)
		ORDER BY j.cancellation_requested DESC, j.next_attempt_at, j.created_at
		FOR UPDATE OF j SKIP LOCKED LIMIT 1`).Scan(
		&job.RunID, &job.Identity.TenantID, &job.Identity.UserID,
		&job.ConversationID, &job.Epoch, &job.Status, &job.AttemptCount,
		&job.ProviderSessionID, &job.ProviderTaskID, &job.CancellationRequested,
		&recalled, &job.CreatedAt,
	)
	if err != nil {
		return captureJob{}, err
	}
	if err := json.Unmarshal(recalled, &job.RecalledURIs); err != nil {
		return captureJob{}, err
	}
	_, err = tx.Exec(ctx, `UPDATE memory_capture_jobs SET
		status=CASE WHEN cancellation_requested THEN status ELSE 'processing' END,
		next_attempt_at=now()+interval '2 minutes', updated_at=now() WHERE run_id=$1`, job.RunID)
	if err != nil {
		return captureJob{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return captureJob{}, err
	}
	return job, nil
}

func (s *Service) submitJob(ctx context.Context, job captureJob) error {
	current, err := s.captureJobCurrent(ctx, job)
	if err != nil || !current {
		return err
	}
	sessionID := job.ProviderSessionID
	if sessionID == "" {
		sessionID = "cocola-" + job.RunID
	}
	if task, found, taskErr := s.client.latestCommitTask(ctx, job.Identity, sessionID); taskErr != nil {
		return s.retryJob(ctx, job, "TASK_LOOKUP")
	} else if found {
		return s.adoptTask(ctx, job, sessionID, task)
	}

	userText, assistantText, includeAssistant, err := s.captureText(ctx, job.RunID)
	if err != nil {
		if permanentCaptureLoadFailure(err) {
			return s.deadJob(ctx, job.RunID, "LOAD_CONVERSATION")
		}
		return s.retryJob(ctx, job, "LOAD_CONVERSATION")
	}
	if userText == "" || !includeAssistant || assistantText == "" {
		return s.deadJob(ctx, job.RunID, "INCOMPLETE_TURN")
	}
	messageCount, err := s.client.ensureSession(ctx, job.Identity, sessionID)
	if err != nil {
		return s.retryJob(ctx, job, "SESSION_GET")
	}
	switch messageCount {
	case 0:
		if err := s.client.addTurn(
			ctx, job.Identity, sessionID, userText, assistantText, job.RecalledURIs,
		); err != nil {
			return s.retryJob(ctx, job, "MESSAGE_APPEND")
		}
	case 2:
		// A prior attempt wrote the one user/assistant batch but did not persist
		// the commit response. Reuse it; never append the turn a second time.
	default:
		return s.deadJob(ctx, job.RunID, "SESSION_MESSAGE_COUNT")
	}
	current, err = s.markJobSubmitting(ctx, job)
	if err != nil || !current {
		return err
	}
	taskID, err := s.client.commit(ctx, job.Identity, sessionID)
	if err != nil {
		return s.retryJob(ctx, job, "COMMIT")
	}
	if taskID == "" {
		return s.deadJob(ctx, job.RunID, "COMMIT_NO_TASK")
	}
	var cancellationRequested bool
	err = s.pool.QueryRow(ctx, `UPDATE memory_capture_jobs SET
		status=CASE WHEN cancellation_requested THEN status ELSE 'processing' END,
		provider_session_id=$2, provider_task_id=$3,
		next_attempt_at=now()+interval '30 seconds', last_error_code='', updated_at=now()
		WHERE run_id=$1 AND status IN ('submitting','cancel_requested')
		RETURNING cancellation_requested`, job.RunID, sessionID, taskID).
		Scan(&cancellationRequested)
	if err != nil {
		return err
	}
	if cancellationRequested {
		job.Status = "submitting"
		job.CancellationRequested = true
		job.ProviderSessionID = sessionID
		job.ProviderTaskID = taskID
		return s.cancelCaptureJob(ctx, job)
	}
	return nil
}

func (s *Service) adoptTask(
	ctx context.Context,
	job captureJob,
	sessionID string,
	task openVikingTask,
) error {
	switch task.Status {
	case "completed", "success", "succeeded":
		return s.completeJob(ctx, job.RunID)
	case "failed", "error":
		return s.deadJob(ctx, job.RunID, "EXTRACTION_FAILED")
	case "cancelled":
		return s.cancelJob(ctx, job.RunID)
	default:
		_, err := s.pool.Exec(ctx, `UPDATE memory_capture_jobs SET status='processing',
			provider_session_id=$2, provider_task_id=$3,
			next_attempt_at=now()+interval '30 seconds', last_error_code='', updated_at=now()
			WHERE run_id=$1 AND NOT cancellation_requested
				AND status NOT IN ('cancelled','cancel_requested')`, job.RunID, sessionID, task.ID)
		return err
	}
}

func (s *Service) captureJobCurrent(ctx context.Context, job captureJob) (bool, error) {
	var current bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM memory_capture_jobs j
		LEFT JOIN memory_user_settings u
			ON u.tenant_id=j.tenant_id AND u.user_id=j.user_id
		WHERE j.run_id=$1 AND NOT j.cancellation_requested
			AND j.status IN ('pending','processing','submitting')
			AND j.epoch=COALESCE(u.epoch, 0)
	)`, job.RunID).Scan(&current)
	return current, err
}

func (s *Service) markJobSubmitting(ctx context.Context, job captureJob) (bool, error) {
	tag, err := s.pool.Exec(ctx, `UPDATE memory_capture_jobs j
		SET status='submitting', next_attempt_at=now()+interval '1 minute', updated_at=now()
		FROM memory_user_settings u
		WHERE j.run_id=$1 AND j.tenant_id=u.tenant_id AND j.user_id=u.user_id
			AND j.epoch=u.epoch AND NOT j.cancellation_requested
			AND j.status IN ('processing','submitting')`, job.RunID)
	return tag.RowsAffected() > 0, err
}

func (s *Service) pollJob(ctx context.Context, job captureJob) error {
	if capturePollExpired(job.CreatedAt, time.Now(), s.cfg.CaptureMaxRetryHorizon) {
		return s.deadJob(ctx, job.RunID, "TASK_POLL_TIMEOUT")
	}
	status, err := s.client.taskStatus(ctx, job.Identity, job.ProviderTaskID)
	if errors.Is(err, ErrNotFound) {
		return s.deadJob(ctx, job.RunID, "TASK_NOT_FOUND")
	}
	if err != nil {
		_, updateErr := s.pool.Exec(ctx, `UPDATE memory_capture_jobs SET
			next_attempt_at=now()+interval '30 seconds', last_error_code='TASK_POLL', updated_at=now()
			WHERE run_id=$1 AND status='processing'`, job.RunID)
		return updateErr
	}
	switch status {
	case "completed", "success", "succeeded":
		return s.completeJob(ctx, job.RunID)
	case "failed", "error":
		return s.deadJob(ctx, job.RunID, "EXTRACTION_FAILED")
	case "cancelled":
		return s.cancelJob(ctx, job.RunID)
	default:
		_, err = s.pool.Exec(ctx, `UPDATE memory_capture_jobs SET status='processing',
			next_attempt_at=now()+interval '30 seconds', updated_at=now()
			WHERE run_id=$1 AND NOT cancellation_requested
				AND status NOT IN ('cancelled','cancel_requested')`, job.RunID)
		return err
	}
}

func permanentCaptureLoadFailure(err error) bool {
	return errors.Is(err, pgx.ErrNoRows) || errors.Is(err, errCapturePayload)
}

func capturePollExpired(createdAt, now time.Time, horizon time.Duration) bool {
	return !createdAt.IsZero() && now.Sub(createdAt) >= horizon
}

func (s *Service) completeJob(ctx context.Context, runID string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE memory_capture_jobs SET status='completed',
		next_attempt_at=now(), last_error_code='', updated_at=now()
		WHERE run_id=$1 AND NOT cancellation_requested
			AND status NOT IN ('cancelled','cancel_requested')`, runID)
	if err == nil && tag.RowsAffected() > 0 {
		s.metrics.capture("completed")
	}
	return err
}

func (s *Service) cancelJob(ctx context.Context, runID string) error {
	_, err := s.pool.Exec(ctx, `UPDATE memory_capture_jobs SET status='cancelled',
		cancellation_requested=TRUE, next_attempt_at=now(), last_error_code='', updated_at=now()
		WHERE run_id=$1`, runID)
	return err
}

func (s *Service) deadJob(ctx context.Context, runID, code string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE memory_capture_jobs SET status='dead',
		last_error_code=$2, updated_at=now() WHERE run_id=$1
			AND NOT cancellation_requested AND status NOT IN ('cancelled','cancel_requested')`, runID, code)
	if err == nil && tag.RowsAffected() > 0 {
		s.metrics.capture("dead")
	}
	return err
}

func (s *Service) retryJob(ctx context.Context, job captureJob, code string) error {
	attempts := job.AttemptCount + 1
	backoff, dead := captureRetryDelay(
		attempts,
		time.Since(job.CreatedAt),
		s.cfg.CaptureAttemptLimit,
		s.cfg.CaptureMaxRetryHorizon,
	)
	if dead {
		return s.deadJob(ctx, job.RunID, code)
	}
	tag, err := s.pool.Exec(ctx, `UPDATE memory_capture_jobs SET status='pending',
		attempt_count=$2, next_attempt_at=$3, last_error_code=$4, updated_at=now()
		WHERE run_id=$1 AND NOT cancellation_requested
			AND status NOT IN ('cancelled','cancel_requested')`,
		job.RunID, attempts, time.Now().Add(backoff), code)
	if err == nil && tag.RowsAffected() > 0 {
		s.metrics.capture("retry")
	}
	return err
}

func captureRetryDelay(
	attempts int,
	age time.Duration,
	attemptLimit int,
	horizon time.Duration,
) (time.Duration, bool) {
	if attempts >= attemptLimit || age >= horizon {
		return 0, true
	}
	backoff := 15 * time.Second
	for step := 1; step < attempts && backoff < 2*time.Hour; step++ {
		backoff *= 4
	}
	if backoff > 2*time.Hour {
		backoff = 2 * time.Hour
	}
	remaining := horizon - age
	if backoff > remaining {
		backoff = remaining
	}
	return backoff, false
}

func (s *Service) captureText(
	ctx context.Context,
	runID string,
) (string, string, bool, error) {
	var status string
	var userParts []byte
	var assistantParts []byte
	err := s.pool.QueryRow(ctx, `SELECT r.status, u.parts_json,
		COALESCE(a.parts_json, '[]'::jsonb)
		FROM conversation_runs r
		JOIN messages u ON u.id=r.trace_id || '-user'
		LEFT JOIN messages a ON a.id=r.trace_id || '-assistant'
		WHERE r.trace_id=$1`, runID).Scan(&status, &userParts, &assistantParts)
	if err != nil {
		return "", "", false, err
	}
	userText, err := allTextParts(userParts)
	if err != nil {
		return "", "", false, fmt.Errorf("%w: user message: %v", errCapturePayload, err)
	}
	includeAssistant := status == "success"
	if !includeAssistant {
		return userText, "", false, nil
	}
	assistantText, err := finalTextParts(assistantParts)
	if err != nil {
		return "", "", false, fmt.Errorf("%w: assistant message: %v", errCapturePayload, err)
	}
	return userText, assistantText, true, nil
}

var errCapturePayload = errors.New("memory: invalid capture payload")

func (s *Service) cancelCaptureJob(ctx context.Context, job captureJob) error {
	sessionID := job.ProviderSessionID
	if sessionID == "" {
		sessionID = "cocola-" + job.RunID
	}
	taskID := job.ProviderTaskID
	if taskID == "" {
		task, found, err := s.client.latestCommitTask(ctx, job.Identity, sessionID)
		if err != nil {
			return s.retryCancellation(ctx, job, "CANCEL_TASK_LOOKUP")
		}
		if !found {
			// A submitting lease means another worker may still be inside Commit.
			// Recheck after the lease rather than declaring cancellation complete.
			if job.Status == "submitting" {
				return s.retryCancellation(ctx, job, "CANCEL_TASK_PENDING")
			}
			return s.cancelJob(ctx, job.RunID)
		}
		taskID = task.ID
		if terminalTaskStatus(task.Status) {
			return s.cancelJob(ctx, job.RunID)
		}
	}
	if err := s.cancelAndWait(ctx, job.Identity, taskID); err != nil {
		return s.retryCancellation(ctx, job, "CANCEL_TASK")
	}
	return s.cancelJob(ctx, job.RunID)
}

func (s *Service) retryCancellation(ctx context.Context, job captureJob, code string) error {
	attempts := job.AttemptCount + 1
	backoff := 15 * time.Second
	if attempts > 1 {
		backoff = time.Minute
	}
	_, err := s.pool.Exec(ctx, `UPDATE memory_capture_jobs SET
		cancellation_requested=TRUE,
		status=CASE WHEN status='submitting' THEN status ELSE 'cancel_requested' END,
		attempt_count=$2, next_attempt_at=$3, last_error_code=$4, updated_at=now()
		WHERE run_id=$1 AND status NOT IN ('cancelled','completed','dead')`,
		job.RunID, attempts, time.Now().Add(backoff), code)
	return err
}

func (s *Service) cleanupCaptureJobs(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM memory_capture_jobs
		WHERE run_id IN (
			SELECT run_id FROM memory_capture_jobs
			WHERE (status IN ('completed','cancelled')
					AND updated_at < now()-interval '7 days')
				OR (status='dead' AND updated_at < now()-interval '30 days')
			ORDER BY updated_at LIMIT 500
		)`)
	return err
}

type storedPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func allTextParts(raw []byte) (string, error) {
	var parts []storedPart
	if err := json.Unmarshal(raw, &parts); err != nil {
		return "", err
	}
	texts := make([]string, 0)
	for _, part := range parts {
		if part.Type == "text" && part.Text != "" {
			texts = append(texts, part.Text)
		}
	}
	return stringsJoinTrimmed(texts), nil
}

func finalTextParts(raw []byte) (string, error) {
	var parts []storedPart
	if err := json.Unmarshal(raw, &parts); err != nil {
		return "", err
	}
	lastProcess := -1
	for index, part := range parts {
		switch part.Type {
		case "environment", "reasoning", "tool-call", "progress", "session-status", "memory-recall":
			lastProcess = index
		}
	}
	texts := make([]string, 0)
	for index, part := range parts {
		if index > lastProcess && part.Type == "text" && part.Text != "" {
			texts = append(texts, part.Text)
		}
	}
	return stringsJoinTrimmed(texts), nil
}

func stringsJoinTrimmed(values []string) string {
	result := ""
	for _, value := range values {
		if result != "" {
			result += "\n\n"
		}
		result += value
	}
	return result
}

func (job captureJob) String() string {
	return fmt.Sprintf("memory capture %s", job.RunID)
}
