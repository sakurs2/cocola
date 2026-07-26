package feishu

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/cocola-project/cocola/apps/gateway/internal/secretbox"
)

const (
	registrationTTL     = 10 * time.Minute
	registrationWait    = 10 * time.Second
	staleRegistration   = 15 * time.Second
	manualBindTTL       = 10 * time.Minute
	maxAppIDLength      = 256
	maxAppSecretLength  = 4 << 10
	maxModelIDLength    = 256
	maxModelAliasLength = 256
)

type Service struct {
	store     Store
	box       *secretbox.Box
	registrar Registrar
	root      context.Context
	now       func() time.Time
	changes   chan struct{}
	avatarURL string

	mu      sync.Mutex
	cancels map[string]context.CancelFunc
}

func NewService(
	root context.Context,
	store Store,
	secretKey string,
	registrar Registrar,
) (*Service, error) {
	if root == nil {
		root = context.Background()
	}
	if store == nil {
		return nil, errors.New("feishu connector store is required")
	}
	box, err := secretbox.New(secretKey)
	if err != nil {
		return nil, err
	}
	return &Service{
		store: store, box: box, registrar: registrar, root: root,
		now:     func() time.Time { return time.Now().UTC() },
		changes: make(chan struct{}, 1), cancels: make(map[string]context.CancelFunc),
	}, nil
}

func (s *Service) Changes() <-chan struct{} { return s.changes }

func (s *Service) WithRegistrationAvatarURL(value string) *Service {
	s.avatarURL = strings.TrimSpace(value)
	return s
}

func (s *Service) notify() {
	select {
	case s.changes <- struct{}{}:
	default:
	}
}

func (s *Service) Connection(ctx context.Context, id Identity) (ConnectorView, error) {
	view := ConnectorView{Status: "not_configured"}
	connector, err := s.store.GetConnector(ctx, id)
	if err == nil {
		view = connectorView(connector)
	} else if !errors.Is(err, ErrNotFound) {
		return ConnectorView{}, err
	}
	flow, flowErr := s.store.GetActiveRegistrationFlow(ctx, id)
	if flowErr == nil {
		view.Registration = &flow
	} else if !errors.Is(flowErr, ErrNotFound) {
		return ConnectorView{}, flowErr
	}
	return view, nil
}

func connectorView(connector Connector) ConnectorView {
	return ConnectorView{
		Connected:       true,
		Enabled:         connector.DesiredEnabled,
		Status:          connector.Status,
		Domain:          connector.Domain,
		BotName:         connector.BotName,
		ModelRouteID:    connector.ModelRouteID,
		ModelAlias:      connector.ModelAlias,
		LastConnectedAt: connector.LastConnectedAt,
		LastErrorCode:   connector.LastErrorCode,
	}
}

func (s *Service) StartRegistration(
	ctx context.Context,
	id Identity,
) (RegistrationFlow, error) {
	if s.registrar == nil {
		return RegistrationFlow{}, errors.New("feishu application registration is unavailable")
	}
	now := s.now()
	_ = s.store.InterruptRegistrationFlows(ctx, now.Add(-staleRegistration), now)
	flow := RegistrationFlow{
		ID: uuid.NewString(), TenantID: id.TenantID, UserID: id.UserID,
		Provider: ProviderFeishu, Status: FlowStarting,
		ExpiresAt: now.Add(registrationTTL), CreatedAt: now, UpdatedAt: now,
	}
	if err := s.store.CreateRegistrationFlow(ctx, flow); err != nil {
		return RegistrationFlow{}, err
	}

	connectorID := uuid.NewString()
	if existing, err := s.store.GetConnector(ctx, id); err == nil {
		connectorID = existing.ID
	} else if !errors.Is(err, ErrNotFound) {
		return RegistrationFlow{}, err
	}

	runCtx, cancel := context.WithTimeout(s.root, registrationTTL)
	s.mu.Lock()
	s.cancels[flow.ID] = cancel
	s.mu.Unlock()
	updated := make(chan struct{}, 1)
	go s.runRegistration(runCtx, flow, id, connectorID, updated)

	timer := time.NewTimer(registrationWait)
	defer timer.Stop()
	select {
	case <-updated:
	case <-timer.C:
	case <-ctx.Done():
		return RegistrationFlow{}, ctx.Err()
	}
	return s.store.GetRegistrationFlow(ctx, id, flow.ID)
}

