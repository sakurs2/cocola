// HTTP-layer tests exercise the full router through httptest.ResponseRecorder —
// no port is bound (network listeners are forbidden), the handler is invoked
// in-process. We cover: auth gating (enabled vs disabled), the token mint+list+
// revoke+denylist loop, quota upsert/list/delete with validation, skill CRUD +
// enable/disable, the audit trail, and the error envelope mapping.
package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cocola-project/cocola/apps/admin-api/internal/service"
	"github.com/cocola-project/cocola/apps/admin-api/internal/store"
	"github.com/cocola-project/cocola/packages/go-common/token"
)

// fixedClock returns a deterministic time so audit/issued timestamps are stable.
func fixedClock() time.Time { return time.Unix(1_700_000_000, 0).UTC() }

func newTestAPI(adminKey string) *API {
	mem := store.NewMemory()
	iss := token.NewIssuer("test-secret", "cocola", 24*time.Hour)
	svc := service.New(mem, iss, fixedClock).WithConfigSecretKey("config-secret")
	return New(svc, adminKey)
}

type fakeNodeManager struct {
	offlineForce bool
	maxPods      *int
}

func (f *fakeNodeManager) ListNodes(context.Context) (service.SandboxNodeList, error) {
	return service.SandboxNodeList{Nodes: []service.SandboxNode{{
		Name:              "node-a",
		Status:            "active",
		Ready:             true,
		Schedulable:       true,
		CPUCapacity:       "4",
		MemoryCapacity:    "8Gi",
		CPUAllocatable:    "3900m",
		MemoryAllocatable: "7Gi",
		SandboxPods:       2,
		MaxSandboxPods:    f.maxPods,
	}}}, nil
}

func (f *fakeNodeManager) DisableNode(context.Context, string) (service.SandboxNode, error) {
	return service.SandboxNode{Name: "node-a", Status: "disabled"}, nil
}

func (f *fakeNodeManager) RestoreNode(context.Context, string) (service.SandboxNode, error) {
	return service.SandboxNode{Name: "node-a", Status: "active"}, nil
}

func (f *fakeNodeManager) SetMaxSandboxPods(_ context.Context, _ string, max *int) (service.SandboxNode, error) {
	f.maxPods = max
	return service.SandboxNode{Name: "node-a", Status: "active", SandboxPods: 2, MaxSandboxPods: max}, nil
}

func (f *fakeNodeManager) OfflineNode(_ context.Context, _ string, force bool) (service.OfflineNodeResult, error) {
	f.offlineForce = force
	if !force {
		return service.OfflineNodeResult{
			Node:        service.SandboxNode{Name: "node-a", Status: "offline_pending", SandboxPods: 1},
			PendingPods: []string{"sandbox-pod-a"},
			Message:     "confirm force",
		}, nil
	}
	return service.OfflineNodeResult{Node: service.SandboxNode{Name: "node-a", Status: "offline_pending"}, Message: "ok"}, nil
}

func (f *fakeNodeManager) JoinCommand(context.Context) (service.JoinCommand, error) {
	return service.JoinCommand{Command: "k3s agent join", Note: "demo"}, nil
}

type fakeRuntimeManager struct{}

func (f fakeRuntimeManager) ListSandboxes(context.Context) (service.SandboxRuntimeList, error) {
	return service.SandboxRuntimeList{Sandboxes: []service.SandboxRuntime{{
		SandboxID:      "sb-1",
		SessionID:      "conv-1",
		UserID:         "alice@example.com",
		Username:       "alice",
		Status:         "running",
		LifecycleState: "active",
		PodName:        "pod-1",
		PodPhase:       "Running",
		NodeName:       "node-a",
	}}}, nil
}

type fakeArchitectureChecker struct {
	http map[string]bool
	tcp  map[string]bool
}

func (f fakeArchitectureChecker) CheckHTTP(_ context.Context, url string) bool {
	return f.http[url]
}

func (f fakeArchitectureChecker) CheckTCP(_ context.Context, addr string) bool {
	return f.tcp[addr]
}

func newTestNodeAPI(adminKey string, mgr service.SandboxNodeManager) *API {
	mem := store.NewMemory()
	iss := token.NewIssuer("test-secret", "cocola", 24*time.Hour)
	svc := service.New(mem, iss, fixedClock).WithSandboxNodeManager(mgr)
	return New(svc, adminKey)
}

func newTestRuntimeAPI(adminKey string, mgr service.SandboxRuntimeManager) *API {
	mem := store.NewMemory()
	iss := token.NewIssuer("test-secret", "cocola", 24*time.Hour)
	svc := service.New(mem, iss, fixedClock).WithSandboxRuntimeManager(mgr)
	return New(svc, adminKey)
}

