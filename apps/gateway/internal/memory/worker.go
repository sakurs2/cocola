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
	RunID               string
	Identity            Identity
	ConversationID      string
	Epoch               int64
	Status              string
	Attempts            int
	OpenVikingSessionID string
	OpenVikingTaskID    string
	RecalledURIs        []string
	CreatedAt           time.Time
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
	enabled, err := s.enabled(ctx)
	if errors.Is(err, ErrNotReady) {
		return false, nil
	}
	if err != nil || !enabled {
		return false, err
	}
	job, err := s.claimJob(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if job.OpenVikingTaskID != "" {
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
		j.conversation_id, j.epoch, j.status, j.attempts, j.openviking_session_id,
		j.openviking_task_id, j.recalled_uris, j.created_at
		FROM memory_capture_jobs j
		LEFT JOIN memory_user_settings s
			ON s.tenant_id=j.tenant_id AND s.user_id=j.user_id
		WHERE j.status IN ('pending','submitted','retry')
			AND j.next_attempt_at<=now()
			AND j.epoch=COALESCE(s.epoch, 0)
		ORDER BY j.next_attempt_at, j.created_at
		FOR UPDATE OF j SKIP LOCKED LIMIT 1`).Scan(
		&job.RunID, &job.Identity.TenantID, &job.Identity.UserID,
		&job.ConversationID, &job.Epoch, &job.Status, &job.Attempts,
		&job.OpenVikingSessionID, &job.OpenVikingTaskID, &recalled, &job.CreatedAt,
	)
	if err != nil {
		return captureJob{}, err
	}
	if err := json.Unmarshal(recalled, &job.RecalledURIs); err != nil {
		return captureJob{}, err
	}
	_, err = tx.Exec(ctx, `UPDATE memory_capture_jobs SET status='submitted',
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
	if err != nil {
		return err
	}
	if !current {
		return nil
	}
	userText, assistantText, includeAssistant, err := s.captureText(ctx, job.RunID)
	if err != nil {
		return s.failJob(ctx, job, "LOAD_CONVERSATION")
	}
	if userText == "" {
		return s.failJob(ctx, job, "EMPTY_USER_MESSAGE")
	}
	sessionID := job.OpenVikingSessionID
	if sessionID == "" {
		sessionID = "cocola-" + job.RunID
	}
	if captureNeedsSessionReset(job.Status) {
		handled, recoveryErr := s.recoverCommit(ctx, job, sessionID)
		if recoveryErr != nil {
			return recoveryErr
		}
		if handled {
			return nil
		}
		if err := s.cleanupCaptureSession(ctx, job.Identity, sessionID); err != nil {
			return s.failJob(ctx, job, "SESSION_RESET")
		}
	}
	if err := s.client.createSession(ctx, job.Identity, sessionID); err != nil {
		return s.failJob(ctx, job, "SESSION_CREATE")
	}
	messages := []map[string]string{{"role": "user", "content": userText}}
	if includeAssistant && assistantText != "" {
		messages = append(messages, map[string]string{
			"role": "assistant", "content": assistantText,
		})
	}
	if err := s.client.addMessages(ctx, job.Identity, sessionID, messages); err != nil {
		_ = s.cleanupCaptureSession(ctx, job.Identity, sessionID)
		return s.failJob(ctx, job, "MESSAGE_APPEND")
	}
	if err := s.client.used(ctx, job.Identity, sessionID, job.RecalledURIs); err != nil {
		_ = s.cleanupCaptureSession(ctx, job.Identity, sessionID)
		return s.failJob(ctx, job, "CONTEXT_USAGE")
	}
	current, err = s.captureJobCurrent(ctx, job)
	if err != nil {
		return err
	}
	if !current {
		_ = s.client.deleteSession(ctx, job.Identity, sessionID)
		return nil
	}
	taskID, err := s.client.commit(ctx, job.Identity, sessionID)
	if err != nil || taskID == "" {
		// Commit archives synchronously before OpenViking returns its task ID. Keep
		// the deterministic session so a retry can recover the task by resource_id
		// (or its archive marker) instead of submitting the same turn twice.
		return s.failJob(ctx, job, "COMMIT")
	}
	_, err = s.pool.Exec(ctx, `UPDATE memory_capture_jobs SET status='submitted',
		openviking_session_id=$2, openviking_task_id=$3,
		next_attempt_at=now()+interval '30 seconds', last_error_code='', updated_at=now()
		WHERE run_id=$1 AND status<>'cancelled'`, job.RunID, sessionID, taskID)
	return err
}

func captureNeedsSessionReset(status string) bool {
	return status == "submitted" || status == "retry"
}

func (s *Service) cleanupCaptureSession(
	ctx context.Context,
	identity Identity,
	sessionID string,
) error {
	if sessionID == "" {
		return nil
	}
	err := s.client.deleteSession(ctx, identity, sessionID)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
}

func (s *Service) captureJobCurrent(ctx context.Context, job captureJob) (bool, error) {
	var current bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM memory_capture_jobs j
		LEFT JOIN memory_user_settings u
			ON u.tenant_id=j.tenant_id AND u.user_id=j.user_id
		WHERE j.run_id=$1 AND j.status<>'cancelled'
			AND j.epoch=COALESCE(u.epoch, 0)
	)`, job.RunID).Scan(&current)
	return current, err
}

func (s *Service) pollJob(ctx context.Context, job captureJob) error {
	status, err := s.client.taskStatus(ctx, job.Identity, job.OpenVikingTaskID)
	if errors.Is(err, ErrNotFound) {
		if _, clearErr := s.pool.Exec(ctx, `UPDATE memory_capture_jobs
			SET openviking_task_id='', updated_at=now()
			WHERE run_id=$1 AND status<>'cancelled'`, job.RunID); clearErr != nil {
			return clearErr
		}
		job.OpenVikingTaskID = ""
		handled, recoveryErr := s.recoverCommit(ctx, job, job.OpenVikingSessionID)
		if recoveryErr != nil {
			return recoveryErr
		}
		if handled {
			return nil
		}
		if cleanupErr := s.cleanupCaptureSession(
			ctx, job.Identity, job.OpenVikingSessionID,
		); cleanupErr != nil {
			return s.failJob(ctx, job, "SESSION_RESET")
		}
		return s.failJob(ctx, job, "TASK_NOT_FOUND")
	}
	if err != nil {
		return s.failJob(ctx, job, "TASK_POLL")
	}
	switch status {
	case "completed", "success", "succeeded":
		return s.completeJob(ctx, job.RunID)
	case "failed", "error", "cancelled":
		_, _ = s.pool.Exec(ctx, `UPDATE memory_capture_jobs SET openviking_task_id=''
			WHERE run_id=$1 AND status<>'cancelled'`, job.RunID)
		if job.OpenVikingSessionID != "" {
			_ = s.client.deleteSession(ctx, job.Identity, job.OpenVikingSessionID)
		}
		return s.failJob(ctx, job, "EXTRACTION_FAILED")
	default:
		_, err = s.pool.Exec(ctx, `UPDATE memory_capture_jobs SET status='submitted',
			next_attempt_at=now()+interval '30 seconds', updated_at=now()
			WHERE run_id=$1 AND status<>'cancelled'`, job.RunID)
		return err
	}
}

type commitRecoveryDecision int

const (
	commitRecoveryResubmit commitRecoveryDecision = iota
	commitRecoveryAdopt
	commitRecoveryWait
	commitRecoveryComplete
)

func decideCommitRecovery(task *openVikingTask, archive commitArchiveState) commitRecoveryDecision {
	if task != nil {
		switch task.Status {
		case "completed", "success", "succeeded":
			return commitRecoveryComplete
		case "failed", "error", "cancelled":
			return commitRecoveryResubmit
		default:
			return commitRecoveryAdopt
		}
	}
	switch archive {
	case commitArchiveCompleted:
		return commitRecoveryComplete
	case commitArchivePending:
		return commitRecoveryWait
	default:
		return commitRecoveryResubmit
	}
}

func (s *Service) recoverCommit(
	ctx context.Context,
	job captureJob,
	sessionID string,
) (bool, error) {
	task, found, err := s.client.latestCommitTask(ctx, job.Identity, sessionID)
	if err != nil {
		return true, s.failJob(ctx, job, "COMMIT_RECOVERY")
	}
	archive := commitArchiveAbsent
	if !found {
		archive, err = s.client.commitArchiveState(ctx, job.Identity, sessionID)
		if err != nil {
			return true, s.failJob(ctx, job, "COMMIT_RECOVERY")
		}
	}
	var recovered *openVikingTask
	if found {
		recovered = &task
	}
	switch decideCommitRecovery(recovered, archive) {
	case commitRecoveryComplete:
		return true, s.completeJob(ctx, job.RunID)
	case commitRecoveryAdopt:
		_, err = s.pool.Exec(ctx, `UPDATE memory_capture_jobs SET status='submitted',
			openviking_session_id=$2, openviking_task_id=$3,
			next_attempt_at=now(), last_error_code='', updated_at=now()
			WHERE run_id=$1 AND status<>'cancelled'`, job.RunID, sessionID, task.ID)
		return true, err
	case commitRecoveryWait:
		if time.Since(job.CreatedAt) >= s.cfg.CaptureMaxRetryHorizon {
			return true, s.failJob(ctx, job, "COMMIT_RECOVERY_TIMEOUT")
		}
		_, err = s.pool.Exec(ctx, `UPDATE memory_capture_jobs SET status='submitted',
			openviking_session_id=$2, openviking_task_id='',
			next_attempt_at=now()+interval '30 seconds', last_error_code='COMMIT_RECOVERY',
			updated_at=now() WHERE run_id=$1 AND status<>'cancelled'`, job.RunID, sessionID)
		return true, err
	default:
		return false, nil
	}
}

func (s *Service) completeJob(ctx context.Context, runID string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE memory_capture_jobs SET status='completed',
		next_attempt_at=now(), last_error_code='', updated_at=now()
		WHERE run_id=$1 AND status<>'cancelled'`, runID)
	if err == nil && tag.RowsAffected() > 0 {
		s.metrics.capture("completed")
	}
	return err
}

