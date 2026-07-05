package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/cocola-project/cocola/apps/admin-api/internal/store"
)

const (
	TaskStatusActive    = "active"
	TaskStatusPaused    = "paused"
	TaskStatusCompleted = "completed"

	ScheduleOnce     = "once"
	ScheduleInterval = "interval"
	ScheduleCron     = "cron"

	defaultTaskTimezone = "Asia/Shanghai"
	defaultMinInterval  = time.Hour
	defaultMaxTurns     = 30
)

type ScheduledTaskInput struct {
	OwnerType          string
	OwnerUserID        string
	Name               string
	Description        string
	Status             string
	ScheduleKind       string
	ScheduleSpec       json.RawMessage
	Timezone           string
	Prompt             string
	ModelAlias         string
	ConfigJSON         json.RawMessage
	Attachments        []store.ScheduledTaskAttachment
	ReplaceAttachments bool
	Actor              string
}

type ScheduledTaskDetail struct {
	store.ScheduledTask
	Attachments []store.ScheduledTaskAttachment `json:"attachments,omitempty"`
}

func rawOrEmptyObject(raw json.RawMessage) json.RawMessage {
	if len(strings.TrimSpace(string(raw))) == 0 {
		return json.RawMessage(`{}`)
	}
	return append(json.RawMessage(nil), raw...)
}

func normalizeTaskStatus(status string) string {
	status = strings.TrimSpace(status)
	if status == "" {
		return TaskStatusActive
	}
	return status
}

func validTaskStatus(status string) bool {
	return status == TaskStatusActive || status == TaskStatusPaused || status == TaskStatusCompleted
}

func (a *Admin) MinScheduleInterval() time.Duration {
	if a.minScheduleInterval > 0 {
		return a.minScheduleInterval
	}
	return defaultMinInterval
}

func (a *Admin) CreateScheduledTask(ctx context.Context, in ScheduledTaskInput) (ScheduledTaskDetail, error) {
	in.OwnerType = "system"
	in.OwnerUserID = ""
	task, err := a.scheduledTaskFromInput(store.ScheduledTask{}, in, true)
	if err != nil {
		return ScheduledTaskDetail{}, err
	}
	atts := in.Attachments
	for i := range atts {
		atts[i].TaskID = task.ID
		if atts[i].ID == "" {
			atts[i].ID = newID()
		}
		if atts[i].CreatedAt.IsZero() {
			atts[i].CreatedAt = task.CreatedAt
		}
		if atts[i].CreatedBy == "" {
			atts[i].CreatedBy = in.Actor
		}
	}
	if err := a.store.CreateScheduledTask(ctx, task, atts); err != nil {
		return ScheduledTaskDetail{}, err
	}
	a.audit(ctx, in.Actor, "scheduled_task.create", task.ID, "name="+task.Name)
	return ScheduledTaskDetail{ScheduledTask: task, Attachments: atts}, nil
}

func (a *Admin) CreateUserScheduledTask(ctx context.Context, ownerUserID string, in ScheduledTaskInput) (ScheduledTaskDetail, error) {
	ownerUserID = strings.TrimSpace(ownerUserID)
	if ownerUserID == "" {
		return ScheduledTaskDetail{}, ErrUnauthenticated
	}
	in.OwnerType = "user"
	in.OwnerUserID = ownerUserID
	if strings.TrimSpace(in.Actor) == "" {
		in.Actor = ownerUserID
	}
	task, err := a.scheduledTaskFromInput(store.ScheduledTask{}, in, true)
	if err != nil {
		return ScheduledTaskDetail{}, err
	}
	atts := in.Attachments
	for i := range atts {
		atts[i].TaskID = task.ID
		if atts[i].ID == "" {
			atts[i].ID = newID()
		}
		if atts[i].CreatedAt.IsZero() {
			atts[i].CreatedAt = task.CreatedAt
		}
		if atts[i].CreatedBy == "" {
			atts[i].CreatedBy = in.Actor
		}
	}
	if err := a.store.CreateScheduledTask(ctx, task, atts); err != nil {
		return ScheduledTaskDetail{}, err
	}
	return ScheduledTaskDetail{ScheduledTask: task, Attachments: atts}, nil
}

