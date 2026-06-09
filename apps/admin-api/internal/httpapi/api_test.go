// HTTP-layer tests exercise the full router through httptest.ResponseRecorder —
// no port is bound (network listeners are forbidden), the handler is invoked
// in-process. We cover: auth gating (enabled vs disabled), the token mint+list+
// revoke+denylist loop, quota upsert/list/delete with validation, skill CRUD +
// enable/disable, the audit trail, and the error envelope mapping.
package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cocola-project/cocola/apps/admin-api/internal/service"
	"github.com/cocola-project/cocola/apps/admin-api/internal/store"
	"github.com/cocola-project/cocola/apps/admin-api/internal/token"
)

// fixedClock returns a deterministic time so audit/issued timestamps are stable.
func fixedClock() time.Time { return time.Unix(1_700_000_000, 0).UTC() }

func newTestAPI(adminKey string) *API {
	mem := store.NewMemory()
	iss := token.NewIssuer("test-secret", "cocola", 24*time.Hour)
	svc := service.New(mem, iss, fixedClock)
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

func TestTokenMintDisabledWithoutSecret(t *testing.T) {
	mem := store.NewMemory()
	svc := service.New(mem, nil, fixedClock) // no issuer
	api := New(svc, "k")
	rec := do(t, api.Router(), http.MethodPost, "/admin/tokens", "k", map[string]any{"user_id": "x"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("mint w/o secret: want 400, got %d", rec.Code)
	}
}
