package feishu

import (
	"errors"
	"testing"
)

func TestReactionResponseErrorPreservesTrustedPermissionURL(t *testing.T) {
	const permissionURL = "https://open.feishu.cn/app/cli_test/auth?q=im%3Amessage.reactions%3Awrite_only"
	err := reactionResponseError(99991672, "no permission", []byte(`{
		"code": 99991672,
		"msg": "no permission",
		"console_url": "`+permissionURL+`",
		"error": {
			"permission_violations": [{
				"type": "scope",
				"subject": "im:message.reactions:write_only"
			}]
		}
	}`))
	var permissionErr *PermissionError
	if !errors.As(err, &permissionErr) {
		t.Fatalf("error = %T, want PermissionError", err)
	}
	if permissionErr.ConsoleURL != permissionURL {
		t.Fatalf("console URL = %q", permissionErr.ConsoleURL)
	}
	if len(permissionErr.MissingScopes) != 1 ||
		permissionErr.MissingScopes[0] != reactionPermissionScope {
		t.Fatalf("missing scopes = %#v", permissionErr.MissingScopes)
	}
}

func TestReactionResponseErrorRejectsUntrustedPermissionURL(t *testing.T) {
	err := reactionResponseError(99991672, "no permission", []byte(`{
		"code": 99991672,
		"console_url": "https://open.feishu.cn.evil.example/app/cli_test/auth"
	}`))
	var permissionErr *PermissionError
	if !errors.As(err, &permissionErr) {
		t.Fatalf("error = %T, want PermissionError", err)
	}
	if permissionErr.ConsoleURL != "" {
		t.Fatalf("console URL = %q, want empty", permissionErr.ConsoleURL)
	}
}

func TestReactionResponseErrorKeepsNonPermissionErrorGeneric(t *testing.T) {
	err := reactionResponseError(99991400, "rate limited", nil)
	var permissionErr *PermissionError
	if errors.As(err, &permissionErr) {
		t.Fatalf("error = %T, should not be PermissionError", err)
	}
}