func (a *Admin) UpdateScheduledTask(ctx context.Context, id string, in ScheduledTaskInput) (ScheduledTaskDetail, error) {
	existing, err := a.store.GetScheduledTask(ctx, id)
	if err != nil {
		return ScheduledTaskDetail{}, err
	}
	task, err := a.scheduledTaskFromInput(existing, in, false)
	if err != nil {
		return ScheduledTaskDetail{}, err
	}
	atts := in.Attachments
	if in.ReplaceAttachments {
		for i := range atts {
			atts[i].TaskID = task.ID
			if atts[i].ID == "" {
				atts[i].ID = newID()
			}
			if atts[i].CreatedAt.IsZero() {
				atts[i].CreatedAt = task.UpdatedAt
			}
			if atts[i].CreatedBy == "" {
				atts[i].CreatedBy = in.Actor
			}
		}
	}
	if err := a.store.UpdateScheduledTask(ctx, task, in.ReplaceAttachments, atts); err != nil {
		return ScheduledTaskDetail{}, err
	}
	outAtts, err := a.store.ListScheduledTaskAttachments(ctx, task.ID)
	if err != nil {
		return ScheduledTaskDetail{}, err
	}
	a.audit(ctx, in.Actor, "scheduled_task.update", task.ID, "status="+task.Status)
	return ScheduledTaskDetail{ScheduledTask: task, Attachments: outAtts}, nil
}

func (a *Admin) UpdateUserScheduledTask(ctx context.Context, id, ownerUserID string, in ScheduledTaskInput) (ScheduledTaskDetail, error) {
	existing, err := a.store.GetScheduledTaskForOwner(ctx, id, ownerUserID)
	if err != nil {
		return ScheduledTaskDetail{}, err
	}
	in.OwnerType = "user"
	in.OwnerUserID = ownerUserID
	if strings.TrimSpace(in.Actor) == "" {
		in.Actor = ownerUserID
	}
	task, err := a.scheduledTaskFromInput(existing, in, false)
	if err != nil {
		return ScheduledTaskDetail{}, err
	}
	atts := in.Attachments
	if in.ReplaceAttachments {
		for i := range atts {
			atts[i].TaskID = task.ID
			if atts[i].ID == "" {
				atts[i].ID = newID()
			}
			if atts[i].CreatedAt.IsZero() {
				atts[i].CreatedAt = task.UpdatedAt
			}
			if atts[i].CreatedBy == "" {
				atts[i].CreatedBy = in.Actor
			}
		}
	}
	if err := a.store.UpdateScheduledTask(ctx, task, in.ReplaceAttachments, atts); err != nil {
		return ScheduledTaskDetail{}, err
	}
	outAtts, err := a.store.ListScheduledTaskAttachments(ctx, task.ID)
	if err != nil {
		return ScheduledTaskDetail{}, err
	}
	return ScheduledTaskDetail{ScheduledTask: task, Attachments: outAtts}, nil
}

func (a *Admin) scheduledTaskFromInput(existing store.ScheduledTask, in ScheduledTaskInput, create bool) (store.ScheduledTask, error) {
	name := strings.TrimSpace(in.Name)
	if !create && name == "" {
		name = existing.Name
	}
	status := normalizeTaskStatus(in.Status)
	if !create && strings.TrimSpace(in.Status) == "" {
		status = existing.Status
	}
	kind := strings.TrimSpace(in.ScheduleKind)
	if !create && kind == "" {
		kind = existing.ScheduleKind
	}
	tz := strings.TrimSpace(in.Timezone)
	if tz == "" {
		if !create && existing.Timezone != "" {
			tz = existing.Timezone
		} else {
			tz = defaultTaskTimezone
		}
	}
	prompt := strings.TrimSpace(in.Prompt)
	if !create && prompt == "" {
		prompt = existing.Prompt
	}
	modelAlias := strings.TrimSpace(in.ModelAlias)
	if !create && modelAlias == "" {
		modelAlias = existing.ModelAlias
	}
	maxTurns := defaultMaxTurns
	spec := rawOrEmptyObject(in.ScheduleSpec)
	if !create && string(spec) == "{}" && len(existing.ScheduleSpec) > 0 {
		spec = rawOrEmptyObject(existing.ScheduleSpec)
	}
	configJSON := rawOrEmptyObject(in.ConfigJSON)
	if !create && string(configJSON) == "{}" && len(existing.ConfigJSON) > 0 {
		configJSON = rawOrEmptyObject(existing.ConfigJSON)
	}
	if name == "" || kind == "" || prompt == "" || modelAlias == "" || !validTaskStatus(status) {
		return store.ScheduledTask{}, ErrInvalidArg
	}
	next, err := computeNextScheduledRun(kind, spec, tz, a.now().UTC(), a.MinScheduleInterval())
	if err != nil {
		return store.ScheduledTask{}, err
	}
	if status != TaskStatusActive {
		next = time.Time{}
	}
	now := a.now().UTC()
	task := existing
	if create {
		task.ID = newID()
		task.OwnerType = normalizeTaskOwnerType(in.OwnerType)
		task.OwnerUserID = strings.TrimSpace(in.OwnerUserID)
		if task.OwnerType == "user" {
			if task.OwnerUserID == "" {
				return store.ScheduledTask{}, ErrInvalidArg
			}
			task.ConversationID = "sched-" + task.ID
		}
		task.CreatedAt = now
		task.CreatedBy = in.Actor
	} else {
		task.OwnerType = existing.OwnerType
		task.OwnerUserID = existing.OwnerUserID
		task.ConversationID = existing.ConversationID
	}
	task.Name = name
	task.Description = strings.TrimSpace(in.Description)
	if !create && in.Description == "" {
		task.Description = existing.Description
	}
	task.Status = status
	task.ScheduleKind = kind
	task.ScheduleSpec = spec
	task.Timezone = tz
	task.Prompt = prompt
	task.ModelAlias = modelAlias
	task.MaxTurns = maxTurns
	task.ConfigJSON = configJSON
	task.NextRunAt = next
	task.UpdatedAt = now
	task.UpdatedBy = in.Actor
	return task, nil
}

