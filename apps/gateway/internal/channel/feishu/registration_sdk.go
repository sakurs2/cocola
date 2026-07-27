package feishu

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"

	registration "github.com/larksuite/oapi-sdk-go/v3/scene/registration"
)

type SDKRegistrar struct{}

func (SDKRegistrar) Register(
	ctx context.Context,
	input RegistrationInput,
	onUpdate func(RegistrationUpdate),
) (RegistrationResult, error) {
	preset := true
	var avatars []string
	if strings.TrimSpace(input.AvatarURL) != "" {
		avatars = []string{strings.TrimSpace(input.AvatarURL)}
	}
	result, err := registration.RegisterApp(ctx, &registration.Options{
		Source: "cocola",
		AppPreset: &registration.AppPreset{
			Name:   input.AppName,
			Desc:   input.AppDesc,
			Avatar: avatars,
		},
		Addons: &registration.AppAddons{
			Preset: &preset,
			Scopes: registration.AppAddonsScopes{
				Tenant: registrationTenantScopes(),
			},
			Events: registration.AppAddonsEvents{
				Items: registration.AppAddonsEventItems{
					Tenant: []string{"im.message.receive_v1"},
				},
			},
		},
		OnQRCode: func(info *registration.QRCodeInfo) {
			if onUpdate != nil {
				onUpdate(RegistrationUpdate{
					VerificationURL: info.URL,
					ExpiresIn:       durationSeconds(info.ExpireIn),
					Status:          FlowAwaitingUser,
				})
			}
		},
		OnStatusChange: func(info *registration.StatusChangeInfo) {
			if onUpdate != nil {
				onUpdate(RegistrationUpdate{Status: FlowAuthorizing})
			}
		},
	})
	if err != nil {
		var denied *registration.AccessDeniedError
		var expired *registration.ExpiredError
		switch {
		case errors.As(err, &denied):
			return RegistrationResult{}, &RegistrationError{Code: "access_denied", Err: err}
		case errors.As(err, &expired):
			return RegistrationResult{}, &RegistrationError{Code: "expired_token", Err: err}
		default:
			return RegistrationResult{}, &RegistrationError{Code: "registration_failed", Err: err}
		}
	}
	output := RegistrationResult{AppID: result.ClientID, AppSecret: result.ClientSecret}
	if result.UserInfo != nil {
		output.OwnerOpenID = result.UserInfo.OpenID
		output.TenantBrand = result.UserInfo.TenantBrand
	}
	return output, nil
}

func registrationTenantScopes() []string {
	return []string{
		"im:message",
		"im:message:send_as_bot",
		"im:message.p2p_msg:readonly",
		"im:resource",
		"im:message.reactions:write_only",
	}
}

func RegistrationAvatarURL(publicOrigins string) string {
	origin := firstPublicOrigin(publicOrigins)
	if origin == "" {
		return ""
	}
	return origin + "/icon.svg"
}

func ConnectorSettingsURL(publicOrigins string) string {
	origin := firstPublicOrigin(publicOrigins)
	if origin == "" {
		return ""
	}
	return origin + "/agents"
}

func firstPublicOrigin(publicOrigins string) string {
	for _, value := range strings.Split(publicOrigins, ",") {
		origin := strings.TrimSpace(value)
		parsed, err := url.Parse(origin)
		if err != nil || parsed.Host == "" ||
			(parsed.Scheme != "http" && parsed.Scheme != "https") ||
			parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" ||
			(parsed.Path != "" && parsed.Path != "/") {
			continue
		}
		return strings.TrimRight(origin, "/")
	}
	return ""
}

func durationSeconds(value int) time.Duration {
	if value <= 0 {
		return 0
	}
	return time.Duration(value) * time.Second
}
