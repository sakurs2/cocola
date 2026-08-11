package config

import (
	"encoding/json"
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
	info, err := os.Stat(paths.Compose)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Fatalf("%s mode = %o", paths.Compose, info.Mode().Perm())
	}
	contents, err := os.ReadFile(paths.Environment)
	if err != nil {
		t.Fatal(err)
	}
	text := string(contents)
	for _, expected := range []string{
		`COCOLA_VERSION="v0.1.0"`,
		`COCOLA_WEB_HOST="0.0.0.0"`,
		`COCOLA_PUBLIC_ORIGINS="http://127.0.0.1:3000,http://localhost:3000"`,
		`COCOLA_BOOTSTRAP_ADMIN_PASSWORD="strong-password"`,
		`COCOLA_AUTH_SECRET="`,
		`COCOLA_SANDBOX_LLM_BASE_URL="http://llm-gateway:8080"`,
		`COCOLA_SESSION_VOLUME_SIZE="2Gi"`,
		`COCOLA_SANDBOX_PROFILE="coding"`,
		`COCOLA_AGENT_MAX_TURNS="200"`,
		`COCOLA_AGENT_TOOL_STEP_TIMEOUT_SECS="600"`,
		`COCOLA_LLM_TIMEOUT_SECS="600"`,
		`COCOLA_SANDBOX_TOKEN_TTL_SECONDS="604800"`,
		`COCOLA_OPENVIKING_URL="http://openviking:1933"`,
		`COCOLA_OPENVIKING_ROOT_API_KEY="`,
		`COCOLA_MEMORY_LLM_SERVICE_TOKEN="`,
		`COCOLA_MEMORY_EMBEDDING_DIMENSION="1024"`,
		`COCOLA_SCM_SECRET_KEY="`,
		`COCOLA_SCM_SECRET_KEY_FILE=""`,
		`COCOLA_FORGEJO_HOST_PORT="3001"`,
		`COCOLA_FORGEJO_API_URL="http://forgejo:3000"`,
		`COCOLA_FORGEJO_CLONE_URL="http://forgejo:3000"`,
		`COCOLA_FEATURE_LOCAL_PROJECTS="true"`,
		`COCOLA_SANDBOX_PROJECT_BROKER_URL="http://gateway:8080"`,
		`COCOLA_SANDBOX_SKILL_BROKER_URL="http://gateway:8080"`,
		`COCOLA_SKILL_PUBLISH_ENABLED="false"`,
		`COCOLA_PROJECT_MAX_REPOSITORY_MB="512"`,
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
		state.GHCREndpoint != DefaultGHCREndpoint ||
		state.SandboxImage != "ghcr.io/sakurs2/cocola-sandbox-runtime:v0.1.0" ||
		len(state.ManagedRuntimeImages) != 3 ||
		state.ConfigSchemaVersion != CurrentSchemaVersion || state.DeploymentRevision == "" ||
		state.LLMPort != 18091 || state.InternalSCM.HostPort != 3001 ||
		state.InternalSCM.APIURL != "http://forgejo:3000" ||
		state.InternalSCM.SandboxCloneURL != "http://forgejo:3000" {
		t.Fatalf("state = %+v", state)
	}
	if _, err := WriteInstallation(paths, options, []byte("different")); !errors.Is(err, ErrAlreadyInstalled) {
		t.Fatalf("second install error = %v", err)
	}
}