func normalizeTaskOwnerType(ownerType string) string {
	switch strings.TrimSpace(ownerType) {
	case "user":
		return "user"
	default:
		return "system"
	}
}

func (a *Admin) ListScheduledTasks(ctx context.Context) ([]ScheduledTaskDetail, error) {
	tasks, err := a.store.ListScheduledTasks(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ScheduledTaskDetail, 0, len(tasks))
	for _, task := range tasks {
		atts, err := a.store.ListScheduledTaskAttachments(ctx, task.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, ScheduledTaskDetail{ScheduledTask: task, Attachments: atts})
	}
	return out, nil
}

func (a *Admin) ListUserScheduledTasks(ctx context.Context, ownerUserID string) ([]ScheduledTaskDetail, error) {
	tasks, err := a.store.ListScheduledTasksForOwner(ctx, ownerUserID)
	if err != nil {
		return nil, err
	}
	out := make([]ScheduledTaskDetail, 0, len(tasks))
	for _, task := range tasks {
		atts, err := a.store.ListScheduledTaskAttachments(ctx, task.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, ScheduledTaskDetail{ScheduledTask: task, Attachments: atts})
	}
	return out, nil
}

func (a *Admin) GetScheduledTask(ctx context.Context, id string) (ScheduledTaskDetail, error) {
	task, err := a.store.GetScheduledTask(ctx, id)
	if err != nil {
		return ScheduledTaskDetail{}, err
	}
	atts, err := a.store.ListScheduledTaskAttachments(ctx, id)
	if err != nil {
		return ScheduledTaskDetail{}, err
	}
	return ScheduledTaskDetail{ScheduledTask: task, Attachments: atts}, nil
}

func (a *Admin) GetUserScheduledTask(ctx context.Context, id, ownerUserID string) (ScheduledTaskDetail, error) {
	task, err := a.store.GetScheduledTaskForOwner(ctx, id, ownerUserID)
	if err != nil {
		return ScheduledTaskDetail{}, err
	}
	atts, err := a.store.ListScheduledTaskAttachments(ctx, id)
	if err != nil {
		return ScheduledTaskDetail{}, err
	}
	return ScheduledTaskDetail{ScheduledTask: task, Attachments: atts}, nil
}

func (a *Admin) DeleteScheduledTask(ctx context.Context, id, actor string) error {
	if err := a.store.DeleteScheduledTask(ctx, id); err != nil {
		return err
	}
	a.audit(ctx, actor, "scheduled_task.delete", id, "")
	return nil
}

func (a *Admin) DeleteUserScheduledTask(ctx context.Context, id, ownerUserID string) error {
	return a.store.DeleteScheduledTaskForOwner(ctx, id, ownerUserID)
}

