package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cocola-project/cocola/apps/gateway/internal/agent"
	"github.com/cocola-project/cocola/apps/gateway/internal/auth"
	"github.com/cocola-project/cocola/apps/gateway/internal/chatrun"
	"github.com/cocola-project/cocola/apps/gateway/internal/convo"
	"github.com/cocola-project/cocola/packages/go-common/logger"
)

type runtimeSettingsStore struct {
	chatrun.Store
	maxTurns json.RawMessage
	timeout  json.RawMessage
}

func (s runtimeSettingsStore) RuntimeSetting(_ context.Context, key string) (json.RawMessage, error) {
	switch key {
	case agentMaxTurnsSetting:
		return s.maxTurns, nil
	case toolTimeoutSetting:
		return s.timeout, nil
	default:
		return nil, errors.New("setting not found")
	}
}

func TestExecutionPolicyReadsRuntimeSettings(t *testing.T) {
	conversations := convo.NewMemory()
	controller := newRunController(runtimeSettingsStore{
		Store: chatrun.NewMemory(conversations), maxTurns: json.RawMessage(`120`), timeout: json.RawMessage(`900`),
	}, RunConfig{AgentMaxTurns: 200, ToolTimeout: 10 * time.Minute})

	policy := controller.executionPolicy(context.Background())
	if policy.agentMaxTurns != 120 || policy.toolTimeout != 15*time.Minute {
		t.Fatalf("policy = %+v, want 120 turns and 15m timeout", policy)
	}
	if got := effectiveMaxTurns(500, policy.agentMaxTurns); got != 120 {
		t.Fatalf("client raised max turns to %d, want configured cap 120", got)
	}
	if got := effectiveMaxTurns(20, policy.agentMaxTurns); got != 20 {
		t.Fatalf("client lower max turns = %d, want 20", got)
	}
}

func TestToolStepWatchdogStopsCompletedStep(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	watchdog := newToolStepWatchdog(10*time.Millisecond, cancel)
	watchdog.Observe(agent.Event{
		Kind: "tool_use", Data: map[string]string{"id": "tool-1", "name": "Bash"},
	})
	watchdog.Observe(agent.Event{
		Kind: "tool_result", Data: map[string]string{"tool_use_id": "tool-1"},
	})
	defer watchdog.Close()

	select {
	case <-ctx.Done():
		t.Fatal("completed tool step cancelled the Run")
	case <-time.After(30 * time.Millisecond):
	}
	if failure := watchdog.Failure(); failure != nil {
		t.Fatalf("completed tool step failure = %+v", failure)
	}
}

type toolTimeoutStreamer struct{}

func (toolTimeoutStreamer) Stream(
	ctx context.Context,
	_ agent.Query,
	onEvent func(agent.Event) error,
) error {
	if err := onEvent(agent.Event{
		Kind: "tool_use", Data: map[string]string{"id": "tool-1", "name": "Bash"},
	}); err != nil {
		return err
	}
	<-ctx.Done()
	return ctx.Err()
}

func TestToolStepTimeoutEndsRunWithoutAggregateDeadline(t *testing.T) {
	conversations := convo.NewMemory()
	runs := chatrun.NewMemory(conversations)
	api := New(toolTimeoutStreamer{}, auth.NewVerifier(auth.Config{}), logger.Must()).
		WithConvoStore(conversations).
		WithChatRuns(runs, RunConfig{
			AgentMaxTurns: 200, ToolTimeout: 20 * time.Millisecond,
			PingEvery: time.Hour, MergeWindow: time.Millisecond, DraftInterval: time.Millisecond,
		})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/chat", strings.NewReader(
		`{"prompt":"run a tool","session_id":"conversation-1"}`,
	))
	api.Handler().ServeHTTP(recorder, request)

	runID := recorder.Header().Get("x-cocola-run-id")
	run, err := runs.GetOwned(context.Background(), runID, auth.DevIdentity.UserID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if run.Status != chatrun.StatusError || run.ErrorCode != "STEP_TIMEOUT" {
		t.Fatalf("run = %+v, want STEP_TIMEOUT error", run)
	}
	if !strings.Contains(recorder.Body.String(), `"code":"STEP_TIMEOUT"`) {
		t.Fatalf("SSE missing STEP_TIMEOUT: %s", recorder.Body.String())
	}

	live := api.newLiveRun(request, auth.DevIdentity, chatRequest{}, chatrun.Run{})
	defer live.cancel()
	if _, hasDeadline := live.ctx.Deadline(); hasDeadline {
		t.Fatal("new Agent Run unexpectedly has an aggregate deadline")
	}
}