func TestPrepareUpgradePreservesConfigurationAndRollsBack(t *testing.T) {
	home := filepath.Join(t.TempDir(), "cocola")
	paths, err := ResolvePaths(home)
	if err != nil {
		t.Fatal(err)
	}
	options := Defaults("v0.1.0")
	options.Home = home
	options.AdminPassword = "strong-password"
	if _, err := WriteInstallation(paths, options, []byte("services:\n  old: {}\n")); err != nil {
		t.Fatal(err)
	}
	originalEnvironment, err := os.ReadFile(paths.Environment)
	if err != nil {
		t.Fatal(err)
	}
	originalEnvironment = append(originalEnvironment,
		[]byte("CUSTOM_OPERATOR_SETTING=keep-me\nCOCOLA_WEB_HOST_PORT=3200\n")...,
	)
	if err := os.WriteFile(paths.Environment, originalEnvironment, 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := PrepareUpgrade(paths, "v0.2.0", []byte("services:\n  new: {}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !result.Updated || result.FromVersion != "v0.1.0" || result.ToVersion != "v0.2.0" || result.BackupDir == "" {
		t.Fatalf("upgrade result = %+v", result)
	}
	migrated, err := os.ReadFile(paths.Environment)
	if err != nil {
		t.Fatal(err)
	}
	text := string(migrated)
	for _, expected := range []string{
		`COCOLA_VERSION="v0.2.0"`,
		`COCOLA_WEB_HOST_PORT=3200`,
		`CUSTOM_OPERATOR_SETTING=keep-me`,
		`COCOLA_BOOTSTRAP_ADMIN_PASSWORD="strong-password"`,
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("migrated environment missing %q: %s", expected, text)
		}
	}
	state, err := Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	if state.PendingUpgrade == nil || state.Version != "v0.2.0" || state.WebPort != 3200 ||
		state.LastSuccessfulVersion != "v0.1.0" || state.GHCREndpoint != DefaultGHCREndpoint {
		t.Fatalf("pending state = %+v", state)
	}
	if _, err := os.Stat(filepath.Join(result.BackupDir, "manifest.json")); err != nil {
		t.Fatalf("backup manifest: %v", err)
	}

	backupDir, err := RollbackUpgrade(paths)
	if err != nil {
		t.Fatal(err)
	}
	if backupDir != result.BackupDir {
		t.Fatalf("rollback backup = %q, want %q", backupDir, result.BackupDir)
	}
	restoredEnvironment, err := os.ReadFile(paths.Environment)
	if err != nil {
		t.Fatal(err)
	}
	if string(restoredEnvironment) != string(originalEnvironment) {
		t.Fatalf("environment was not restored exactly\n got: %s\nwant: %s", restoredEnvironment, originalEnvironment)
	}
	restoredCompose, err := os.ReadFile(paths.Compose)
	if err != nil {
		t.Fatal(err)
	}
	if string(restoredCompose) != "services:\n  old: {}\n" {
		t.Fatalf("compose was not restored: %s", restoredCompose)
	}
}

func TestPrepareGHCREndpointChangeIsTransactionalAndIdempotent(t *testing.T) {
	home := filepath.Join(t.TempDir(), "cocola")
	paths, err := ResolvePaths(home)
	if err != nil {
		t.Fatal(err)
	}
	options := Defaults("v0.1.0")
	options.Home = home
	options.AdminPassword = "strong-password"
	if _, err := WriteInstallation(paths, options, []byte("services:\n  app: {}\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := MarkStarted(paths); err != nil {
		t.Fatal(err)
	}
	originalEnvironment, err := os.ReadFile(paths.Environment)
	if err != nil {
		t.Fatal(err)
	}

	result, err := PrepareGHCREndpointChange(paths, " GHCR.NJU.EDU.CN ")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Updated || result.FromVersion != result.ToVersion ||
		result.FromGHCREndpoint != DefaultGHCREndpoint || result.ToGHCREndpoint != "ghcr.nju.edu.cn" ||
		result.BackupDir == "" {
		t.Fatalf("endpoint change result = %+v", result)
	}
	state, err := Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	if state.GHCREndpoint != DefaultGHCREndpoint || state.PendingUpgrade == nil ||
		state.PendingUpgrade.FromGHCREndpoint != DefaultGHCREndpoint ||
		state.PendingUpgrade.ToGHCREndpoint != "ghcr.nju.edu.cn" {
		t.Fatalf("pending endpoint state = %+v", state)
	}
	effectiveEndpoint, err := EffectiveGHCREndpoint(state)
	if err != nil || effectiveEndpoint != "ghcr.nju.edu.cn" {
		t.Fatalf("effective pending endpoint = %q, %v", effectiveEndpoint, err)
	}
	environment, err := os.ReadFile(paths.Environment)
	if err != nil {
		t.Fatal(err)
	}
	text := string(environment)
	for _, expected := range []string{
		`COCOLA_IMAGE_REGISTRY="ghcr.nju.edu.cn/sakurs2"`,
		`COCOLA_REDIS_IMAGE="ghcr.nju.edu.cn/sakurs2/cocola-redis:`,
		`COCOLA_OPENVIKING_IMAGE="ghcr.nju.edu.cn/volcengine/openviking:`,
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("endpoint environment missing %q:\n%s", expected, text)
		}
	}
	if strings.Contains(text, "docker.io") || strings.Contains(text, "codeberg.org") ||
		!strings.Contains(text, openSandboxAliyunRegistry+"/execd:v1.0.19") {
		t.Fatalf("endpoint environment contains an unexpected registry:\n%s", text)
	}

	repeated, err := PrepareGHCREndpointChange(paths, "ghcr.nju.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if repeated.BackupDir != result.BackupDir {
		t.Fatalf("idempotent endpoint change created a new backup: %+v", repeated)
	}
	if _, err := RollbackUpgrade(paths); err != nil {
		t.Fatal(err)
	}
	restored, err := os.ReadFile(paths.Environment)
	if err != nil {
		t.Fatal(err)
	}
	if string(restored) != string(originalEnvironment) {
		t.Fatal("endpoint rollback did not restore config.env exactly")
	}
}

func TestPrepareLastSuccessfulGHCREndpointPreservesPendingVersionUpgrade(t *testing.T) {
	home := filepath.Join(t.TempDir(), "cocola")
	paths, err := ResolvePaths(home)
	if err != nil {
		t.Fatal(err)
	}
	options := Defaults("v0.1.0")
	options.Home = home
	options.AdminPassword = "strong-password"
	if _, err := WriteInstallation(paths, options, []byte("services:\n  old: {}\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := MarkStarted(paths); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareUpgrade(paths, "v0.2.0", []byte("services:\n  new: {}\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareGHCREndpointChange(paths, "ghcr.nju.edu.cn"); err != nil {
		t.Fatal(err)
	}

	result, err := PrepareLastSuccessfulGHCREndpoint(paths)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Updated || result.ToVersion != "v0.2.0" ||
		result.FromGHCREndpoint != DefaultGHCREndpoint ||
		result.ToGHCREndpoint != DefaultGHCREndpoint {
		t.Fatalf("restored endpoint candidate = %+v", result)
	}
	state, err := Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	if state.GHCREndpoint != DefaultGHCREndpoint || state.Version != "v0.2.0" ||
		state.PendingUpgrade == nil || state.PendingUpgrade.ToGHCREndpoint != DefaultGHCREndpoint {
		t.Fatalf("pending version candidate = %+v", state)
	}
	environment, err := os.ReadFile(paths.Environment)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(environment), "ghcr.nju.edu.cn") ||
		!strings.Contains(string(environment), `COCOLA_REDIS_IMAGE="ghcr.io/sakurs2/cocola-redis:`) {
		t.Fatalf("version candidate did not return to committed endpoint:\n%s", environment)
	}
}

func TestPrepareGHCREndpointChangePreservesCustomCocolaRegistry(t *testing.T) {
	home := filepath.Join(t.TempDir(), "cocola")
	paths, err := ResolvePaths(home)
	if err != nil {
		t.Fatal(err)
	}
	options := Defaults("v0.1.0")
	options.Home = home
	options.AdminPassword = "strong-password"
	options.Registry = "registry.example/team"
	options.RegistryExplicit = true
	if _, err := WriteInstallation(paths, options, []byte("services: {}\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareGHCREndpointChange(paths, "ghcr.nju.edu.cn"); err != nil {
		t.Fatal(err)
	}
	environment, err := os.ReadFile(paths.Environment)
	if err != nil {
		t.Fatal(err)
	}
	text := string(environment)
	if !strings.Contains(text, `COCOLA_IMAGE_REGISTRY="registry.example/team"`) ||
		!strings.Contains(text, `COCOLA_REDIS_IMAGE="ghcr.nju.edu.cn/sakurs2/cocola-redis:`) {
		t.Fatalf("custom registry or third-party endpoint was not preserved:\n%s", text)
	}
}

func TestValidateConfiguredImagesDetectsEnvironmentDrift(t *testing.T) {
	home := filepath.Join(t.TempDir(), "cocola")
	paths, err := ResolvePaths(home)
	if err != nil {
		t.Fatal(err)
	}
	options := Defaults("v0.1.0")
	options.Home = home
	options.AdminPassword = "strong-password"
	if _, err := WriteInstallation(paths, options, []byte("services: {}\n")); err != nil {
		t.Fatal(err)
	}
	state, err := Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateConfiguredImages(paths, state); err != nil {
		t.Fatalf("valid image configuration: %v", err)
	}
	environment, err := os.ReadFile(paths.Environment)
	if err != nil {
		t.Fatal(err)
	}
	drifted := strings.Replace(
		string(environment), "ghcr.io/sakurs2/cocola-redis", "docker.io/library/redis", 1,
	)
	if err := os.WriteFile(paths.Environment, []byte(drifted), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ValidateConfiguredImages(paths, state); err == nil ||
		!strings.Contains(err.Error(), "COCOLA_REDIS_IMAGE") {
		t.Fatalf("image drift error = %v", err)
	}
}

func TestPrepareUpgradeMigratesLegacyStateAndCommitsAfterStart(t *testing.T) {
	home := filepath.Join(t.TempDir(), "cocola")
	paths, err := ResolvePaths(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	legacyState := State{
		Version: "v0.1.0", ManagedOpenSandbox: true,
		SandboxImage: "registry.example/cocola-sandbox-runtime:v0.1.0",
		WebPort:      3000, GatewayPort: 8080,
	}
	stateData, err := json.Marshal(legacyState)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.State, stateData, 0o600); err != nil {
		t.Fatal(err)
	}
	legacyEnvironment := strings.Join([]string{
		`COCOLA_VERSION="v0.1.0"`,
		`COCOLA_IMAGE_REGISTRY="registry.example"`,
		`COCOLA_WEB_HOST_PORT="3000"`,
		`COCOLA_GATEWAY_HOST_PORT="8080"`,
		`COCOLA_LLM_HOST_PORT="18091"`,
		`COCOLA_PG_PASSWORD="database-secret"`,
		`COCOLA_BOOTSTRAP_ADMIN_PASSWORD="admin-secret"`,
		"",
	}, "\n")
	if err := os.WriteFile(paths.Environment, []byte(legacyEnvironment), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.Compose, []byte("legacy compose\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := PrepareUpgrade(paths, "v0.2.0", []byte("current compose\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !result.Updated {
		t.Fatal("legacy installation was not migrated")
	}
	migrated, err := os.ReadFile(paths.Environment)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(migrated), `COCOLA_PG_PASSWORD="database-secret"`) ||
		!strings.Contains(string(migrated), `COCOLA_BOOTSTRAP_ADMIN_PASSWORD="admin-secret"`) ||
		!strings.Contains(string(migrated), `COCOLA_IMAGE_REGISTRY="registry.example"`) {
		t.Fatalf("legacy secrets changed: %s", migrated)
	}
	state, err := MarkStarted(paths)
	if err != nil {
		t.Fatal(err)
	}
	if state.PendingUpgrade != nil || state.LastSuccessfulVersion != "v0.2.0" ||
		state.ConfigSchemaVersion != CurrentSchemaVersion || state.GHCREndpoint != DefaultGHCREndpoint {
		t.Fatalf("committed state = %+v", state)
	}
}

func TestPrepareUpgradeMigratesRemovedCNMirrorAndRollsBack(t *testing.T) {
	home := filepath.Join(t.TempDir(), "cocola")
	paths, err := ResolvePaths(home)
	if err != nil {
		t.Fatal(err)
	}
	options := Defaults("v0.1.0")
	options.Home = home
	options.AdminPassword = "strong-password"
	if _, err := WriteInstallation(paths, options, []byte("services:\n  app: {}\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := MarkStarted(paths); err != nil {
		t.Fatal(err)
	}
	directEnvironment, err := os.ReadFile(paths.Environment)
	if err != nil {
		t.Fatal(err)
	}
	mirrorEnvironment := strings.ReplaceAll(string(directEnvironment), "ghcr.io/", "ghcr.nju.edu.cn/")
	mirrorEnvironment = strings.ReplaceAll(mirrorEnvironment, "docker.io/", "docker.nju.edu.cn/")
	mirrorEnvironment = strings.Replace(
		mirrorEnvironment,
		"COCOLA_VERSION=\"v0.1.0\"\n",
		"COCOLA_VERSION=\"v0.1.0\"\nCOCOLA_IMAGE_SOURCE=\"cn-mirror\"\n",
		1,
	)
	if err := os.WriteFile(paths.Environment, []byte(mirrorEnvironment), 0o600); err != nil {
		t.Fatal(err)
	}
	stateData, err := os.ReadFile(paths.State)
	if err != nil {
		t.Fatal(err)
	}
	var legacyState map[string]any
	if err := json.Unmarshal(stateData, &legacyState); err != nil {
		t.Fatal(err)
	}
	legacyState["config_schema_version"] = float64(3)
	legacyState["image_source"] = "cn-mirror"
	legacyState["sandbox_image"] = "ghcr.nju.edu.cn/sakurs2/cocola-sandbox-runtime:v0.1.0"
	legacyState["managed_runtime_images"] = []string{
		"ghcr.nju.edu.cn/sakurs2/cocola-sandbox-runtime:v0.1.0",
		openSandboxAliyunRegistry + "/execd:v1.0.19",
		openSandboxAliyunRegistry + "/egress:v1.1.2",
	}
	stateData, err = json.Marshal(legacyState)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.State, stateData, 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := PrepareUpgrade(paths, "v0.1.0", []byte("services:\n  app: {}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !result.Updated || result.FromVersion != result.ToVersion ||
		result.FromImageRegistry != legacyCNMirrorRegistry || result.ToImageRegistry != DefaultRegistry {
		t.Fatalf("mirror migration result = %+v", result)
	}
	state, err := Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	if state.PendingUpgrade == nil || state.ConfigSchemaVersion != CurrentSchemaVersion ||
		state.GHCREndpoint != DefaultGHCREndpoint || !strings.HasPrefix(state.SandboxImage, "ghcr.io/sakurs2/") {
		t.Fatalf("pending mirror migration = %+v", state)
	}
	migratedState, err := os.ReadFile(paths.State)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(migratedState), "image_source") {
		t.Fatalf("migrated state still contains removed image-source fields: %s", migratedState)
	}
	migratedEnvironment, err := os.ReadFile(paths.Environment)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(migratedEnvironment), "COCOLA_IMAGE_SOURCE") ||
		strings.Contains(string(migratedEnvironment), "ghcr.nju.edu.cn") ||
		strings.Contains(string(migratedEnvironment), "docker.nju.edu.cn") {
		t.Fatalf("migrated environment still contains image-source state:\n%s", migratedEnvironment)
	}
	if _, err := RollbackUpgrade(paths); err != nil {
		t.Fatal(err)
	}
	restoredEnvironment, err := os.ReadFile(paths.Environment)
	if err != nil {
		t.Fatal(err)
	}
	if string(restoredEnvironment) != mirrorEnvironment {
		t.Fatal("mirror migration rollback did not restore the original environment")
	}
}

func TestLoadRejectsNewerConfigSchema(t *testing.T) {
	paths, err := ResolvePaths(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	data := []byte(`{"config_schema_version":999,"version":"v9"}`)
	if err := os.WriteFile(paths.State, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(paths); err == nil || !strings.Contains(err.Error(), "newer than this CLI") {
		t.Fatalf("Load() error = %v", err)
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
		{"duplicate internal SCM port", func(o *Options) { o.InternalSCM.HostPort = o.WebPort }},
		{"bad registry", func(o *Options) { o.Registry = "https://ghcr.io/acme" }},
		{"bad registry slash", func(o *Options) { o.Registry = "ghcr.io/acme/" }},
		{"email with display name", func(o *Options) { o.AdminEmail = "Admin <admin@example.com>" }},
		{"bad external URL", func(o *Options) { o.ManagedOpenSandbox = false; o.ExternalOpenSandboxURL = "localhost" }},
		{"external sandbox missing LLM URL", func(o *Options) {
			o.ManagedOpenSandbox = false
			o.ExternalOpenSandboxURL = "https://sandbox.example.com/v1"
		}},
		{"external sandbox missing SCM URL", func(o *Options) {
			o.ManagedOpenSandbox = false
			o.ExternalOpenSandboxURL = "https://sandbox.example.com/v1"
			o.SandboxLLMBaseURL = "https://llm.example.com"
		}},
		{"invalid sandbox SCM URL", func(o *Options) {
			o.InternalSCM.SandboxCloneURL = "host.docker.internal:3001"
		}},
		{"short password", func(o *Options) { o.AdminPassword = "short" }},
		{"short unicode password", func(o *Options) { o.AdminPassword = "密码密码密码" }},
		{"blank password", func(o *Options) { o.AdminPassword = "        " }},
		{"oversized password", func(o *Options) { o.AdminPassword = strings.Repeat("x", 73) }},
		{"public URL path", func(o *Options) { o.PublicURL = "https://cocola.example.com/app" }},
		{"public URL wildcard", func(o *Options) { o.PublicURL = "https://*.example.com" }},
		{"public URL bind address", func(o *Options) { o.PublicURL = "http://0.0.0.0:3000" }},
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

func TestUpgradeRequiresExplicitInternalSCMURLForExternalSandboxes(t *testing.T) {
	paths := Paths{Home: t.TempDir(), SandboxRoot: t.TempDir()}
	state := State{ManagedOpenSandbox: false, WebPort: 3000, GatewayPort: 8080, LLMPort: 18091}
	environment := []byte(strings.Join([]string{
		`COCOLA_VERSION="v0.1.0"`,
		`COCOLA_OPENSANDBOX_MANAGED="0"`,
		`COCOLA_IMAGE_REGISTRY="ghcr.io/sakurs2"`,
		"",
	}, "\n"))

	if _, _, err := migrateEnvironment(paths, state, "v0.2.0", environment); err == nil ||
		!strings.Contains(err.Error(), "COCOLA_FORGEJO_CLONE_URL is required") {
		t.Fatalf("missing external SCM URL error = %v", err)
	}

	environment = append(environment, []byte(
		`COCOLA_FORGEJO_CLONE_URL="https://scm.sandbox.example.com"`+"\n",
	)...)
	if _, _, err := migrateEnvironment(paths, state, "v0.2.0", environment); err != nil {
		t.Fatalf("explicit external SCM URL: %v", err)
	}
}

func TestManagedUpgradeMovesLegacySandboxEndpointsToDedicatedNetwork(t *testing.T) {
	paths := Paths{Home: t.TempDir(), SandboxRoot: t.TempDir()}
	state := State{
		ManagedOpenSandbox: true,
		WebPort:            3000, GatewayPort: 8080, LLMPort: 18091,
		InternalSCM: InternalSCMEndpoint{
			APIURL:          defaultInternalSCMAPIURL,
			HostPort:        defaultInternalSCMHostPort,
			SandboxCloneURL: "http://host.docker.internal:3001",
		},
	}
	environment := []byte(strings.Join([]string{
		`COCOLA_VERSION="v0.1.0"`,
		`COCOLA_OPENSANDBOX_MANAGED="1"`,
		`COCOLA_IMAGE_REGISTRY="ghcr.io/sakurs2"`,
		`COCOLA_LLM_HOST_PORT="18091"`,
		`COCOLA_GATEWAY_HOST_PORT="8080"`,
		`COCOLA_FORGEJO_HOST_PORT="3001"`,
		`COCOLA_SANDBOX_LLM_BASE_URL="http://host.docker.internal:18091"`,
		`COCOLA_FORGEJO_CLONE_URL="http://host.docker.internal:3001"`,
		`COCOLA_SANDBOX_PROJECT_BROKER_URL="http://host.docker.internal:8080"`,
		`COCOLA_SANDBOX_SKILL_BROKER_URL="http://host.docker.internal:8080"`,
		"",
	}, "\n"))

	migrated, derived, err := migrateEnvironment(paths, state, "v0.2.0", environment)
	if err != nil {
		t.Fatal(err)
	}
	text := string(migrated)
	for _, expected := range []string{
		`COCOLA_SANDBOX_LLM_BASE_URL="http://llm-gateway:8080"`,
		`COCOLA_FORGEJO_CLONE_URL="http://forgejo:3000"`,
		`COCOLA_SANDBOX_PROJECT_BROKER_URL="http://gateway:8080"`,
		`COCOLA_SANDBOX_SKILL_BROKER_URL="http://gateway:8080"`,
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("managed endpoint migration missing %q:\n%s", expected, text)
		}
	}
	if derived.internalSCM.SandboxCloneURL != defaultInternalSCMAPIURL {
		t.Fatalf("derived Sandbox clone URL = %q", derived.internalSCM.SandboxCloneURL)
	}
}

func TestManagedUpgradePreservesOperatorOwnedSandboxEndpoints(t *testing.T) {
	paths := Paths{Home: t.TempDir(), SandboxRoot: t.TempDir()}
	state := State{ManagedOpenSandbox: true, WebPort: 3000, GatewayPort: 8080, LLMPort: 18091}
	environment := []byte(strings.Join([]string{
		`COCOLA_VERSION="v0.1.0"`,
		`COCOLA_OPENSANDBOX_MANAGED="1"`,
		`COCOLA_IMAGE_REGISTRY="ghcr.io/sakurs2"`,
		`COCOLA_SANDBOX_LLM_BASE_URL="https://llm.sandbox.example.com" # operator route`,
		`COCOLA_FORGEJO_CLONE_URL="https://scm.sandbox.example.com"`,
		`COCOLA_SANDBOX_PROJECT_BROKER_URL="https://project-broker.example.com"`,
		`COCOLA_SANDBOX_SKILL_BROKER_URL="https://skill-broker.example.com"`,
		"",
	}, "\n"))

	migrated, _, err := migrateEnvironment(paths, state, "v0.2.0", environment)
	if err != nil {
		t.Fatal(err)
	}
	text := string(migrated)
	for _, expected := range []string{
		`COCOLA_SANDBOX_LLM_BASE_URL="https://llm.sandbox.example.com" # operator route`,
		`COCOLA_FORGEJO_CLONE_URL="https://scm.sandbox.example.com"`,
		`COCOLA_SANDBOX_PROJECT_BROKER_URL="https://project-broker.example.com"`,
		`COCOLA_SANDBOX_SKILL_BROKER_URL="https://skill-broker.example.com"`,
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("operator endpoint changed; missing %q:\n%s", expected, text)
		}
	}
}

func TestPublicOriginDefaultsAndProductionConfiguration(t *testing.T) {
	options := Defaults("v0.1.0")
	if got, err := options.PublicOrigin(); err != nil || got != "http://localhost:3000" {
		t.Fatalf("default PublicOrigin() = %q, %v", got, err)
	}
	options.PublicURL = "https://cocola.example.com/"
	if got, err := options.PublicOrigin(); err != nil || got != "https://cocola.example.com" {
		t.Fatalf("production PublicOrigin() = %q, %v", got, err)
	}
	options.Home = t.TempDir()
	if err := options.Validate(); err != nil {
		t.Fatalf("production options: %v", err)
	}
	text := renderEnvironment(Paths{Home: options.Home, SandboxRoot: filepath.Join(options.Home, "sandboxes")}, options, secrets{}, "password")
	if !strings.Contains(text, `COCOLA_PUBLIC_ORIGINS="http://127.0.0.1:3000,http://localhost:3000,https://cocola.example.com"`) {
		t.Fatalf("production origins missing: %s", text)
	}
}

func TestQuoteEnvEscapesComposeInterpolation(t *testing.T) {
	if got := quoteEnv(`pa$HOME\\"word`); got != `"pa$$HOME\\\\\"word"` {
		t.Fatalf("quoteEnv() = %q", got)
	}
}

func TestParseEnvironmentAcceptsComposeQuotesAndComments(t *testing.T) {
	document, err := parseEnvironment([]byte(strings.Join([]string{
		"DOUBLE=\"value with spaces\" # operator note",
		"SINGLE='literal $value # retained' # another note",
		"PLAIN=value # trailing note",
		"HASH=value#part",
		"",
	}, "\n")))
	if err != nil {
		t.Fatal(err)
	}
	for key, expected := range map[string]string{
		"DOUBLE": "value with spaces",
		"SINGLE": "literal $value # retained",
		"PLAIN":  "value",
		"HASH":   "value#part",
	} {
		if got := document.values[key]; got != expected {
			t.Fatalf("%s = %q, want %q", key, got, expected)
		}
	}
	if rendered := string(document.render()); !strings.Contains(rendered, "# operator note") ||
		!strings.Contains(rendered, "# another note") {
		t.Fatalf("comments were not preserved: %s", rendered)
	}
}
