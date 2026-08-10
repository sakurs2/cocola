package command

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cocola-project/cocola/apps/cli/internal/config"
	"github.com/cocola-project/cocola/apps/cli/internal/operationlock"
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
	if !strings.Contains(string(compose), "content: |") || !strings.Contains(string(compose), "\nconfigs:") {
		t.Fatal("embedded release compose must include the OpenSandbox config inline")
	}
	if !strings.Contains(string(compose), `allowed_host_paths = ["${COCOLA_SANDBOX_ROOT}"]`) {
		t.Fatal("embedded release compose does not configure the sandbox root")
	}
	if strings.Count(string(compose), `"${COCOLA_SANDBOX_ROOT}:${COCOLA_SANDBOX_ROOT}"`) != 2 {
		t.Fatal("embedded release compose must mount the sandbox root path-isomorphically into sandbox-manager and OpenSandbox")
	}
	if strings.Count(string(compose), `"${COCOLA_DOCKER_SOCKET_SOURCE:-/var/run/docker.sock}:/var/run/docker.sock"`) != 2 {
		t.Fatal("embedded release compose must use the resolved Docker socket for host-agent and OpenSandbox")
	}
	for _, floating := range []string{
		"redis:7-alpine", "postgres:16-alpine", "minio/minio:latest",
		"minio/mc:latest", "opensandbox/server:latest",
	} {
		if strings.Contains(string(compose), floating) {
			t.Fatalf("embedded release compose uses floating image %q", floating)
		}
	}
	if _, err := os.Stat(filepath.Join(home, "opensandbox.toml")); !os.IsNotExist(err) {
		t.Fatalf("install generated an unnecessary OpenSandbox config file: %v", err)
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
	for _, expected := range []string{
		`COCOLA_IMAGE_REGISTRY="ghcr.io/sakurs2"`,
		`COCOLA_REDIS_IMAGE="docker.io/library/redis:7.4.10-alpine3.21"`,
		`COCOLA_FORGEJO_IMAGE="ghcr.io/sakurs2/cocola-forgejo:16.0.1@sha256:3eb3107`,
	} {
		if !strings.Contains(string(environment), expected) {
			t.Fatalf("generated environment missing %q: %s", expected, environment)
		}
	}
	if strings.Contains(string(environment), "COCOLA_IMAGE_SOURCE") || strings.Contains(string(environment), "nju.edu.cn") {
		t.Fatalf("generated environment contains removed image-source configuration: %s", environment)
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

func TestInstallRejectsRemovedImageSourceFlag(t *testing.T) {
	home := filepath.Join(t.TempDir(), "cocola")
	var output, stderr bytes.Buffer
	err := Execute(context.Background(), []string{
		"install", "--home", home, "--yes", "--admin-password", "test-password",
		"--image-source", "direct",
	}, IO{In: &bytes.Buffer{}, Out: &output, Err: &stderr})
	if err == nil || !strings.Contains(err.Error(), "unknown flag: --image-source") {
		t.Fatalf("removed image-source flag error = %v, stderr=%s", err, stderr.String())
	}
}

func TestInstallJSONOmitsRemovedImageSource(t *testing.T) {
	home := filepath.Join(t.TempDir(), "cocola")
	var output, stderr bytes.Buffer
	if err := Execute(context.Background(), []string{
		"install", "--json", "--home", home, "--yes", "--admin-password", "test-password",
	}, IO{In: &bytes.Buffer{}, Out: &output, Err: &stderr}); err != nil {
		t.Fatalf("install: %v, stderr=%s", err, stderr.String())
	}
	var result map[string]any
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatalf("decode install JSON: %v, output=%s", err, output.String())
	}
	if _, exists := result["image_source"]; exists {
		t.Fatalf("install JSON contains removed image_source field: %s", output.String())
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
	if !strings.Contains(output.String(), "Before version") || !strings.Contains(output.String(), "New version") ||
		strings.Contains(output.String(), "Target version") || strings.Contains(output.String(), "Current version") {
		t.Fatalf("upgrade preparation uses misleading version copy: %q", output.String())
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
	for _, expected := range []string{"Cocola deployment update is ready", "Deployment backup", "$ cocola start"} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("upgrade output missing %q: %q", expected, output.String())
		}
	}
}

func TestInteractiveCommandFailsClearlyWithoutTTY(t *testing.T) {
	var output, errors bytes.Buffer
	err := Execute(context.Background(), []string{"install", "--home", t.TempDir()}, IO{
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

func TestMutatingCommandsRespectInstallationOperationLock(t *testing.T) {
	home := filepath.Join(t.TempDir(), "cocola")
	var output, errors bytes.Buffer
	if err := Execute(context.Background(), []string{
		"install", "--home", home, "--yes", "--admin-password", "test-password",
	}, IO{In: &bytes.Buffer{}, Out: &output, Err: &errors}); err != nil {
		t.Fatal(err)
	}
	lock, err := operationlock.Acquire(home, "cocola start")
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	for _, command := range []string{"install", "start", "stop"} {
		output.Reset()
		errors.Reset()
		err := Execute(context.Background(), []string{command, "--home", home}, IO{
			In: &bytes.Buffer{}, Out: &output, Err: &errors,
		})
		if err == nil || !strings.Contains(err.Error(), "another Cocola operation") ||
			!strings.Contains(err.Error(), "cocola start") {
			t.Fatalf("%s error = %v", command, err)
		}
	}
}
