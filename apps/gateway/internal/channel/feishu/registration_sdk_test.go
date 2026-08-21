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

func TestWorkspaceRegistrationDoesNotSubscribeToInboundMessages(t *testing.T) {
	workspace := registrationAddons(false)
	if got := workspace.Events.Items.Tenant; len(got) != 0 {
		t.Fatalf("workspace registration events = %#v, want none", got)
	}
	agent := registrationAddons(true)
	if got := agent.Events.Items.Tenant; !slices.Equal(got, []string{"im.message.receive_v1"}) {
		t.Fatalf("Agent registration events = %#v", got)
	}
}

func TestConnectorSettingsURLUsesFirstValidPublicOrigin(t *testing.T) {
	got := ConnectorSettingsURL(
		"javascript:alert(1), https://cocola.example.com, https://ignored.example.com",
	)
	if got != "https://cocola.example.com/agents" {
		t.Fatalf("connector settings URL = %q", got)
	}
}
