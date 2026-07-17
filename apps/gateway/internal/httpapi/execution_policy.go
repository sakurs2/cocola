package httpapi

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/cocola-project/cocola/apps/gateway/internal/agent"
)

const (
	agentMaxTurnsSetting = "execution.agent_max_turns"
	toolTimeoutSetting   = "execution.tool_step_timeout_secs"
)

type executionPolicy struct {
	agentMaxTurns int32
	toolTimeout   time.Duration
}

type runtimeSettingReader interface {
	RuntimeSetting(context.Context, string) (json.RawMessage, error)
}

func (c *runController) executionPolicy(ctx context.Context) executionPolicy {
	policy := executionPolicy{agentMaxTurns: c.agentMaxTurns, toolTimeout: c.toolTimeout}
	reader, ok := c.store.(runtimeSettingReader)
	if !ok {
		return policy
	}
	policy.agentMaxTurns = int32(runtimeSettingInt(
		ctx, reader, agentMaxTurnsSetting, int(c.agentMaxTurns), 1, 1000,
	))
	policy.toolTimeout = time.Duration(runtimeSettingInt(
		ctx, reader, toolTimeoutSetting, int(c.toolTimeout/time.Second), 30, 86400,
	)) * time.Second
	return policy
}

func runtimeSettingInt(
	ctx context.Context,
	reader runtimeSettingReader,
	key string,
	fallback, minValue, maxValue int,
) int {
	raw, err := reader.RuntimeSetting(ctx, key)
	if err != nil {
		return fallback
	}
	var value int
	if json.Unmarshal(raw, &value) != nil || value < minValue || value > maxValue {
		return fallback
	}
	return value
}

func effectiveMaxTurns(requested, configured int32) int32 {
	if requested > 0 && requested < configured {
		return requested
	}
	return configured
}

type toolStepFailure struct {
	Name  string
	Limit time.Duration
}

type toolTimer struct {
	token uint64
	timer *time.Timer
}

type toolStepWatchdog struct {
	mu        sync.Mutex
	limit     time.Duration
	cancelRun context.CancelFunc
	timers    map[string]toolTimer
	nextToken uint64
	failure   *toolStepFailure
	closed    bool
}

func newToolStepWatchdog(limit time.Duration, cancelRun context.CancelFunc) *toolStepWatchdog {
	return &toolStepWatchdog{
		limit: limit, cancelRun: cancelRun, timers: make(map[string]toolTimer),
	}
}

func (w *toolStepWatchdog) Observe(event agent.Event) {
	switch event.Kind {
	case "tool_use":
		w.start(event.Data["id"], event.Data["name"])
	case "tool_result":
		w.complete(event.Data["tool_use_id"])
	}
}

func (w *toolStepWatchdog) start(id, name string) {
	if id == "" || w.limit <= 0 {
		return
	}
	if name == "" {
		name = "tool"
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed || w.failure != nil {
		return
	}
	if previous, ok := w.timers[id]; ok {
		previous.timer.Stop()
	}
	w.nextToken++
	token := w.nextToken
	timer := time.AfterFunc(w.limit, func() { w.expire(id, name, token) })
	w.timers[id] = toolTimer{token: token, timer: timer}
}

func (w *toolStepWatchdog) complete(id string) {
	if id == "" {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if timer, ok := w.timers[id]; ok {
		timer.timer.Stop()
		delete(w.timers, id)
	}
}

func (w *toolStepWatchdog) expire(id, name string, token uint64) {
	w.mu.Lock()
	current, ok := w.timers[id]
	if !ok || current.token != token || w.closed || w.failure != nil {
		w.mu.Unlock()
		return
	}
	delete(w.timers, id)
	w.failure = &toolStepFailure{Name: name, Limit: w.limit}
	w.mu.Unlock()
	w.cancelRun()
}

func (w *toolStepWatchdog) Close() {
	w.mu.Lock()
	w.closed = true
	w.timers = nil
	w.mu.Unlock()
}

func (w *toolStepWatchdog) Failure() *toolStepFailure {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.failure == nil {
		return nil
	}
	failure := *w.failure
	return &failure
}