func (s *Service) failJob(ctx context.Context, job captureJob, code string) error {
	attempts := job.Attempts + 1
	backoff, dead := captureRetryDelay(
		attempts,
		time.Since(job.CreatedAt),
		s.cfg.CaptureAttemptLimit,
		s.cfg.CaptureMaxRetryHorizon,
	)
	if dead {
		tag, err := s.pool.Exec(ctx, `UPDATE memory_capture_jobs SET status='dead',
			attempts=$2, last_error_code=$3, updated_at=now()
			WHERE run_id=$1 AND status<>'cancelled'`,
			job.RunID, attempts, code)
		if err == nil && tag.RowsAffected() > 0 {
			s.metrics.capture("dead")
		}
		return err
	}
	tag, err := s.pool.Exec(ctx, `UPDATE memory_capture_jobs SET status='retry',
		attempts=$2, next_attempt_at=$3, last_error_code=$4, updated_at=now()
		WHERE run_id=$1 AND status<>'cancelled'`,
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
	backoff := time.Minute
	for step := 1; step < attempts && backoff < 6*time.Hour; step++ {
		backoff *= 2
	}
	if backoff > 6*time.Hour {
		backoff = 6 * time.Hour
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
		return "", "", false, err
	}
	includeAssistant := status == "success"
	if !includeAssistant {
		return userText, "", false, nil
	}
	assistantText, err := finalTextParts(assistantParts)
	return userText, assistantText, true, err
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