func (a *Admin) SetScheduledTaskStatus(ctx context.Context, id, status, actor string) (ScheduledTaskDetail, error) {
	if status != TaskStatusActive && status != TaskStatusPaused {
		return ScheduledTaskDetail{}, ErrInvalidArg
	}
	task, err := a.store.GetScheduledTask(ctx, id)
	if err != nil {
		return ScheduledTaskDetail{}, err
	}
	task.Status = status
	task.UpdatedAt = a.now().UTC()
	task.UpdatedBy = actor
	if status == TaskStatusActive {
		next, err := computeNextScheduledRun(task.ScheduleKind, task.ScheduleSpec, task.Timezone, a.now().UTC(), a.MinScheduleInterval())
		if err != nil {
			return ScheduledTaskDetail{}, err
		}
		task.NextRunAt = next
	} else {
		task.NextRunAt = time.Time{}
	}
	if err := a.store.UpdateScheduledTask(ctx, task, false, nil); err != nil {
		return ScheduledTaskDetail{}, err
	}
	atts, err := a.store.ListScheduledTaskAttachments(ctx, id)
	if err != nil {
		return ScheduledTaskDetail{}, err
	}
	a.audit(ctx, actor, "scheduled_task."+status, id, "")
	return ScheduledTaskDetail{ScheduledTask: task, Attachments: atts}, nil
}

func (a *Admin) SetUserScheduledTaskStatus(ctx context.Context, id, ownerUserID, status string) (ScheduledTaskDetail, error) {
	if status != TaskStatusActive && status != TaskStatusPaused {
		return ScheduledTaskDetail{}, ErrInvalidArg
	}
	task, err := a.store.GetScheduledTaskForOwner(ctx, id, ownerUserID)
	if err != nil {
		return ScheduledTaskDetail{}, err
	}
	task.Status = status
	task.UpdatedAt = a.now().UTC()
	task.UpdatedBy = ownerUserID
	if status == TaskStatusActive {
		next, err := computeNextScheduledRun(task.ScheduleKind, task.ScheduleSpec, task.Timezone, a.now().UTC(), a.MinScheduleInterval())
		if err != nil {
			return ScheduledTaskDetail{}, err
		}
		task.NextRunAt = next
	} else {
		task.NextRunAt = time.Time{}
	}
	if err := a.store.UpdateScheduledTask(ctx, task, false, nil); err != nil {
		return ScheduledTaskDetail{}, err
	}
	atts, err := a.store.ListScheduledTaskAttachments(ctx, id)
	if err != nil {
		return ScheduledTaskDetail{}, err
	}
	return ScheduledTaskDetail{ScheduledTask: task, Attachments: atts}, nil
}

func (a *Admin) EnqueueScheduledTaskNow(ctx context.Context, id, actor string) (ScheduledTaskDetail, error) {
	task, err := a.store.GetScheduledTask(ctx, id)
	if err != nil {
		return ScheduledTaskDetail{}, err
	}
	if task.Status != TaskStatusActive {
		return ScheduledTaskDetail{}, ErrInvalidArg
	}
	task.NextRunAt = a.now().UTC()
	task.UpdatedAt = task.NextRunAt
	task.UpdatedBy = actor
	if err := a.store.UpdateScheduledTask(ctx, task, false, nil); err != nil {
		return ScheduledTaskDetail{}, err
	}
	atts, err := a.store.ListScheduledTaskAttachments(ctx, id)
	if err != nil {
		return ScheduledTaskDetail{}, err
	}
	a.audit(ctx, actor, "scheduled_task.run_now", id, "")
	return ScheduledTaskDetail{ScheduledTask: task, Attachments: atts}, nil
}

func (a *Admin) ListScheduledTaskRuns(ctx context.Context, taskID, status string, limit int) ([]store.ScheduledTaskRun, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	return a.store.ListScheduledTaskRuns(ctx, taskID, status, limit)
}

type ScheduledTaskRunDetail struct {
	store.ScheduledTaskRun
	Events []store.ScheduledTaskRunEvent `json:"events,omitempty"`
}

func (a *Admin) GetScheduledTaskRun(ctx context.Context, id string) (ScheduledTaskRunDetail, error) {
	run, err := a.store.GetScheduledTaskRun(ctx, id)
	if err != nil {
		return ScheduledTaskRunDetail{}, err
	}
	events, err := a.store.ListScheduledTaskRunEvents(ctx, id)
	if err != nil {
		return ScheduledTaskRunDetail{}, err
	}
	return ScheduledTaskRunDetail{ScheduledTaskRun: run, Events: events}, nil
}

type onceSpec struct {
	RunAt string `json:"run_at"`
}

type intervalSpec struct {
	EverySeconds int64 `json:"every_seconds"`
}

type cronSpec struct {
	Expression string `json:"expression"`
}

