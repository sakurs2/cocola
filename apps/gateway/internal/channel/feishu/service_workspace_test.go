package feishu

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"
)

type workspaceRegistrationStore struct {
	Store
	mu        sync.Mutex
	flow      RegistrationFlow
	connector Connector
	completed chan Connector
}

func (store *workspaceRegistrationStore) GetConnector(
	context.Context,
	Identity,
	string,
) (Connector, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.connector.ID == "" {
		return Connector{}, ErrNotFound
	}
	return store.connector, nil
}

func (store *workspaceRegistrationStore) CreateRegistrationFlow(
	_ context.Context,
	flow RegistrationFlow,
) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.flow = flow
	return nil
}

func (store *workspaceRegistrationStore) GetRegistrationFlow(
	_ context.Context,
	_ Identity,
	agentID string,
	flowID string,
) (RegistrationFlow, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.flow.ID != flowID || store.flow.AgentID != agentID {
		return RegistrationFlow{}, ErrNotFound
	}
	return store.flow, nil
}

func (store *workspaceRegistrationStore) UpdateRegistrationFlow(
	_ context.Context,
	flowID string,
	status string,
	verificationURL string,
	errorCode string,
	expiresAt time.Time,
	updatedAt time.Time,
) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.flow.ID != flowID {
		return ErrNotFound
	}
	store.flow.Status = status
	store.flow.VerificationURL = verificationURL
	store.flow.ErrorCode = errorCode
	store.flow.ExpiresAt = expiresAt
	store.flow.UpdatedAt = updatedAt
	return nil
}

func (store *workspaceRegistrationStore) CompleteRegistration(
	_ context.Context,
	_ Identity,
	flowID string,
	connector Connector,
	updatedAt time.Time,
) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.flow.ID != flowID {
		return ErrNotFound
	}
	store.connector = connector
	store.flow.Status = FlowReady
	store.flow.UpdatedAt = updatedAt
	select {
	case store.completed <- connector:
	default:
	}
	return nil
}

func (*workspaceRegistrationStore) InterruptRegistrationFlows(
	context.Context,
	time.Time,
	time.Time,
) error {
	return nil
}

type recordingWorkspaceRegistrar struct {
	mu    sync.Mutex
	input RegistrationInput
}

func (registrar *recordingWorkspaceRegistrar) Register(
	_ context.Context,
	input RegistrationInput,
	onUpdate func(RegistrationUpdate),
) (RegistrationResult, error) {
	registrar.mu.Lock()
	registrar.input = input
	registrar.mu.Unlock()
	onUpdate(RegistrationUpdate{
		Status: FlowAwaitingUser, VerificationURL: "https://open.feishu.cn/register",
		ExpiresIn: time.Minute,
	})
	return RegistrationResult{
		AppID: "workspace-app", AppSecret: "workspace-secret",
		OwnerOpenID: "owner-open-id", TenantBrand: DomainFeishu,
	}, nil
}

func TestWorkspaceRegistrationCreatesReadyNonInboundConnector(t *testing.T) {
	store := &workspaceRegistrationStore{completed: make(chan Connector, 1)}
	registrar := &recordingWorkspaceRegistrar{}
	service, err := NewService(
		context.Background(),
		store,
		"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		registrar,
	)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	service.WithTokenHTTPClient(&http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return jsonResponse(
				`{"code":0,"tenant_access_token":"workspace-token","expire":3600}`,
			), nil
		}),
	})
	identity := Identity{TenantID: "tenant", UserID: "user"}
	if _, err := service.StartWorkspaceRegistration(context.Background(), identity); err != nil {
		t.Fatalf("StartWorkspaceRegistration: %v", err)
	}

	select {
	case connector := <-store.completed:
		if connector.AgentID != "" || connector.Status != StatusReady ||
			!connector.DesiredEnabled || connector.LastConnectedAt == nil {
			t.Fatalf("workspace connector = %+v", connector)
		}
	case <-time.After(time.Second):
		t.Fatal("workspace registration did not complete")
	}
	registrar.mu.Lock()
	input := registrar.input
	registrar.mu.Unlock()
	if input.InboundMessages {
		t.Fatal("workspace registration unexpectedly enabled inbound Feishu messages")
	}
}
