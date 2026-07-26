package feishu

import (
	"slices"
	"testing"
)

func TestRegistrationTenantScopesIncludeReactionWriteOnly(t *testing.T) {
	scopes := registrationTenantScopes()
	if !slices.Contains(scopes, reactionPermissionScope) {
		t.Fatalf("registration scopes = %#v", scopes)
	}
	if slices.Contains(scopes, "im:message.reactions:read") {
		t.Fatalf("registration scopes request unnecessary reaction read access: %#v", scopes)
	}
}

func TestConnectorSettingsURLUsesFirstValidPublicOrigin(t *testing.T) {
	got := ConnectorSettingsURL(
		"javascript:alert(1), https://cocola.example.com, https://ignored.example.com",
	)
	if got != "https://cocola.example.com/connectors" {
		t.Fatalf("connector settings URL = %q", got)
	}
}