func computeNextScheduledRun(kind string, spec json.RawMessage, tzName string, after time.Time, minInterval time.Duration) (time.Time, error) {
	if minInterval <= 0 {
		minInterval = defaultMinInterval
	}
	loc, err := time.LoadLocation(tzName)
	if err != nil {
		return time.Time{}, ErrInvalidArg
	}
	switch kind {
	case ScheduleOnce:
		var s onceSpec
		if err := json.Unmarshal(spec, &s); err != nil || strings.TrimSpace(s.RunAt) == "" {
			return time.Time{}, ErrInvalidArg
		}
		runAt, err := time.Parse(time.RFC3339, strings.TrimSpace(s.RunAt))
		if err != nil {
			return time.Time{}, ErrInvalidArg
		}
		if !runAt.After(after) {
			return time.Time{}, ErrScheduleInPast
		}
		return runAt.UTC(), nil
	case ScheduleInterval:
		var s intervalSpec
		if err := json.Unmarshal(spec, &s); err != nil || s.EverySeconds <= 0 {
			return time.Time{}, ErrInvalidArg
		}
		d := time.Duration(s.EverySeconds) * time.Second
		if d < minInterval {
			return time.Time{}, ErrScheduleTooFrequent
		}
		return after.Add(d).UTC(), nil
	case ScheduleCron:
		var s cronSpec
		if err := json.Unmarshal(spec, &s); err != nil || strings.TrimSpace(s.Expression) == "" {
			return time.Time{}, ErrInvalidArg
		}
		c, err := parseCron5(s.Expression)
		if err != nil {
			return time.Time{}, ErrInvalidArg
		}
		first, ok := c.next(after.In(loc))
		if !ok {
			return time.Time{}, ErrInvalidArg
		}
		second, ok := c.next(first.Add(time.Second))
		if !ok {
			return time.Time{}, ErrInvalidArg
		}
		if second.Sub(first) < minInterval {
			return time.Time{}, ErrScheduleTooFrequent
		}
		return first.UTC(), nil
	default:
		return time.Time{}, ErrInvalidArg
	}
}

type cron5 struct {
	minutes map[int]bool
	hours   map[int]bool
	dom     map[int]bool
	months  map[int]bool
	dow     map[int]bool
}

func parseCron5(expr string) (cron5, error) {
	parts := strings.Fields(expr)
	if len(parts) != 5 {
		return cron5{}, fmt.Errorf("cron: want 5 fields")
	}
	fields := []struct {
		raw      string
		min, max int
	}{
		{parts[0], 0, 59},
		{parts[1], 0, 23},
		{parts[2], 1, 31},
		{parts[3], 1, 12},
		{parts[4], 0, 6},
	}
	sets := make([]map[int]bool, 0, len(fields))
	for _, f := range fields {
		set, err := parseCronField(f.raw, f.min, f.max)
		if err != nil {
			return cron5{}, err
		}
		sets = append(sets, set)
	}
	return cron5{minutes: sets[0], hours: sets[1], dom: sets[2], months: sets[3], dow: sets[4]}, nil
}

func parseCronField(raw string, min, max int) (map[int]bool, error) {
	out := map[int]bool{}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "*" {
			for i := min; i <= max; i++ {
				out[i] = true
			}
			continue
		}
		if strings.HasPrefix(part, "*/") {
			step, err := strconv.Atoi(strings.TrimPrefix(part, "*/"))
			if err != nil || step <= 0 {
				return nil, fmt.Errorf("bad step")
			}
			for i := min; i <= max; i += step {
				out[i] = true
			}
			continue
		}
		n, err := strconv.Atoi(part)
		if err != nil || n < min || n > max {
			return nil, fmt.Errorf("bad value")
		}
		out[n] = true
	}
	return out, nil
}

func (c cron5) next(after time.Time) (time.Time, bool) {
	candidate := after.Truncate(time.Minute).Add(time.Minute)
	end := candidate.AddDate(1, 0, 0)
	for !candidate.After(end) {
		weekday := int(candidate.Weekday())
		if c.minutes[candidate.Minute()] && c.hours[candidate.Hour()] &&
			c.dom[candidate.Day()] && c.months[int(candidate.Month())] && c.dow[weekday] {
			return candidate, true
		}
		candidate = candidate.Add(time.Minute)
	}
	return time.Time{}, false
}

func nextRunAfterTask(task store.ScheduledTask, after time.Time, minInterval time.Duration) (time.Time, error) {
	if task.ScheduleKind == ScheduleOnce {
		return time.Time{}, nil
	}
	return computeNextScheduledRun(task.ScheduleKind, task.ScheduleSpec, task.Timezone, after, minInterval)
}

func summarizeOutput(s string) string {
	s = strings.TrimSpace(s)
	const max = 12000
	if len(s) <= max {
		return s
	}
	head := s[:max/2]
	tail := s[len(s)-max/2:]
	return head + "\n...[truncated]...\n" + tail
}
