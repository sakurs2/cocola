package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteInstallationCreatesPrivateConfigAndStableState(t *testing.T) {
	home := filepath.Join(t.TempDir(), "cocola")
	options := Defaults("v0.1.0")
	options.Home = home
	options.AdminPassword = "strong-password"
	paths, err := ResolvePaths(home)
	if err != nil {
		t.Fatal(err)
	}
	credentials, err := WriteInstallation(paths, options, []byte("services: {}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if credentials.AdminPassword != options.AdminPassword {
		t.Fatalf("password = %q", credentials.AdminPassword)
	}
	for _, path := range []string{paths.Environment, paths.State} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode = %o", path, info.Mode().Perm())
		}
	}
	contents, err := os.ReadFile(paths.Environment)
	if err != nil {
		t.Fatal(err)
	}
	text := string(contents)
	for _, expected := range []string{
		`COCOLA_VERSION="v0.1.0"`,
		`COCOLA_BOOTSTRAP_ADMIN_PASSWORD="strong-password"`,
		`COCOLA_AUTH_SECRET="`,
		`COCOLA_SANDBOX_LLM_BASE_URL="http://host.docker.internal:18091"`,
		`COCOLA_SESSION_VOLUME_SIZE="2Gi"`,
		`COCOLA_SANDBOX_PROFILE="coding"`,
		`COCOLA_AGENT_MAX_TURNS="200"`,
		`COCOLA_AGENT_TOOL_STEP_TIMEOUT_SECS="600"`,
		`COCOLA_LLM_TIMEOUT_SECS="600"`,
		`COCOLA_SANDBOX_TOKEN_TTL_SECONDS="604800"`,
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("config missing %q", expected)
		}
	}
	state, err := Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	if state.Version != "v0.1.0" || !state.ManagedOpenSandbox ||
		state.SandboxImage != "ghcr.io/sakurs2/cocola-sandbox-runtime:v0.1.0" {
		t.Fatalf("state = %+v", state)
	}
	if _, err := WriteInstallation(paths, options, []byte("different")); !errors.Is(err, ErrAlreadyInstalled) {
		t.Fatalf("second install error = %v", err)
	}
}

func TestOptionsValidation(t *testing.T) {
	valid := Defaults("v0.1.0")
	valid.Home = t.TempDir()
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid options: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*Options)
	}{
		{"invalid version", func(o *Options) { o.Version = "bad/tag" }},
		{"invalid version prefix", func(o *Options) { o.Version = ".bad" }},
		{"duplicate ports", func(o *Options) { o.GatewayPort = o.WebPort }},
		{"bad registry", func(o *Options) { o.Registry = "https://ghcr.io/acme" }},
		{"bad registry slash", func(o *Options) { o.Registry = "ghcr.io/acme/" }},
		{"email with display name", func(o *Options) { o.AdminEmail = "Admin <admin@example.com>" }},
		{"bad external URL", func(o *Options) { o.ManagedOpenSandbox = false; o.ExternalOpenSandboxURL = "localhost" }},
		{"external sandbox missing LLM URL", func(o *Options) {
			o.ManagedOpenSandbox = false
			o.ExternalOpenSandboxURL = "https://sandbox.example.com/v1"
		}},
		{"short password", func(o *Options) { o.AdminPassword = "short" }},
		{"invalid session volume", func(o *Options) { o.SessionVolumeSize = "0Gi" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			options := valid
			test.mutate(&options)
			if err := options.Validate(); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestQuoteEnvEscapesComposeInterpolation(t *testing.T) {
	if got := quoteEnv(`pa$HOME\\"word`); got != `"pa$$HOME\\\\\"word"` {
		t.Fatalf("quoteEnv() = %q", got)
	}
}