func newTestArchitectureAPI(adminKey string, checker service.ArchitectureHealthChecker) *API {
	mem := store.NewMemory()
	iss := token.NewIssuer("test-secret", "cocola", 24*time.Hour)
	svc := service.New(mem, iss, fixedClock).
		WithSandboxNodeManager(&fakeNodeManager{}).
		WithSandboxRuntimeManager(fakeRuntimeManager{}).
		WithArchitectureHealthChecker(checker)
	return New(svc, adminKey)
}

// do issues a request against the router and returns the recorder.
func do(t *testing.T, h http.Handler, method, path, key string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var rdr *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		rdr = bytes.NewReader(b)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, rdr)
	if key != "" {
		req.Header.Set("authorization", "Bearer "+key)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func doAs(t *testing.T, h http.Handler, method, path, key, actor string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var rdr *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		rdr = bytes.NewReader(b)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, rdr)
	if key != "" {
		req.Header.Set("authorization", "Bearer "+key)
	}
	if actor != "" {
		req.Header.Set("x-cocola-admin", actor)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestHealthNoAuth(t *testing.T) {
	api := newTestAPI("s3cr3t")
	rec := do(t, api.Router(), http.MethodGet, "/healthz", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("health: want 200, got %d", rec.Code)
	}
}

func TestAdminAuthRequired(t *testing.T) {
	api := newTestAPI("s3cr3t")
	r := api.Router()

	// no key -> 401
	if rec := do(t, r, http.MethodGet, "/admin/tokens", "", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing key: want 401, got %d", rec.Code)
	}
	// wrong key -> 401
	if rec := do(t, r, http.MethodGet, "/admin/tokens", "nope", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong key: want 401, got %d", rec.Code)
	}
	// right key -> 200
	if rec := do(t, r, http.MethodGet, "/admin/tokens", "s3cr3t", nil); rec.Code != http.StatusOK {
		t.Fatalf("right key: want 200, got %d", rec.Code)
	}
}

func TestAuthDisabledWhenNoKey(t *testing.T) {
	api := newTestAPI("") // disabled
	rec := do(t, api.Router(), http.MethodGet, "/admin/tokens", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("auth-disabled: want 200, got %d", rec.Code)
	}
}

func TestAdminSettingsAPI(t *testing.T) {
	api := newTestAPI("k")
	r := api.Router()

	rec := do(t, r, http.MethodGet, "/admin/settings", "k", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list settings: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var listed struct {
		Settings []service.SystemSettingView `json:"settings"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode settings: %v", err)
	}
	if len(listed.Settings) == 0 {
		t.Fatal("expected settings")
	}

	rec = do(t, r, http.MethodPatch, "/admin/settings/scheduler.poll_secs", "k", map[string]any{
		"value": 5, "expected_version": 0,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("patch poll setting: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var updated service.SystemSettingView
	if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode updated setting: %v", err)
	}
	if updated.Source != "db" || updated.Value.(float64) != 5 || updated.Version != 1 {
		t.Fatalf("bad updated setting: %+v", updated)
	}

	rec = do(t, r, http.MethodPatch, "/admin/settings/scheduler.min_interval_secs", "k", map[string]any{
		"value": 1800, "expected_version": 0,
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("too-small min interval: want 400, got %d (%s)", rec.Code, rec.Body.String())
	}
	rec = do(t, r, http.MethodPatch, "/admin/settings/auth.secret", "k", map[string]any{
		"value": "new-secret", "expected_version": 0,
	})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("secret update: want 403, got %d (%s)", rec.Code, rec.Body.String())
	}

	rec = do(t, r, http.MethodDelete, "/admin/settings/scheduler.poll_secs?expected_version=1", "k", nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("reset poll setting: want 204, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestAuthUsersLoginAndRuntimeToken(t *testing.T) {
	api := newTestAPI("k")
	r := api.Router()

	// Public login misses until an admin creates the user.
	rec := do(t, r, http.MethodPost, "/auth/login", "", map[string]any{
		"email": "alice@example.com", "password": "pw",
	})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unknown login: want 401, got %d", rec.Code)
	}

	// User management is admin-key protected.
	rec = do(t, r, http.MethodPost, "/admin/users", "", map[string]any{
		"username": "alice", "email": "alice@example.com", "password": "pw", "role": "user",
	})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("create user without key: want 401, got %d", rec.Code)
	}

	rec = do(t, r, http.MethodPost, "/admin/users", "k", map[string]any{
		"username": "Alice", "email": "Alice@Example.COM", "password": "pw", "role": "user",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create auth user: want 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	var created store.AuthUser
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created user: %v", err)
	}
	if created.Username != "alice" || created.Email != "alice@example.com" || created.PasswordHash != "" {
		t.Fatalf("public user JSON should be normalized and hide password hash: %+v", created)
	}
	for name, payload := range map[string]map[string]any{
		"duplicate username":          {"username": "ALICE", "email": "alice2@example.com", "password": "pw", "role": "user"},
		"duplicate email":             {"username": "alice2", "email": "ALICE@example.com", "password": "pw", "role": "user"},
		"cross-kind duplicate handle": {"username": "ALICE@example.com", "email": "alice3@example.com", "password": "pw", "role": "user"},
	} {
		rec = do(t, r, http.MethodPost, "/admin/users", "k", payload)
		if rec.Code != http.StatusConflict {
			t.Fatalf("%s: want 409, got %d (%s)", name, rec.Code, rec.Body.String())
		}
		var body errBody
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("%s: decode error response: %v", name, err)
		}
		if body.Error.Message != "username or email already exists" {
			t.Fatalf("%s: wrong message %q", name, body.Error.Message)
		}
	}
	rec = do(t, r, http.MethodGet, "/admin/users/lookup?email=Alice%40Example.COM", "k", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("lookup auth user: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}

	rec = do(t, r, http.MethodPost, "/auth/login", "", map[string]any{
		"identifier": "ALICE@example.com", "password": "pw",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("login: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var login struct {
		User service.LoginResult `json:"user"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &login)
	if login.User.ID != created.ID || login.User.Username != "alice" || login.User.Role != "user" {
		t.Fatalf("login user payload wrong: %+v", login.User)
	}
	rec = do(t, r, http.MethodPost, "/auth/login", "", map[string]any{
		"identifier": "alice", "password": "pw",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("username login: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}

	rec = do(t, r, http.MethodPost, "/admin/runtime-token", "k", map[string]any{
		"user_id": login.User.Email,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("runtime token: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var rt struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &rt)
	if rt.Token == "" {
		t.Fatalf("runtime token empty")
	}
	if _, err := token.Decode(rt.Token, "test-secret", fixedClock().Unix()); err != nil {
		t.Fatalf("runtime token not decodable: %v", err)
	}

	rec = do(t, r, http.MethodPatch, "/admin/users/"+created.ID, "k", map[string]any{"enabled": false})
	if rec.Code != http.StatusOK {
		t.Fatalf("disable auth user: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	rec = do(t, r, http.MethodPost, "/auth/login", "", map[string]any{
		"email": "alice@example.com", "password": "pw",
	})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("disabled login: want 403, got %d", rec.Code)
	}
	var disabledBody errBody
	if err := json.Unmarshal(rec.Body.Bytes(), &disabledBody); err != nil {
		t.Fatalf("disabled login error body: %v", err)
	}
	if disabledBody.Error.Code != "ACCOUNT_DISABLED" {
		t.Fatalf("disabled login code: %+v", disabledBody)
	}
	rec = do(t, r, http.MethodPost, "/admin/runtime-token", "k", map[string]any{
		"user_id": login.User.Email,
	})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("disabled runtime token: want 403, got %d (%s)", rec.Code, rec.Body.String())
	}

	rec = do(t, r, http.MethodPatch, "/admin/users/"+created.ID, "k", map[string]any{"enabled": true})
	if rec.Code != http.StatusOK {
		t.Fatalf("reenable auth user: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	rec = do(t, r, http.MethodDelete, "/admin/users/"+created.ID, "k", nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete auth user: want 204, got %d (%s)", rec.Code, rec.Body.String())
	}
	rec = do(t, r, http.MethodGet, "/admin/users", "k", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list after delete: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var listed struct {
		Users []store.AuthUser `json:"users"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &listed)
	if len(listed.Users) != 0 {
		t.Fatalf("deleted user should be hidden from list: %+v", listed.Users)
	}
	rec = do(t, r, http.MethodPost, "/auth/login", "", map[string]any{
		"identifier": "alice", "password": "pw",
	})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("deleted login: want 403, got %d (%s)", rec.Code, rec.Body.String())
	}
	rec = do(t, r, http.MethodPost, "/admin/users", "k", map[string]any{
		"username": "alice-new", "email": "alice@example.com", "password": "pw", "role": "user",
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("deleted user email should still conflict: got %d (%s)", rec.Code, rec.Body.String())
	}
	rec = do(t, r, http.MethodDelete, "/admin/users/"+created.ID, "k", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("delete already deleted user: want 404, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestProtectedBootstrapAdminCannotBeDemotedDisabledOrDeleted(t *testing.T) {
	api := newTestAPI("k")
	if err := api.svc.BootstrapAdmin(context.Background(), service.BootstrapAdminInput{
		Username: "admin",
		Email:    "admin@example.com",
		Password: "pw",
		Actor:    "bootstrap",
	}); err != nil {
		t.Fatalf("bootstrap admin: %v", err)
	}
	rec := do(t, api.Router(), http.MethodGet, "/admin/users/lookup?email=admin%40example.com", "k", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("lookup bootstrap admin: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var admin store.AuthUser
	if err := json.Unmarshal(rec.Body.Bytes(), &admin); err != nil {
		t.Fatalf("decode bootstrap admin: %v", err)
	}

	for name, req := range map[string]struct {
		method string
		body   any
	}{
		"downgrade": {method: http.MethodPatch, body: map[string]any{"role": "user"}},
		"disable":   {method: http.MethodPatch, body: map[string]any{"enabled": false}},
		"delete":    {method: http.MethodDelete},
	} {
		rec = do(t, api.Router(), req.method, "/admin/users/"+admin.ID, "k", req.body)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s: want 403, got %d (%s)", name, rec.Code, rec.Body.String())
		}
		var body errBody
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("%s: decode error body: %v", name, err)
		}
		if body.Error.Code != "PROTECTED_ADMIN" {
			t.Fatalf("%s: wrong error body: %+v", name, body)
		}
	}
}

func TestAdminCannotChangeOwnPermissionsHTTP(t *testing.T) {
	api := newTestAPI("k")
	r := api.Router()

	rec := doAs(t, r, http.MethodPost, "/admin/users", "k", "owner@example.com", map[string]any{
		"username": "self-admin", "email": "self@example.com", "password": "pw", "role": "admin",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create admin user: want 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	var admin store.AuthUser
	if err := json.Unmarshal(rec.Body.Bytes(), &admin); err != nil {
		t.Fatalf("decode admin user: %v", err)
	}

	for name, req := range map[string]struct {
		method string
		body   any
	}{
		"downgrade": {method: http.MethodPatch, body: map[string]any{"role": "user"}},
		"disable":   {method: http.MethodPatch, body: map[string]any{"enabled": false}},
		"delete":    {method: http.MethodDelete},
	} {
		rec = doAs(t, r, req.method, "/admin/users/"+admin.ID, "k", "SELF@example.com", req.body)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s: want 403, got %d (%s)", name, rec.Code, rec.Body.String())
		}
		var body errBody
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("%s: decode error body: %v", name, err)
		}
		if body.Error.Code != "SELF_PERMISSION_CHANGE" {
			t.Fatalf("%s: wrong error body: %+v", name, body)
		}
	}
}

func TestTokenLifecycle(t *testing.T) {
	api := newTestAPI("k")
	r := api.Router()

	// issue
	rec := do(t, r, http.MethodPost, "/admin/tokens", "k", map[string]any{
		"user_id": "alice", "tenant": "acme",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("issue: want 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	var issued service.IssueTokenResult
	if err := json.Unmarshal(rec.Body.Bytes(), &issued); err != nil {
		t.Fatalf("decode issue: %v", err)
	}
	if issued.Token == "" || issued.Record.ID == "" {
		t.Fatalf("issue: empty token or id: %+v", issued)
	}
	if issued.Record.UserID != "alice" || issued.Record.TenantID != "acme" {
		t.Fatalf("issue: claims not reflected: %+v", issued.Record)
	}

	// verify the minted token is decodable with the shared secret
	claims, err := token.Decode(issued.Token, "test-secret", fixedClock().Unix())
	if err != nil {
		t.Fatalf("minted token not verifiable: %v", err)
	}
	if claims.Subject != "alice" {
		t.Fatalf("decoded sub: want alice, got %q", claims.Subject)
	}
	// The denylist closes only if the token's jti IS the persisted record id.
	if claims.ID == "" || claims.ID != issued.Record.ID {
		t.Fatalf("jti must equal record id: jti=%q record=%q", claims.ID, issued.Record.ID)
	}

	// list -> 1 entry
	rec = do(t, r, http.MethodGet, "/admin/tokens", "k", nil)
	var listed struct {
		Tokens []store.TokenRecord `json:"tokens"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &listed)
	if len(listed.Tokens) != 1 {
		t.Fatalf("list: want 1, got %d", len(listed.Tokens))
	}

	// revoke
	rec = do(t, r, http.MethodDelete, "/admin/tokens/"+issued.Record.ID, "k", nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("revoke: want 204, got %d", rec.Code)
	}

	// denylist now contains the id
	rec = do(t, r, http.MethodGet, "/admin/tokens/revoked", "k", nil)
	var dl struct {
		Revoked []string `json:"revoked"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &dl)
	if len(dl.Revoked) != 1 || dl.Revoked[0] != issued.Record.ID {
		t.Fatalf("denylist: want [%s], got %v", issued.Record.ID, dl.Revoked)
	}

	// revoking an unknown id -> 404
	rec = do(t, r, http.MethodDelete, "/admin/tokens/does-not-exist", "k", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("revoke unknown: want 404, got %d", rec.Code)
	}
}

func TestIssueTokenValidation(t *testing.T) {
	api := newTestAPI("k")
	r := api.Router()
	// missing user_id -> 400
	rec := do(t, r, http.MethodPost, "/admin/tokens", "k", map[string]any{"tenant": "acme"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing user_id: want 400, got %d", rec.Code)
	}
	// unknown field -> 400 (DisallowUnknownFields)
	rec = do(t, r, http.MethodPost, "/admin/tokens", "k", map[string]any{"user_id": "a", "bogus": 1})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown field: want 400, got %d", rec.Code)
	}
}

func TestQuotaCRUD(t *testing.T) {
	api := newTestAPI("k")
	r := api.Router()

	// upsert user quota
	rec := do(t, r, http.MethodPut, "/admin/quotas", "k", map[string]any{
		"scope": "user", "subject": "alice", "limit": 1000,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("set quota: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}

	// invalid scope -> 400
	rec = do(t, r, http.MethodPut, "/admin/quotas", "k", map[string]any{
		"scope": "org", "subject": "x", "limit": 1,
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad scope: want 400, got %d", rec.Code)
	}

	// list -> 1
	rec = do(t, r, http.MethodGet, "/admin/quotas", "k", nil)
	var ql struct {
		Quotas []store.QuotaOverride `json:"quotas"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &ql)
	if len(ql.Quotas) != 1 || ql.Quotas[0].Limit != 1000 {
		t.Fatalf("list quotas: unexpected %+v", ql.Quotas)
	}

	// delete
	rec = do(t, r, http.MethodDelete, "/admin/quotas/user/alice", "k", nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete quota: want 204, got %d", rec.Code)
	}
	// delete again -> 404
	rec = do(t, r, http.MethodDelete, "/admin/quotas/user/alice", "k", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("delete missing quota: want 404, got %d", rec.Code)
	}
}

func TestSkillCRUD(t *testing.T) {
	api := newTestAPI("k")
	r := api.Router()

	// create
	rec := do(t, r, http.MethodPost, "/admin/skills", "k", map[string]any{
		"id": "web-search", "name": "Web Search", "version": "1.0.0",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create skill: want 201, got %d (%s)", rec.Code, rec.Body.String())
	}

	// duplicate -> 409
	rec = do(t, r, http.MethodPost, "/admin/skills", "k", map[string]any{
		"id": "web-search", "name": "Dup",
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("dup skill: want 409, got %d", rec.Code)
	}

	// get
	rec = do(t, r, http.MethodGet, "/admin/skills/web-search", "k", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get skill: want 200, got %d", rec.Code)
	}

	// enable
	rec = do(t, r, http.MethodPost, "/admin/skills/web-search/enable", "k", nil)
	var s store.Skill
	_ = json.Unmarshal(rec.Body.Bytes(), &s)
	if rec.Code != http.StatusOK || !s.Enabled {
		t.Fatalf("enable: code=%d enabled=%v", rec.Code, s.Enabled)
	}

	// list enabled -> 1
	rec = do(t, r, http.MethodGet, "/admin/skills?enabled=true", "k", nil)
	var sl struct {
		Skills []store.Skill `json:"skills"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &sl)
	if len(sl.Skills) != 1 {
		t.Fatalf("list enabled: want 1, got %d", len(sl.Skills))
	}

	// disable -> list enabled now 0
	_ = do(t, r, http.MethodPost, "/admin/skills/web-search/disable", "k", nil)
	rec = do(t, r, http.MethodGet, "/admin/skills?enabled=true", "k", nil)
	sl.Skills = nil
	_ = json.Unmarshal(rec.Body.Bytes(), &sl)
	if len(sl.Skills) != 0 {
		t.Fatalf("after disable: want 0 enabled, got %d", len(sl.Skills))
	}

	// delete
	rec = do(t, r, http.MethodDelete, "/admin/skills/web-search", "k", nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete skill: want 204, got %d", rec.Code)
	}
	// get -> 404
	rec = do(t, r, http.MethodGet, "/admin/skills/web-search", "k", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("get deleted: want 404, got %d", rec.Code)
	}
}

func TestMCPCRUDAndEffectiveConfig(t *testing.T) {
	api := newTestAPI("k")
	r := api.Router()

	rec := do(t, r, http.MethodPost, "/admin/mcps", "k", map[string]any{
		"id":              "github",
		"name":            "GitHub",
		"transport":       "stdio",
		"command":         "npx",
		"args":            []string{"-y", "@modelcontextprotocol/server-github"},
		"env":             map[string]string{"GITHUB_TOKEN": "ghp_secret123"},
		"default_enabled": true,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create mcp: want 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	if bytes.Contains(rec.Body.Bytes(), []byte("ghp_secret123")) {
		t.Fatal("create response leaked plaintext secret")
	}

	rec = do(t, r, http.MethodPost, "/admin/mcps", "k", map[string]any{
		"id":              "amap",
		"name":            "Amap",
		"transport":       "http",
		"url":             "https://mcp.amap.com/mcp?key=${AMAP_KEY}",
		"url_vars":        map[string]string{"AMAP_KEY": "amap_secret456"},
		"default_enabled": true,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create amap mcp: want 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	if bytes.Contains(rec.Body.Bytes(), []byte("amap_secret456")) {
		t.Fatal("create response leaked URL var secret")
	}

	rec = do(t, r, http.MethodGet, "/admin/mcps", "k", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list mcps: want 200, got %d", rec.Code)
	}
	if bytes.Contains(rec.Body.Bytes(), []byte("ghp_secret123")) ||
		bytes.Contains(rec.Body.Bytes(), []byte("amap_secret456")) {
		t.Fatal("list response leaked plaintext secret")
	}
	var listed struct {
		MCPs []store.MCPServer `json:"mcps"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listed.MCPs) != 2 {
		t.Fatalf("expected 2 MCPs, got %+v", listed.MCPs)
	}
	if !bytes.Contains(listed.MCPs[1].EnvHintJSON, []byte("GITHUB_TOKEN")) {
		t.Fatalf("expected masked env hint, got %+v", listed.MCPs)
	}
	if !bytes.Contains(listed.MCPs[0].URLVarHintJSON, []byte("AMAP_KEY")) {
		t.Fatalf("expected masked URL var hint, got %+v", listed.MCPs)
	}

	rec = do(t, r, http.MethodGet, "/admin/mcps/effective?user_id=alice", "k", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("effective mcps: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var effective struct {
		MCPServers map[string]map[string]any `json:"mcp_servers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &effective); err != nil {
		t.Fatalf("decode effective: %v", err)
	}
	github := effective.MCPServers["github"]
	if github["command"] != "npx" {
		t.Fatalf("runtime command = %#v", github["command"])
	}
	env, ok := github["env"].(map[string]any)
	if !ok || env["GITHUB_TOKEN"] != "ghp_secret123" {
		t.Fatalf("runtime env not decrypted: %#v", github["env"])
	}
	amap := effective.MCPServers["amap"]
	if amap["url"] != "https://mcp.amap.com/mcp?key=amap_secret456" {
		t.Fatalf("runtime URL not rendered: %#v", amap["url"])
	}

	rec = do(t, r, http.MethodDelete, "/admin/mcps/github", "k", nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete mcp: want 204, got %d", rec.Code)
	}
}

func TestAgentPromptGlobalAndEffective(t *testing.T) {
	api := newTestAPI("k")
	r := api.Router()

	rec := do(t, r, http.MethodGet, "/admin/agent-prompts/global", "k", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get default prompt: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var prompt store.AgentPrompt
	if err := json.Unmarshal(rec.Body.Bytes(), &prompt); err != nil {
		t.Fatalf("decode default prompt: %v", err)
	}
	if prompt.ID != service.GlobalAgentPromptID || prompt.Enabled || prompt.Content != "" {
		t.Fatalf("bad default prompt: %+v", prompt)
	}

	rec = doAs(t, r, http.MethodPatch, "/admin/agent-prompts/global", "k", "admin@example.com", map[string]any{
		"content": "Prefer concise answers.",
		"enabled": true,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("update prompt: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	prompt = store.AgentPrompt{}
	if err := json.Unmarshal(rec.Body.Bytes(), &prompt); err != nil {
		t.Fatalf("decode updated prompt: %v", err)
	}
	if !prompt.Enabled || prompt.Version != 1 || prompt.Content != "Prefer concise answers." {
		t.Fatalf("bad updated prompt: %+v", prompt)
	}

	rec = do(t, r, http.MethodGet, "/admin/agent-prompts/effective?user_id=alice", "k", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("effective prompt: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var effective service.AgentPromptRuntimeConfig
	if err := json.Unmarshal(rec.Body.Bytes(), &effective); err != nil {
		t.Fatalf("decode effective prompt: %v", err)
	}
	if effective.SystemPrompt != "Prefer concise answers." {
		t.Fatalf("bad effective prompt: %+v", effective)
	}
	if len(effective.Prompts) != 1 || effective.Prompts[0].ID != service.GlobalAgentPromptID {
		t.Fatalf("bad effective markers: %+v", effective.Prompts)
	}

	rec = do(t, r, http.MethodPost, "/admin/agent-prompts/global/disable", "k", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("disable prompt: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	rec = do(t, r, http.MethodGet, "/admin/agent-prompts/effective?user_id=alice", "k", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("effective disabled prompt: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	effective = service.AgentPromptRuntimeConfig{}
	if err := json.Unmarshal(rec.Body.Bytes(), &effective); err != nil {
		t.Fatalf("decode disabled effective prompt: %v", err)
	}
	if effective.SystemPrompt != "" || len(effective.Prompts) != 0 {
		t.Fatalf("disabled prompt should not be effective: %+v", effective)
	}
}

func TestArchitectureGraph(t *testing.T) {
	t.Setenv("COCOLA_GATEWAY_URL", "http://gateway.local:8080")
	t.Setenv("COCOLA_LLM_GATEWAY_URL", "http://llm-gateway.local:8080")
	t.Setenv("COCOLA_OPENSANDBOX_URL", "http://opensandbox.local:8090/v1")
	t.Setenv("COCOLA_MINIO_ENDPOINT", "minio.local:9000")
	t.Setenv("COCOLA_REDIS_ADDR", "redis.local:6379")
	t.Setenv("COCOLA_PG_DSN", "postgres://cocola:secret@postgres.local:5432/cocola?sslmode=disable")
	t.Setenv("COCOLA_AGENT_ADDR", "agent-runtime.local:50061")
	t.Setenv("COCOLA_SANDBOX_ADDR", "sandbox-manager.local:50051")

	api := newTestArchitectureAPI("k", fakeArchitectureChecker{
		http: map[string]bool{
			"http://gateway.local:8080/healthz":         true,
			"http://llm-gateway.local:8080/healthz":     true,
			"http://opensandbox.local:8090/health":      true,
			"http://minio.local:9000/minio/health/live": false,
		},
		tcp: map[string]bool{
			"agent-runtime.local:50061":   true,
			"sandbox-manager.local:50051": false,
			"postgres.local:5432":         true,
			"redis.local:6379":            true,
		},
	})
	rec := do(t, api.Router(), http.MethodGet, "/admin/architecture", "k", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("architecture: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var graph service.ArchitectureGraph
	if err := json.Unmarshal(rec.Body.Bytes(), &graph); err != nil {
		t.Fatalf("decode architecture: %v", err)
	}
	if len(graph.Nodes) != 11 {
		t.Fatalf("expected 11 nodes, got %d", len(graph.Nodes))
	}
	if len(graph.Edges) == 0 {
		t.Fatal("expected edges")
	}
	nodes := map[string]service.ArchitectureNode{}
	for _, node := range graph.Nodes {
		nodes[node.ID] = node
	}
	if nodes["postgres"].Status != service.ArchitectureHealthy {
		t.Fatalf("postgres status = %q", nodes["postgres"].Status)
	}
	if nodes["minio"].Status != service.ArchitectureUnhealthy {
		t.Fatalf("minio status = %q", nodes["minio"].Status)
	}
	if nodes["sandbox-manager"].Status != service.ArchitectureUnhealthy {
		t.Fatalf("sandbox-manager status = %q", nodes["sandbox-manager"].Status)
	}
	if nodes["user-sandboxes"].Metadata["running_sandboxes"].(float64) != 1 {
		t.Fatalf("bad sandbox metadata: %+v", nodes["user-sandboxes"].Metadata)
	}
	if bytes.Contains(rec.Body.Bytes(), []byte("secret")) {
		t.Fatal("architecture response leaked postgres secret")
	}
	if bytes.Contains(rec.Body.Bytes(), []byte("latency")) || bytes.Contains(rec.Body.Bytes(), []byte("recent_error")) {
		t.Fatal("architecture response should not include latency or recent errors")
	}
}

func TestArchitectureUnconfiguredInfraIsUnknown(t *testing.T) {
	t.Setenv("COCOLA_MINIO_ENDPOINT", "")
	t.Setenv("COCOLA_REDIS_ADDR", "")
	t.Setenv("COCOLA_PG_DSN", "")

	api := newTestArchitectureAPI("k", fakeArchitectureChecker{})
	rec := do(t, api.Router(), http.MethodGet, "/admin/architecture", "k", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("architecture: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var graph service.ArchitectureGraph
	if err := json.Unmarshal(rec.Body.Bytes(), &graph); err != nil {
		t.Fatalf("decode architecture: %v", err)
	}
	nodes := map[string]service.ArchitectureNode{}
	for _, node := range graph.Nodes {
		nodes[node.ID] = node
	}
	for _, id := range []string{"postgres", "redis", "minio"} {
		if nodes[id].Status != service.ArchitectureUnknown {
			t.Fatalf("%s status = %q", id, nodes[id].Status)
		}
	}
}

func TestSandboxNodeRoutes(t *testing.T) {
	mgr := &fakeNodeManager{}
	api := newTestNodeAPI("k", mgr)
	r := api.Router()

	rec := do(t, r, http.MethodGet, "/admin/sandbox-nodes", "k", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list nodes: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var listed service.SandboxNodeList
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode nodes: %v", err)
	}
	if len(listed.Nodes) != 1 || listed.Nodes[0].Name != "node-a" || listed.Nodes[0].SandboxPods != 2 {
		t.Fatalf("unexpected nodes: %+v", listed.Nodes)
	}

	rec = do(t, r, http.MethodGet, "/admin/sandbox-nodes/join-command", "k", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("join command: want 200, got %d", rec.Code)
	}
	var join service.JoinCommand
	_ = json.Unmarshal(rec.Body.Bytes(), &join)
	if join.Command == "" {
		t.Fatalf("empty join command")
	}

	rec = do(t, r, http.MethodPatch, "/admin/sandbox-nodes/node-a/capacity", "k", map[string]any{"max_sandbox_pods": 3})
	if rec.Code != http.StatusOK {
		t.Fatalf("set capacity: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var capacity service.SandboxNode
	if err := json.Unmarshal(rec.Body.Bytes(), &capacity); err != nil {
		t.Fatalf("decode capacity: %v", err)
	}
	if capacity.MaxSandboxPods == nil || *capacity.MaxSandboxPods != 3 {
		t.Fatalf("capacity max = %+v, want 3", capacity.MaxSandboxPods)
	}

	rec = do(t, r, http.MethodPost, "/admin/sandbox-nodes/node-a/offline", "k", map[string]any{"force": true})
	if rec.Code != http.StatusOK {
		t.Fatalf("offline: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if !mgr.offlineForce {
		t.Fatalf("offline did not pass force=true")
	}

	rec = do(t, r, http.MethodPost, "/admin/sandbox-nodes/node-a/offline", "k", map[string]any{"force": false})
	if rec.Code != http.StatusOK {
		t.Fatalf("offline pending: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var pending service.OfflineNodeResult
	if err := json.Unmarshal(rec.Body.Bytes(), &pending); err != nil {
		t.Fatalf("decode offline pending: %v", err)
	}
	if len(pending.PendingPods) != 1 || pending.PendingPods[0] != "sandbox-pod-a" {
		t.Fatalf("offline pending pods wrong: %+v", pending)
	}
}

func TestSandboxNodeRoutesNotConfigured(t *testing.T) {
	api := newTestAPI("k")
	rec := do(t, api.Router(), http.MethodGet, "/admin/sandbox-nodes", "k", nil)
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("not configured: want 501, got %d", rec.Code)
	}
}

func TestSandboxRuntimeRoutes(t *testing.T) {
	api := newTestRuntimeAPI("k", fakeRuntimeManager{})
	rec := do(t, api.Router(), http.MethodGet, "/admin/sandboxes", "k", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list sandboxes: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var listed service.SandboxRuntimeList
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode sandboxes: %v", err)
	}
	if len(listed.Sandboxes) != 1 || listed.Sandboxes[0].SandboxID != "sb-1" || listed.Sandboxes[0].Username != "alice" {
		t.Fatalf("unexpected sandboxes: %+v", listed.Sandboxes)
	}
}

func TestSandboxRuntimeRoutesNotConfigured(t *testing.T) {
	api := newTestAPI("k")
	rec := do(t, api.Router(), http.MethodGet, "/admin/sandboxes", "k", nil)
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("not configured: want 501, got %d", rec.Code)
	}
}

func TestAuditTrail(t *testing.T) {
	api := newTestAPI("k")
	r := api.Router()

	// generate a couple of audited writes
	_ = do(t, r, http.MethodPost, "/admin/tokens", "k", map[string]any{"user_id": "bob"})
	_ = do(t, r, http.MethodPut, "/admin/quotas", "k", map[string]any{"scope": "tenant", "subject": "acme", "limit": 5})

	rec := do(t, r, http.MethodGet, "/admin/audit", "k", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("audit: want 200, got %d", rec.Code)
	}
	var al struct {
		Audit []store.AuditEntry `json:"audit"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &al)
	if len(al.Audit) < 2 {
		t.Fatalf("audit: want >=2 entries, got %d", len(al.Audit))
	}
	// newest first: most recent action should be the quota.set
	if al.Audit[0].Action != "quota.set" {
		t.Fatalf("audit order: want newest quota.set first, got %q", al.Audit[0].Action)
	}
}

func TestAuditEventsIncludeHTTPReads(t *testing.T) {
	api := newTestAPI("k")
	r := api.Router()

	rec := do(t, r, http.MethodGet, "/admin/tokens", "k", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list tokens: want 200, got %d", rec.Code)
	}

	rec = do(t, r, http.MethodGet, "/admin/audit-events?action=admin.tokens.list", "k", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("audit events: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var body struct {
		Events []store.AuditEvent `json:"events"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode audit events: %v", err)
	}
	if len(body.Events) == 0 {
		t.Fatal("expected at least one audit event")
	}
	got := body.Events[0]
	if got.Action != "admin.tokens.list" || got.Result != "success" || got.HTTPMethod != http.MethodGet {
		t.Fatalf("unexpected audit event: %+v", got)
	}
}

func TestTokenMintDisabledWithoutSecret(t *testing.T) {
	mem := store.NewMemory()
	svc := service.New(mem, nil, fixedClock) // no issuer
	api := New(svc, "k")
	rec := do(t, api.Router(), http.MethodPost, "/admin/tokens", "k", map[string]any{"user_id": "x"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("mint w/o secret: want 400, got %d", rec.Code)
	}
}
