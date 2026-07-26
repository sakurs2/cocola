package feishu

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cocola-project/cocola/packages/go-common/token"
)

func TestAccountAuthorizerUsesTrustedAccountProfile(t *testing.T) {
	issuer := token.NewIssuer("test-secret", "cocola", time.Hour)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/me/account" {
			http.NotFound(w, r)
			return
		}
		if !strings.HasPrefix(r.Header.Get("authorization"), "Bearer ") {
			t.Error("missing account probe token")
		}
		_ = json.NewEncoder(w).Encode(Account{
			ID: "user-1", TenantID: "tenant-1", Enabled: true,
			Email: "trusted@example.com", Name: "Trusted", Username: "trusted-user",
		})
	}))
	defer server.Close()

	authorizer, err := NewAccountAuthorizer(server.URL, issuer, server.Client())
	if err != nil {
		t.Fatalf("NewAccountAuthorizer: %v", err)
	}
	runtimeToken, account, err := authorizer.Authorize(context.Background(), Connector{
		UserID: "user-1", TenantID: "tenant-1",
	})
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	if account.Email != "trusted@example.com" {
		t.Fatalf("account email = %q", account.Email)
	}
	claims, err := token.Decode(runtimeToken, "test-secret", time.Now().Unix())
	if err != nil {
		t.Fatalf("decode runtime token: %v", err)
	}
	if claims.Subject != "user-1" || claims.Tenant != "tenant-1" ||
		claims.Email != "trusted@example.com" || claims.Username != "trusted-user" {
		t.Fatalf("runtime claims = %+v", claims)
	}
	if claims.Expires-claims.IssuedAt != int64((5 * time.Minute).Seconds()) {
		t.Fatalf("runtime token lifetime = %d", claims.Expires-claims.IssuedAt)
	}
}

func TestAccountAuthorizerRejectsDisabledOrMismatchedAccount(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		account Account
	}{
		{name: "deleted", status: http.StatusNotFound},
		{
			name: "disabled", status: http.StatusOK,
			account: Account{ID: "user-1", TenantID: "tenant-1", Enabled: false},
		},
		{
			name: "different user", status: http.StatusOK,
			account: Account{ID: "user-2", TenantID: "tenant-1", Enabled: true},
		},
		{
			name: "different tenant", status: http.StatusOK,
			account: Account{ID: "user-1", TenantID: "tenant-2", Enabled: true},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.status)
				if test.status == http.StatusOK {
					_ = json.NewEncoder(w).Encode(test.account)
				}
			}))
			defer server.Close()
			authorizer, err := NewAccountAuthorizer(
				server.URL,
				token.NewIssuer("test-secret", "cocola", time.Hour),
				server.Client(),
			)
			if err != nil {
				t.Fatalf("NewAccountAuthorizer: %v", err)
			}
			_, _, err = authorizer.Authorize(context.Background(), Connector{
				UserID: "user-1", TenantID: "tenant-1",
			})
			if err != ErrAccountDisabled {
				t.Fatalf("Authorize error = %v, want %v", err, ErrAccountDisabled)
			}
		})
	}
}
