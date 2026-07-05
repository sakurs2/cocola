package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	"github.com/cocola-project/cocola/apps/admin-api/internal/store"
	agentv1 "github.com/cocola-project/cocola/packages/proto/gen/go/cocola/agent/v1"
)

type SchedulerConfig struct {
	Enabled        bool
	AgentAddr      string
	GatewayURL     string
	WorkerID       string
	PollEvery      time.Duration
	RunTimeout     time.Duration
	HeartbeatEvery time.Duration
	LeaseTimeout   time.Duration
}

func (a *Admin) StartScheduler(ctx context.Context, cfg SchedulerConfig) error {
	if !cfg.Enabled {
		return nil
	}
	if strings.TrimSpace(cfg.AgentAddr) == "" {
		return ErrInvalidArg
	}
	if cfg.WorkerID == "" {
		cfg.WorkerID = "admin-api"
	}
	if cfg.PollEvery <= 0 {
		cfg.PollEvery = time.Minute
	}
	if cfg.RunTimeout <= 0 {
		cfg.RunTimeout = time.Hour
	}
	if cfg.HeartbeatEvery <= 0 {
		cfg.HeartbeatEvery = 30 * time.Second
	}
	if cfg.LeaseTimeout <= 0 {
		cfg.LeaseTimeout = maxDuration(5*time.Minute, cfg.HeartbeatEvery*4)
	}
	conn, err := grpc.Dial(cfg.AgentAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}
	runner := &grpcTaskRunner{
		rpc:        agentv1.NewAgentRuntimeServiceClient(conn),
		admin:      a,
		gatewayURL: strings.TrimRight(strings.TrimSpace(cfg.GatewayURL), "/"),
		httpClient: &http.Client{},
	}
	go func() {
		defer conn.Close()
		a.schedulerLoop(ctx, cfg, runner)
	}()
	a.schedulerStarted.Store(true)
	return nil
}

type taskRunner interface {
	Run(ctx context.Context, task store.ScheduledTask, attachments []store.ScheduledTaskAttachment, onEvent func(kind string, data map[string]string)) (string, error)
}

type grpcTaskRunner struct {
	rpc        agentv1.AgentRuntimeServiceClient
	admin      *Admin
	gatewayURL string
	httpClient *http.Client
}

func (r *grpcTaskRunner) Run(ctx context.Context, task store.ScheduledTask, attachments []store.ScheduledTaskAttachment, onEvent func(kind string, data map[string]string)) (string, error) {
	if task.OwnerType == "user" {
		return r.runUserTask(ctx, task, attachments, onEvent)
	}
	if strings.TrimSpace(task.ModelAlias) != "" {
		ctx = metadata.AppendToOutgoingContext(ctx, "x-cocola-model-alias", strings.TrimSpace(task.ModelAlias))
	}
	atts := make([]*agentv1.Attachment, 0, len(attachments))
	for _, att := range attachments {
		var content []byte
		if att.ContentB64 != "" {
			decoded, err := base64.StdEncoding.DecodeString(att.ContentB64)
			if err != nil {
				return "", fmt.Errorf("attachment %s: %w", att.Filename, err)
			}
			content = decoded
		}
		atts = append(atts, &agentv1.Attachment{
			Filename: att.Filename,
			Content:  content,
			Mime:     att.Mime,
			OssKey:   att.ObjectKey,
			Size:     att.SizeBytes,
		})
	}
	sessionID := "sched-" + task.ID + "-" + newID()
	stream, err := r.rpc.Query(ctx, &agentv1.QueryRequest{
		UserId:      "system:scheduler",
		SessionId:   sessionID,
		Prompt:      task.Prompt,
		MaxTurns:    int32(task.MaxTurns),
		Attachments: atts,
	})
	if err != nil {
		return sessionID, err
	}
	for {
		msg, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return sessionID, nil
		}
		if err != nil {
			return sessionID, err
		}
		data := msg.GetData()
		onEvent(msg.GetKind(), data)
	}
}