func (s *Service) runRegistration(
	ctx context.Context,
	flow RegistrationFlow,
	id Identity,
	connectorID string,
	updated chan<- struct{},
) {
	defer func() {
		s.mu.Lock()
		delete(s.cancels, flow.ID)
		s.mu.Unlock()
	}()
	expiresAt := flow.ExpiresAt
	signalUpdate := func() {
		select {
		case updated <- struct{}{}:
		default:
		}
	}
	result, err := s.registrar.Register(ctx, RegistrationInput{
		AppName:   registrationAppName(id),
		AppDesc:   "通过飞书与 Cocola Agent 对话",
		AvatarURL: s.avatarURL,
	}, func(update RegistrationUpdate) {
		now := s.now()
		if update.ExpiresIn > 0 {
			candidate := now.Add(update.ExpiresIn)
			if candidate.Before(expiresAt) {
				expiresAt = candidate
			}
		}
		status := FlowAuthorizing
		if update.VerificationURL != "" {
			status = FlowAwaitingUser
		}
		if update.Status == FlowAuthorizing {
			status = FlowAuthorizing
		}
		if updateErr := s.store.UpdateRegistrationFlow(
			context.Background(),
			flow.ID,
			status,
			update.VerificationURL,
			"",
			expiresAt,
			now,
		); updateErr == nil {
			signalUpdate()
		}
	})
	if err != nil {
		s.finishRegistrationError(flow.ID, err, expiresAt)
		signalUpdate()
		return
	}
	if result.AppID == "" || result.AppSecret == "" || result.OwnerOpenID == "" {
		s.finishRegistrationError(
			flow.ID,
			&RegistrationError{Code: "invalid_registration_response"},
			expiresAt,
		)
		signalUpdate()
		return
	}
	domain := DomainFeishu
	if strings.EqualFold(strings.TrimSpace(result.TenantBrand), DomainLark) {
		domain = DomainLark
	}
	ciphertext, encryptErr := s.box.Encrypt(
		result.AppSecret,
		connectorSecretAAD(id.TenantID, id.UserID, connectorID),
	)
	if encryptErr != nil {
		s.finishRegistrationError(flow.ID, encryptErr, expiresAt)
		signalUpdate()
		return
	}
	now := s.now()
	completeErr := s.store.CompleteRegistration(context.Background(), id, flow.ID, Connector{
		ID: connectorID, TenantID: id.TenantID, UserID: id.UserID,
		Provider: ProviderFeishu, Domain: domain, AppID: result.AppID,
		AppSecretCiphertext: ciphertext, OwnerOpenID: result.OwnerOpenID,
		DesiredEnabled: true, Status: StatusConnecting,
		CreatedAt: now, UpdatedAt: now,
	}, now)
	if completeErr != nil && !errors.Is(completeErr, ErrFlowTerminated) {
		s.finishRegistrationError(flow.ID, completeErr, expiresAt)
	}
	if completeErr == nil {
		s.notify()
	}
	signalUpdate()
}

func (s *Service) finishRegistrationError(
	flowID string,
	err error,
	expiresAt time.Time,
) {
	if errors.Is(err, context.Canceled) {
		return
	}
	status := FlowFailed
	code := "registration_failed"
	var registrationErr *RegistrationError
	if errors.As(err, &registrationErr) && registrationErr.Code != "" {
		code = registrationErr.Code
	}
	if errors.Is(err, context.DeadlineExceeded) || code == "expired_token" {
		status = FlowExpired
		code = "expired_token"
	}
	if code == "access_denied" {
		status = FlowDenied
	}
	_ = s.store.UpdateRegistrationFlow(
		context.Background(),
		flowID,
		status,
		"",
		code,
		expiresAt,
		s.now(),
	)
}

func registrationAppName(id Identity) string {
	name := strings.TrimSpace(id.Name)
	if name == "" {
		name = strings.TrimSpace(id.Username)
	}
	if name == "" {
		name = "我的"
		return name + " Cocola"
	}
	runes := []rune(name)
	if len(runes) > 32 {
		name = string(runes[:32])
	}
	return name + " 的 Cocola"
}

func (s *Service) Registration(
	ctx context.Context,
	id Identity,
	flowID string,
) (RegistrationFlow, error) {
	now := s.now()
	_ = s.store.InterruptRegistrationFlows(ctx, now.Add(-staleRegistration), now)
	return s.store.GetRegistrationFlow(ctx, id, flowID)
}

func (s *Service) CancelRegistration(
	ctx context.Context,
	id Identity,
	flowID string,
) error {
	if err := s.store.CancelRegistrationFlow(ctx, id, flowID, s.now()); err != nil {
		return err
	}
	s.mu.Lock()
	cancel := s.cancels[flowID]
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return nil
}

