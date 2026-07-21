package httpapi

import (
	"context"
	"testing"

	"github.com/cocola-project/cocola/apps/gateway/internal/chatrun"
	"github.com/cocola-project/cocola/apps/gateway/internal/project"
)

type brokerRunStore struct {
	chatrun.Store
	run chatrun.Run
}

func (s *brokerRunStore) GetOwned(_ context.Context, runID, userID string) (chatrun.Run, error) {
	if s.run.ID != runID || s.run.UserID != userID {
		return chatrun.Run{}, chatrun.ErrNotFound
	}
	return s.run, nil
}

func TestBrokerRunActiveUsesDurableExecutionState(t *testing.T) {
	run := chatrun.Run{
		ID: "run-1", UserID: "user-1", ConversationID: "conversation-1",
		Status: chatrun.StatusRunning,
	}
	api := &API{runs: &runController{
		store: &brokerRunStore{run: run}, live: map[string]*liveRun{},
	}}
	claims := project.BrokerCredentialClaims{
		RunID: run.ID, UserID: run.UserID, ConversationID: run.ConversationID,
	}
	if !api.brokerRunActive(context.Background(), claims) {
		t.Fatal("durable running execution was rejected without process-local state")
	}
	api.runs.store = &brokerRunStore{run: chatrun.Run{
		ID: run.ID, UserID: run.UserID, ConversationID: run.ConversationID,
		Status: chatrun.StatusInterrupted,
	}}
	if api.brokerRunActive(context.Background(), claims) {
		t.Fatal("terminal durable run kept broker credential active")
	}
}