func (r *grpcTaskRunner) runUserTask(ctx context.Context, task store.ScheduledTask, attachments []store.ScheduledTaskAttachment, onEvent func(kind string, data map[string]string)) (string, error) {
	if r.admin == nil || strings.TrimSpace(r.gatewayURL) == "" {
		return task.ConversationID, ErrNotConfigured
	}
	sessionID := strings.TrimSpace(task.ConversationID)
	if sessionID == "" {
		sessionID = "sched-" + task.ID
	}
	tokenTTL := time.Hour
	if deadline, ok := ctx.Deadline(); ok {
		tokenTTL = time.Until(deadline) + 5*time.Minute
	}
	tok, err := r.admin.IssueRuntimeToken(ctx, task.OwnerUserID, "", tokenTTL)
	if err != nil {
		return sessionID, err
	}
	type gatewayAttachment struct {
		Filename   string `json:"filename"`
		ContentB64 string `json:"content_b64"`
		Mime       string `json:"mime"`
	}
	atts := make([]gatewayAttachment, 0, len(attachments))
	for _, att := range attachments {
		atts = append(atts, gatewayAttachment{
			Filename:   att.Filename,
			ContentB64: att.ContentB64,
			Mime:       att.Mime,
		})
	}
	body, err := json.Marshal(map[string]any{
		"prompt":             task.Prompt,
		"session_id":         sessionID,
		"max_turns":          task.MaxTurns,
		"model_alias":        strings.TrimSpace(task.ModelAlias),
		"conversation_title": task.Name,
		"conversation_type":  "scheduled_task",
		"defer_conversation_visibility_until_done": true,
		"attachments": atts,
	})
	if err != nil {
		return sessionID, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.gatewayURL+"/v1/chat", bytes.NewReader(body))
	if err != nil {
		return sessionID, err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("authorization", "Bearer "+tok)
	resp, err := r.httpClient.Do(req)
	if err != nil {
		return sessionID, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return sessionID, fmt.Errorf("gateway chat returned %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	var eventName string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event:") {
			eventName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		var ev struct {
			Kind string            `json:"kind"`
			Data map[string]string `json:"data"`
		}
		if err := json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &ev); err != nil {
			return sessionID, err
		}
		if ev.Kind == "" {
			ev.Kind = eventName
		}
		onEvent(ev.Kind, ev.Data)
	}
	if err := scanner.Err(); err != nil {
		return sessionID, err
	}
	return sessionID, nil
}

func (a *Admin) schedulerLoop(ctx context.Context, cfg SchedulerConfig, runner taskRunner) {
	for {
		a.runSchedulerOnce(ctx, cfg, runner)
		next := a.effectiveSchedulerConfig(ctx, cfg).PollEvery
		if next <= 0 {
			next = time.Minute
		}
		timer := time.NewTimer(next)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (a *Admin) runSchedulerOnce(ctx context.Context, cfg SchedulerConfig, runner taskRunner) {
	now := a.now().UTC()
	cfg = a.effectiveSchedulerConfig(ctx, cfg)
	a.expireStaleScheduledTaskRuns(ctx, cfg, now)
	if !cfg.Enabled {
		return
	}
	due, err := a.store.ListDueScheduledTasks(ctx, now, 5)
	if err != nil {
		return
	}
	for _, task := range due {
		a.executeDueTask(ctx, cfg, runner, task)
	}
}

func (a *Admin) executeDueTask(ctx context.Context, cfg SchedulerConfig, runner taskRunner, task store.ScheduledTask) {
	now := a.now().UTC()
	next, err := nextRunAfterTask(task, now, a.MinScheduleInterval())
	if err != nil {
		next = now.Add(5 * time.Minute)
	}
	runID := newID()
	sessionID := "sched-" + task.ID + "-" + runID
	run := store.ScheduledTaskRun{
		ID:           runID,
		TaskID:       task.ID,
		ScheduledFor: now,
		Status:       "running",
		WorkerID:     cfg.WorkerID,
		SessionID:    sessionID,
		ModelAlias:   task.ModelAlias,
		StartedAt:    now,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	claimedTask, ok, err := a.store.TryStartScheduledTaskRun(ctx, task.ID, run, next)
	if err != nil || !ok {
		return
	}
	if claimedTask.OwnerType == "user" {
		a.publishScheduledTaskUserEvent(ctx, UserEventScheduledTaskRunStarted, claimedTask, run, "running", "")
	}
	attachments, err := a.store.ListScheduledTaskAttachments(ctx, claimedTask.ID)
	if err != nil {
		a.finishRun(ctx, run, next, "error", "", err.Error())
		if claimedTask.OwnerType == "user" {
			a.publishScheduledTaskUserEvent(ctx, UserEventScheduledTaskRunFailed, claimedTask, run, "error", err.Error())
		}
		return
	}
	runCtx, cancel := context.WithTimeout(ctx, cfg.RunTimeout)
	defer cancel()
	stopHeartbeat := a.startScheduledTaskRunHeartbeat(runCtx, cfg, run.ID)
	defer stopHeartbeat()
	seq := 0
	var output []string
	sessionID, err = runner.Run(runCtx, claimedTask, attachments, func(kind string, data map[string]string) {
		seq++
		raw, _ := json.Marshal(data)
		_ = a.store.AppendScheduledTaskRunEvent(ctx, store.ScheduledTaskRunEvent{
			RunID:     run.ID,
			Seq:       seq,
			Kind:      kind,
			DataJSON:  raw,
			CreatedAt: a.now().UTC(),
		})
		if kind == "text" && data["text"] != "" {
			output = append(output, data["text"])
		}
	})
	run.SessionID = sessionID
	if err != nil {
		a.finishRun(ctx, run, next, "error", strings.Join(output, ""), err.Error())
		if claimedTask.OwnerType == "user" {
			a.publishScheduledTaskUserEvent(ctx, UserEventScheduledTaskRunFailed, claimedTask, run, "error", err.Error())
		}
		return
	}
	a.finishRun(ctx, run, next, "success", strings.Join(output, ""), "")
	if claimedTask.OwnerType == "user" {
		a.publishScheduledTaskUserEvent(ctx, UserEventScheduledTaskRunFinished, claimedTask, run, "success", "")
	}
}

func (a *Admin) startScheduledTaskRunHeartbeat(ctx context.Context, cfg SchedulerConfig, runID string) func() {
	every := cfg.HeartbeatEvery
	if every <= 0 {
		every = 30 * time.Second
	}
	heartbeatCtx, cancel := context.WithCancel(ctx)
	go func() {
		ticker := time.NewTicker(every)
		defer ticker.Stop()
		for {
			select {
			case <-heartbeatCtx.Done():
				return
			case <-ticker.C:
				_, _ = a.store.HeartbeatScheduledTaskRun(heartbeatCtx, runID, cfg.WorkerID, a.now().UTC())
			}
		}
	}()
	return cancel
}

func (a *Admin) expireStaleScheduledTaskRuns(ctx context.Context, cfg SchedulerConfig, now time.Time) {
	leaseTimeout := cfg.LeaseTimeout
	if leaseTimeout <= 0 {
		heartbeatEvery := cfg.HeartbeatEvery
		if heartbeatEvery <= 0 {
			heartbeatEvery = 30 * time.Second
		}
		leaseTimeout = maxDuration(5*time.Minute, heartbeatEvery*4)
	}
	const errText = "scheduled task run expired after worker heartbeat timeout"
	expired, err := a.store.ExpireStaleScheduledTaskRuns(ctx, now.Add(-leaseTimeout), now, errText, 20)
	if err != nil {
		return
	}
	for _, run := range expired {
		task, err := a.store.GetScheduledTask(ctx, run.TaskID)
		if err != nil || task.OwnerType != "user" {
			continue
		}
		a.publishScheduledTaskUserEvent(ctx, UserEventScheduledTaskRunFailed, task, run, "error", errText)
	}
}

func (a *Admin) finishRun(ctx context.Context, run store.ScheduledTaskRun, next time.Time, status, output, errText string) {
	now := a.now().UTC()
	run.Status = status
	run.OutputText = summarizeOutput(output)
	run.Error = errText
	run.FinishedAt = now
	run.UpdatedAt = now
	_ = a.store.UpdateScheduledTaskRun(ctx, run, next, true)
}

func (a *Admin) publishScheduledTaskUserEvent(ctx context.Context, eventType string, task store.ScheduledTask, run store.ScheduledTaskRun, status, errText string) {
	if task.OwnerType != "user" || strings.TrimSpace(task.OwnerUserID) == "" {
		return
	}
	if err := a.PublishUserEvent(ctx, scheduledTaskUserEvent(eventType, task, run, status, errText, a.now().UTC())); err != nil {
		// Event delivery is best-effort; the scheduled run and conversation are
		// already persisted on the authoritative path.
		return
	}
}

func maxDuration(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}

func (a *Admin) effectiveSchedulerConfig(ctx context.Context, base SchedulerConfig) SchedulerConfig {
	cfg := base
	cfg.Enabled = a.settingBool(ctx, SettingSchedulerEnabled, base.Enabled)
	cfg.PollEvery = secondsDuration(a.settingInt(ctx, SettingSchedulerPollSecs, int(base.PollEvery.Seconds())))
	cfg.RunTimeout = secondsDuration(a.settingInt(ctx, SettingSchedulerRunTimeoutSecs, int(base.RunTimeout.Seconds())))
	cfg.HeartbeatEvery = secondsDuration(a.settingInt(ctx, SettingSchedulerHeartbeatSecs, int(base.HeartbeatEvery.Seconds())))
	cfg.LeaseTimeout = secondsDuration(a.settingInt(ctx, SettingSchedulerLeaseTimeoutSecs, int(base.LeaseTimeout.Seconds())))
	return cfg
}
