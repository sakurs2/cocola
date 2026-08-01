package command

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cocola-project/cocola/apps/cli/internal/config"
	"github.com/cocola-project/cocola/apps/cli/internal/ui"
)

func TestVersionJSON(t *testing.T) {
	var output, errors bytes.Buffer
	err := Execute(context.Background(), []string{"version", "--json"}, IO{
		In: &bytes.Buffer{}, Out: &output, Err: &errors,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"version"`) || strings.Contains(output.String(), "\x1b[") {
		t.Fatalf("output = %q", output.String())
	}
}

func TestRootExposesOnlyStartAndStopLifecycleCommands(t *testing.T) {
	app := &application{io: IO{In: &bytes.Buffer{}, Out: &bytes.Buffer{}, Err: &bytes.Buffer{}}}
	root := app.rootCommand()
	found := map[string]bool{}
	for _, command := range root.Commands() {
		found[command.Name()] = true
	}
	for _, name := range []string{"start", "stop"} {
		if !found[name] {
			t.Fatalf("missing lifecycle command %q", name)
		}
	}
	for _, name := range []string{"up", "down", "restart"} {
		if found[name] {
			t.Fatalf("unexpected lifecycle command %q", name)
		}
	}
}

func TestNonInteractiveInstallWritesEmbeddedRelease(t *testing.T) {
	home := filepath.Join(t.TempDir(), "cocola")
	var output, errors bytes.Buffer
	err := Execute(context.Background(), []string{
		"install", "--home", home, "--yes", "--admin-password", "test-password",
	}, IO{In: &bytes.Buffer{}, Out: &output, Err: &errors})
	if err != nil {
		t.Fatalf("install: %v, stderr=%s", err, errors.String())
	}
	compose, err := os.ReadFile(filepath.Join(home, "compose.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(compose), "build:") {
		t.Fatal("embedded release compose must not build from source")
	}
	if !strings.Contains(string(compose), "cocola-gateway:${COCOLA_VERSION}") {
		t.Fatal("embedded release compose does not use versioned images")
	}
	if strings.Contains(string(compose), "content: |") || strings.Contains(string(compose), "\nconfigs:") {
		t.Fatal("embedded release compose must not require configs.content")
	}
	if !strings.Contains(string(compose), `./opensandbox.toml:/etc/opensandbox/config.toml:ro`) {
		t.Fatal("embedded release compose does not mount the generated OpenSandbox config")
	}
	for _, floating := range []string{
		"redis:7-alpine", "postgres:16-alpine", "minio/minio:latest",
		"minio/mc:latest", "opensandbox/server:latest",
	} {
		if strings.Contains(string(compose), floating) {
			t.Fatalf("embedded release compose uses floating image %q", floating)
		}
	}
	openSandboxConfig, err := os.ReadFile(filepath.Join(home, "opensandbox.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(openSandboxConfig), `allowed_host_paths = ["`+filepath.Join(home, "sandboxes")+`"]`) {
		t.Fatalf("generated OpenSandbox config has the wrong sandbox root: %s", openSandboxConfig)
	}
	for _, expected := range []string{
		`COCOLA_AGENT_RUNTIME_DEFAULT_ID: "${COCOLA_AGENT_RUNTIME_DEFAULT_ID:-claude-code}"`,
		`COCOLA_AGENT_RUNTIME_PICKER_ENABLED: "${COCOLA_AGENT_RUNTIME_PICKER_ENABLED:-false}"`,
	} {
		if !strings.Contains(string(compose), expected) {
			t.Fatalf("embedded release compose missing %q", expected)
		}
	}
	environment, err := os.ReadFile(filepath.Join(home, "config.env"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(environment), `COCOLA_PUBLIC_ORIGINS="http://127.0.0.1:3000,http://localhost:3000"`) {
		t.Fatal("generated environment does not configure explicit local public origins")
	}
	if strings.Contains(string(environment), `COCOLA_PUBLIC_ORIGINS="*"`) {
		t.Fatal("generated environment must not trust wildcard origins")
	}
	if !strings.Contains(output.String(), "cocola start") {
		t.Fatalf("install output must explain how to start Cocola: %q", output.String())
	}
	if !strings.Contains(output.String(), filepath.Join(home, "config.env")) {
		t.Fatalf("install output must show the generated configuration path: %q", output.String())
	}
}

func TestInstallPersistsPublicURLAndReportsIt(t *testing.T) {
	home := filepath.Join(t.TempDir(), "cocola")
	var output, errors bytes.Buffer
	err := Execute(context.Background(), []string{
		"install", "--home", home, "--yes", "--admin-password", "test-password",
		"--public-url", "https://cocola.example.com",
	}, IO{In: &bytes.Buffer{}, Out: &output, Err: &errors})
	if err != nil {
		t.Fatalf("install: %v, stderr=%s", err, errors.String())
	}
	environment, err := os.ReadFile(filepath.Join(home, "config.env"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(environment), `COCOLA_PUBLIC_ORIGINS="http://127.0.0.1:3000,http://localhost:3000,https://cocola.example.com"`) {
		t.Fatalf("generated environment missing public URL: %s", environment)
	}
	if !strings.Contains(output.String(), "https://cocola.example.com") {
		t.Fatalf("install output missing public URL: %q", output.String())
	}
}

func TestRepeatedInstallPreparesUpgradeWithoutPromptingOrReplacingSecrets(t *testing.T) {
	home := filepath.Join(t.TempDir(), "cocola")
	var firstOutput, firstErrors bytes.Buffer
	if err := Execute(context.Background(), []string{
		"install", "--home", home, "--yes", "--version", "v0.1.0",
		"--admin-password", "test-password",
	}, IO{In: &bytes.Buffer{}, Out: &firstOutput, Err: &firstErrors}); err != nil {
		t.Fatalf("first install: %v, stderr=%s", err, firstErrors.String())
	}
	before, err := os.ReadFile(filepath.Join(home, "config.env"))
	if err != nil {
		t.Fatal(err)
	}

	var output, errors bytes.Buffer
	if err := Execute(context.Background(), []string{
		"install", "--home", home, "--version", "v0.2.0",
	}, IO{In: &bytes.Buffer{}, Out: &output, Err: &errors}); err != nil {
		t.Fatalf("repeat install: %v, stderr=%s", err, errors.String())
	}
	after, err := os.ReadFile(filepath.Join(home, "config.env"))
	if err != nil {
		t.Fatal(err)
	}
	for _, secretLine := range strings.Split(string(before), "\n") {
		if !strings.Contains(secretLine, "SECRET") && !strings.Contains(secretLine, "PASSWORD") &&
			!strings.Contains(secretLine, "ADMIN_KEY") && !strings.Contains(secretLine, "AUTH_SECRET") {
			continue
		}
		if !strings.Contains(string(after), secretLine) {
			t.Fatalf("repeat install changed or removed %q", secretLine)
		}
	}
	paths, err := config.ResolvePaths(home)
	if err != nil {
		t.Fatal(err)
	}
	state, err := config.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	if state.PendingUpgrade == nil || state.PendingUpgrade.FromVersion != "v0.1.0" ||
		state.PendingUpgrade.ToVersion != "v0.2.0" {
		t.Fatalf("upgrade state = %+v", state)
	}
	for _, expected := range []string{"Cocola upgrade is ready", "Deployment backup", "$ cocola start"} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("upgrade output missing %q: %q", expected, output.String())
		}
	}
}

func TestInteractiveCommandFailsClearlyWithoutTTY(t *testing.T) {
	var output, errors bytes.Buffer
	err := Execute(context.Background(), []string{"install"}, IO{
		In: &bytes.Buffer{}, Out: &output, Err: &errors,
	})
	if err == nil || !strings.Contains(err.Error(), "requires an interactive terminal") {
		t.Fatalf("error = %v", err)
	}
}

func TestInstallSummaryHighlightsConfigurationAndNextStep(t *testing.T) {
	var output, errors bytes.Buffer
	result := installResult{
		WebURL:        "http://localhost:3000",
		GatewayURL:    "http://localhost:8080",
		AdminUsername: "admin",
		AdminEmail:    "admin@cocola.local",
		AdminPassword: "generated-password",
		ConfigFile:    "/tmp/cocola/config.env",
	}
	printInstallSummary(ui.Printer{Out: &output, Err: &errors}, result)
	for _, expected := range []string{
		"Cocola configuration is ready",
		"Installation summary",
		"/tmp/cocola/config.env",
		"Review the generated configuration file",
		"$ cocola start",
	} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("install summary missing %q: %q", expected, output.String())
		}
	}
	if !strings.Contains(errors.String(), "shown only once") {
		t.Fatalf("install warning missing: %q", errors.String())
	}
}

func TestInstallHelpKeepsInteractiveFlowPrimary(t *testing.T) {
	app := &application{io: IO{In: &bytes.Buffer{}, Out: &bytes.Buffer{}, Err: &bytes.Buffer{}}}
	command := app.installCommand()
	flag := command.Flags().Lookup("yes")
	if flag == nil || !flag.Hidden {
		t.Fatal("the unattended compatibility flag must stay out of the default install help")
	}
}