func (s *Service) ConfigureManual(
	ctx context.Context,
	id Identity,
	domain string,
	appID string,
	appSecret string,
) (ManualResult, error) {
	domain = strings.ToLower(strings.TrimSpace(domain))
	appID = strings.TrimSpace(appID)
	appSecret = strings.TrimSpace(appSecret)
	if (domain != DomainFeishu && domain != DomainLark) ||
		appID == "" || len(appID) > maxAppIDLength ||
		appSecret == "" || len(appSecret) > maxAppSecretLength {
		return ManualResult{}, ErrInvalid
	}
	connectorID := uuid.NewString()
	createdAt := s.now()
	if existing, err := s.store.GetConnector(ctx, id); err == nil {
		connectorID = existing.ID
		createdAt = existing.CreatedAt
	} else if !errors.Is(err, ErrNotFound) {
		return ManualResult{}, err
	}
	ciphertext, err := s.box.Encrypt(
		appSecret,
		connectorSecretAAD(id.TenantID, id.UserID, connectorID),
	)
	if err != nil {
		return ManualResult{}, err
	}
	code, err := newBindCode()
	if err != nil {
		return ManualResult{}, err
	}
	now := s.now()
	expiresAt := now.Add(manualBindTTL)
	connector, err := s.store.UpsertConnector(ctx, Connector{
		ID: connectorID, TenantID: id.TenantID, UserID: id.UserID,
		Provider: ProviderFeishu, Domain: domain, AppID: appID,
		AppSecretCiphertext: ciphertext, DesiredEnabled: true,
		Status: StatusAwaitingBind, BindCodeHash: bindCodeHash(connectorID, code),
		BindExpiresAt: &expiresAt, CreatedAt: createdAt, UpdatedAt: now,
	})
	if err != nil {
		return ManualResult{}, err
	}
	s.notify()
	return ManualResult{
		Connection: connectorView(connector), BindCode: code, ExpiresAt: expiresAt,
	}, nil
}

func (s *Service) BindOwner(
	ctx context.Context,
	connectorID string,
	code string,
	ownerOpenID string,
) (Connector, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	ownerOpenID = strings.TrimSpace(ownerOpenID)
	if code == "" || ownerOpenID == "" {
		return Connector{}, ErrInvalid
	}
	connector, err := s.store.BindConnectorOwner(
		ctx,
		connectorID,
		bindCodeHash(connectorID, code),
		ownerOpenID,
		s.now(),
	)
	return connector, err
}

func (s *Service) Enable(ctx context.Context, id Identity) (ConnectorView, error) {
	connector, err := s.store.SetConnectorEnabled(ctx, id, true, s.now())
	if err != nil {
		return ConnectorView{}, err
	}
	s.notify()
	return connectorView(connector), nil
}

func (s *Service) Disable(ctx context.Context, id Identity) (ConnectorView, error) {
	connector, err := s.store.SetConnectorEnabled(ctx, id, false, s.now())
	if err != nil {
		return ConnectorView{}, err
	}
	s.notify()
	return connectorView(connector), nil
}

func (s *Service) ConfigureModel(
	ctx context.Context,
	id Identity,
	modelRouteID string,
	modelAlias string,
) (ConnectorView, error) {
	modelRouteID = strings.TrimSpace(modelRouteID)
	modelAlias = strings.TrimSpace(modelAlias)
	if modelRouteID == "" ||
		len(modelRouteID) > maxModelIDLength ||
		modelAlias == "" ||
		len(modelAlias) > maxModelAliasLength ||
		strings.ContainsAny(modelRouteID+modelAlias, "\x00\r\n") {
		return ConnectorView{}, ErrInvalid
	}
	connector, err := s.store.SetConnectorModel(
		ctx,
		id,
		modelRouteID,
		modelAlias,
		s.now(),
	)
	if err != nil {
		return ConnectorView{}, err
	}
	return connectorView(connector), nil
}

func (s *Service) Disconnect(ctx context.Context, id Identity) error {
	if err := s.store.DeleteConnector(ctx, id); err != nil {
		return err
	}
	s.notify()
	return nil
}

func (s *Service) AppSecret(connector Connector) (string, error) {
	return s.box.Decrypt(
		connector.AppSecretCiphertext,
		connectorSecretAAD(connector.TenantID, connector.UserID, connector.ID),
	)
}

func connectorSecretAAD(tenantID, userID, connectorID string) []byte {
	return []byte(fmt.Sprintf(
		"cocola:feishu_connector:%s:%s:%s:app_secret",
		tenantID,
		userID,
		connectorID,
	))
}

func newBindCode() (string, error) {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	code := make([]byte, len(raw))
	for index, value := range raw {
		code[index] = alphabet[int(value)%len(alphabet)]
	}
	return string(code), nil
}

func bindCodeHash(connectorID, code string) string {
	sum := sha256.Sum256([]byte(connectorID + "\x00" + strings.ToUpper(code)))
	return hex.EncodeToString(sum[:])
}
