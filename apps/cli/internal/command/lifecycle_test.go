package command

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cocola-project/cocola/apps/cli/internal/compose"
	"github.com/cocola-project/cocola/apps/cli/internal/config"
	"github.com/cocola-project/cocola/apps/cli/internal/ui"
)

func TestStartPrintsActionableComposeUpgradeError(t *testing.T) {
	home := filepath.Join(t.TempDir(), "cocola")
	var output, errors bytes.Buffer
	if err := Execute(context.Background(), []string{
		"install", "--home", home, "--yes", "--admin-password", "test-password",
	}, IO{In: &bytes.Buffer{}, Out: &output, Err: &errors}); err != nil {
		t.Fatal(err)
	}

	directory := t.TempDir()
	dockerPath := filepath.Join(directory, "docker")
	script := strings.Join([]string{
		"#!/bin/sh",
		"if [ \"$1\" = \"info\" ]; then exit 0; fi",
		"if [ \"$1\" = \"version\" ]; then printf '20.10.0\n'; exit 0; fi",
		"if [ \"$1 $2 $3\" = \"compose version --short\" ]; then printf '2.22.0\n'; exit 0; fi",
		"exit 1",
		"",
	}, "\n")
	if err := os.WriteFile(dockerPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("COCOLA_DOCKER_BIN", dockerPath)
	t.Setenv("COCOLA_DOCKER_SOCKET_SOURCE", "/var/run/docker.sock")
	output.Reset()
	errors.Reset()
	err := Execute(context.Background(), []string{"start", "--home", home}, IO{
		In: &bytes.Buffer{}, Out: &output, Err: &errors,
	})
	if err == nil || !strings.Contains(errors.String(), "Docker Compose 2.22.0 is too old") ||
		!strings.Contains(errors.String(), "Upgrade Docker Compose") {
		t.Fatalf("start error = %v, stderr = %q", err, errors.String())
	}
}

func TestStartPrintsActionableConfigSchemaMigrationCommands(t *testing.T) {
	home := filepath.Join(t.TempDir(), "cocola user's home")
	var output, stderr bytes.Buffer
	if err := Execute(context.Background(), []string{
		"install", "--home", home, "--yes", "--admin-password", "test-password",
	}, IO{In: &bytes.Buffer{}, Out: &output, Err: &stderr}); err != nil {
		t.Fatal(err)
	}
	downgradeInstalledConfigSchema(t, home)
	setHealthyDockerStub(t)

	output.Reset()
	stderr.Reset()
	err := Execute(context.Background(), []string{"start", "--home", home}, IO{
		In: &bytes.Buffer{}, Out: &output, Err: &stderr,
	})
	installCommand := "cocola install --home " + quoteShellArgument(home)
	startCommand := "cocola start --home " + quoteShellArgument(home)
	for _, expected := range []string{
		fmt.Sprintf("schema %d is older than required schema %d", config.CurrentSchemaVersion-1, config.CurrentSchemaVersion),
		"Run:", installCommand, "Then start Cocola again:", startCommand,
	} {
		if err == nil || !strings.Contains(stderr.String(), expected) {
			t.Fatalf("start error = %v, stderr = %q; missing %q", err, stderr.String(), expected)
		}
	}
}

func TestStartReturnsStructuredConfigSchemaMigrationErrorJSON(t *testing.T) {
	home := filepath.Join(t.TempDir(), "cocola")
	var output, stderr bytes.Buffer
	if err := Execute(context.Background(), []string{
		"install", "--home", home, "--yes", "--admin-password", "test-password",
	}, IO{In: &bytes.Buffer{}, Out: &output, Err: &stderr}); err != nil {
		t.Fatal(err)
	}
	downgradeInstalledConfigSchema(t, home)
	setHealthyDockerStub(t)

	output.Reset()
	stderr.Reset()
	err := Execute(context.Background(), []string{"start", "--home", home, "--json"}, IO{
		In: &bytes.Buffer{}, Out: &output, Err: &stderr,
	})
	if err == nil {
		t.Fatal("start with an outdated config schema unexpectedly succeeded")
	}
	var payload struct {
		Error           string `json:"error"`
		ErrorCode       string `json:"error_code"`
		DetectedSchema  int    `json:"detected_schema"`
		RequiredSchema  int    `json:"required_schema"`
		NextCommand     string `json:"next_command"`
		FollowUpCommand string `json:"follow_up_command"`
	}
	if decodeErr := json.Unmarshal(stderr.Bytes(), &payload); decodeErr != nil {
		t.Fatalf("decode JSON error: %v; stderr=%q", decodeErr, stderr.String())
	}
	if payload.ErrorCode != "deployment_config_schema_outdated" ||
		payload.DetectedSchema != config.CurrentSchemaVersion-1 ||
		payload.RequiredSchema != config.CurrentSchemaVersion ||
		payload.NextCommand != "cocola install --home "+quoteShellArgument(home) ||
		payload.FollowUpCommand != "cocola start --home "+quoteShellArgument(home) {
		t.Fatalf("structured error = %+v", payload)
	}
	if strings.Contains(payload.Error, "\n") {
		t.Fatalf("JSON error message should stay concise: %q", payload.Error)
	}
}

func downgradeInstalledConfigSchema(t *testing.T, home string) {
	t.Helper()
	statePath := filepath.Join(home, "state.json")
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	state["config_schema_version"] = config.CurrentSchemaVersion - 1
	data, err = json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

func setHealthyDockerStub(t *testing.T) {
	t.Helper()
	dockerPath := filepath.Join(t.TempDir(), "docker")
	script := strings.Join([]string{
		"#!/bin/sh",
		"if [ \"$1\" = \"info\" ]; then exit 0; fi",
		"if [ \"$1\" = \"version\" ]; then printf '20.10.0\\n'; exit 0; fi",
		"if [ \"$1 $2 $3\" = \"compose version --short\" ]; then printf '2.23.1\\n'; exit 0; fi",
		"exit 1",
		"",
	}, "\n")
	if err := os.WriteFile(dockerPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("COCOLA_DOCKER_BIN", dockerPath)
	t.Setenv("COCOLA_DOCKER_SOCKET_SOURCE", "/var/run/docker.sock")
}

func TestStartSummaryUsesCommittedVersionAndRegistrySemantics(t *testing.T) {
	state := config.State{
		Version: "v0.2.0", GHCREndpoint: config.DefaultGHCREndpoint,
		PublicURL: "http://localhost:3000", WebPort: 3000,
	}
	pending := &config.PendingUpgrade{
		FromVersion: "v0.1.0", ToVersion: "v0.2.0",
		FromImageRegistry: "ghcr.nju.edu.cn/sakurs2", ToImageRegistry: config.DefaultRegistry,
		FromGHCREndpoint: "ghcr.nju.edu.cn", ToGHCREndpoint: config.DefaultGHCREndpoint,
	}
	var output bytes.Buffer
	printStartSummary(ui.Printer{Out: &output, Err: &output}, state, pending, "")
	text := output.String()
	for _, expected := range []string{
		"Before version", "v0.1.0", "Current version", "v0.2.0",
		"Before GHCR endpoint", "ghcr.nju.edu.cn", "Current GHCR endpoint", config.DefaultGHCREndpoint,
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("start summary missing %q: %q", expected, text)
		}
	}
	if strings.Contains(text, "Target version") || strings.Contains(text, "New version") ||
		strings.Contains(text, "image source") {
		t.Fatalf("successful start still uses preparation copy: %q", text)
	}

	output.Reset()
	pending.FromVersion = state.Version
	pending.ToVersion = state.Version
	pending.FromGHCREndpoint = state.GHCREndpoint
	pending.ToGHCREndpoint = state.GHCREndpoint
	printStartSummary(ui.Printer{Out: &output, Err: &output}, state, pending, "")
	if strings.Contains(output.String(), "Before version") {
		t.Fatalf("same-version registry migration duplicated the version: %q", output.String())
	}
}

func TestStartUsesCachedImagesWhenRegistryIsUnavailableAndSkipsPullOnResume(t *testing.T) {
	home := filepath.Join(t.TempDir(), "cocola")
	webPort, gatewayPort, llmPort := 33001, 33002, 33003
	var installOutput, installErrors bytes.Buffer
	if err := Execute(context.Background(), []string{
		"install", "--home", home, "--yes", "--version", "v0.1.0",
		"--admin-password", "test-password",
		"--web-port", fmt.Sprint(webPort),
		"--gateway-port", fmt.Sprint(gatewayPort),
		"--llm-port", fmt.Sprint(llmPort),
		"--internal-scm-port", "33004",
	}, IO{In: &bytes.Buffer{}, Out: &installOutput, Err: &installErrors}); err != nil {
		t.Fatalf("install: %v, stderr=%s", err, installErrors.String())
	}

	directory := t.TempDir()
	dockerPath := filepath.Join(directory, "docker")
	logPath := filepath.Join(directory, "docker.log")
	script := strings.Join([]string{
		"#!/bin/sh",
		"printf '%s\n' \"$*\" >> \"$DOCKER_ARGS_LOG\"",
		"if [ \"$1 $2 $3\" = \"compose version --short\" ]; then printf '2.23.1\n'; exit 0; fi",
		"if [ \"$1\" = \"info\" ]; then exit 0; fi",
		"if [ \"$1\" = \"version\" ]; then printf '20.10.0\n'; exit 0; fi",
		"if [ \"$1\" = \"image\" ]; then exit 0; fi",
		"if [ \"$1\" = \"ps\" ]; then printf 'container-id\n'; exit 0; fi",
		"case \"$*\" in",
		"  *' config --images'*) printf '%s\n' 'redis:7.4.10-alpine3.21' 'ghcr.io/sakurs2/cocola-web:v0.1.0'; exit 0 ;;",
		"  *' ps -aq'*) [ \"$HAS_CONTAINERS\" != 1 ] || printf 'container-id\n'; exit 0 ;;",
		"  *' pull') [ \"$FAIL_PULL\" != 1 ]; exit $? ;;",
		"esac",
		"exit 0",
		"",
	}, "\n")
	if err := os.WriteFile(dockerPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("COCOLA_DOCKER_BIN", dockerPath)
	t.Setenv("COCOLA_DOCKER_SOCKET_SOURCE", "/var/run/docker.sock")
	t.Setenv("DOCKER_ARGS_LOG", logPath)
	t.Setenv("FAIL_PULL", "1")
	t.Setenv("HAS_CONTAINERS", "1")
	var output, errors bytes.Buffer
	if err := Execute(context.Background(), []string{"start", "--home", home}, IO{
		In: &bytes.Buffer{}, Out: &output, Err: &errors,
	}); err != nil {
		t.Fatalf("first start: %v, stdout=%s stderr=%s", err, output.String(), errors.String())
	}
	if !strings.Contains(errors.String(), "all required images are cached locally") {
		t.Fatalf("missing cache fallback warning: %q", errors.String())
	}
	if !strings.Contains(output.String(), "Cocola is ready") ||
		!strings.Contains(output.String(), "/admin/models") ||
		!strings.Contains(output.String(), "Current version") ||
		strings.Contains(output.String(), "Image source") {
		t.Fatalf("start summary = %q", output.String())
	}
	paths, err := config.ResolvePaths(home)
	if err != nil {
		t.Fatal(err)
	}
	state, err := config.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	if state.LastSuccessfulVersion != "v0.1.0" {
		t.Fatalf("started state = %+v", state)
	}

	if err := os.WriteFile(logPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAIL_PULL", "0")
	t.Setenv("HAS_CONTAINERS", "1")
	output.Reset()
	errors.Reset()
	if err := Execute(context.Background(), []string{"start", "--home", home}, IO{
		In: &bytes.Buffer{}, Out: &output, Err: &errors,
	}); err != nil {
		t.Fatalf("resume: %v, stdout=%s stderr=%s", err, output.String(), errors.String())
	}
	logged, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(logged), " pull") {
		t.Fatalf("resume unexpectedly pulled images: %s", logged)
	}
}

func TestStartGHCREndpointSuccessPersistsAndReturnsJSON(t *testing.T) {
	home := filepath.Join(t.TempDir(), "cocola")
	var output, stderr bytes.Buffer
	if err := Execute(context.Background(), []string{
		"install", "--home", home, "--yes", "--version", "v0.1.0", "--admin-password", "test-password",
		"--web-port", "33501", "--gateway-port", "33502", "--llm-port", "33503", "--internal-scm-port", "33504",
	}, IO{In: &bytes.Buffer{}, Out: &output, Err: &stderr}); err != nil {
		t.Fatal(err)
	}
	paths, err := config.ResolvePaths(home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := config.MarkStarted(paths); err != nil {
		t.Fatal(err)
	}

	directory := t.TempDir()
	dockerPath := filepath.Join(directory, "docker")
	script := `#!/bin/sh
if [ "$1 $2 $3" = "compose version --short" ]; then printf '2.23.1\n'; exit 0; fi
if [ "$1" = "info" ]; then exit 0; fi
if [ "$1" = "version" ]; then printf '20.10.0\n'; exit 0; fi
if [ "$1" = "ps" ]; then printf 'container-id\n'; exit 0; fi
if [ "$1 $2" = "image inspect" ]; then exit 0; fi
case "$*" in
  *' config --quiet') exit 0 ;;
  *' config --images') printf '%s\n' 'ghcr.nju.edu.cn/sakurs2/cocola-redis:7.4.10-alpine3.21' 'ghcr.nju.edu.cn/sakurs2/cocola-web:v0.1.0'; exit 0 ;;
  *' pull') exit 0 ;;
  *' ps -aq') printf 'container-id\n'; exit 0 ;;
esac
exit 0
`
	if err := os.WriteFile(dockerPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("COCOLA_DOCKER_BIN", dockerPath)
	t.Setenv("COCOLA_DOCKER_SOCKET_SOURCE", "/var/run/docker.sock")
	output.Reset()
	stderr.Reset()
	if err := Execute(context.Background(), []string{
		"start", "--json", "--home", home, "--ghcr-endpoint", "ghcr.nju.edu.cn",
	}, IO{In: &bytes.Buffer{}, Out: &output, Err: &stderr}); err != nil {
		t.Fatalf("start endpoint: %v, stdout=%s stderr=%s", err, output.String(), stderr.String())
	}
	var result lifecycleResult
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatalf("decode start JSON: %v, output=%s", err, output.String())
	}
	if result.GHCREndpoint != "ghcr.nju.edu.cn" || result.PreviousGHCREndpoint != config.DefaultGHCREndpoint {
		t.Fatalf("start JSON = %+v", result)
	}
	state, err := config.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	if state.GHCREndpoint != "ghcr.nju.edu.cn" || state.PendingUpgrade != nil {
		t.Fatalf("committed endpoint state = %+v", state)
	}
}

func TestStartWithoutEndpointDiscardsInterruptedEndpointCandidate(t *testing.T) {
	home := filepath.Join(t.TempDir(), "cocola")
	var output, stderr bytes.Buffer
	if err := Execute(context.Background(), []string{
		"install", "--home", home, "--yes", "--version", "v0.1.0", "--admin-password", "test-password",
		"--web-port", "33801", "--gateway-port", "33802", "--llm-port", "33803", "--internal-scm-port", "33804",
	}, IO{In: &bytes.Buffer{}, Out: &output, Err: &stderr}); err != nil {
		t.Fatal(err)
	}
	paths, err := config.ResolvePaths(home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := config.MarkStarted(paths); err != nil {
		t.Fatal(err)
	}
	if _, err := config.PrepareGHCREndpointChange(paths, "ghcr.nju.edu.cn"); err != nil {
		t.Fatal(err)
	}

	directory := t.TempDir()
	dockerPath := filepath.Join(directory, "docker")
	logPath := filepath.Join(directory, "docker.log")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$DOCKER_ARGS_LOG"
if [ "$1 $2 $3" = "compose version --short" ]; then printf '2.23.1\n'; exit 0; fi
if [ "$1" = "info" ]; then exit 0; fi
if [ "$1" = "version" ]; then printf '20.10.0\n'; exit 0; fi
case "$*" in
  *' config --quiet') exit 0 ;;
  *' config --images') printf '%s\n' 'ghcr.io/sakurs2/cocola-redis:7.4.10-alpine3.21' 'ghcr.io/sakurs2/cocola-web:v0.1.0'; exit 0 ;;
  *' ps -aq') printf 'container-id\n'; exit 0 ;;
esac
exit 0
`
	if err := os.WriteFile(dockerPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("COCOLA_DOCKER_BIN", dockerPath)
	t.Setenv("COCOLA_DOCKER_SOCKET_SOURCE", "/var/run/docker.sock")
	t.Setenv("DOCKER_ARGS_LOG", logPath)
	output.Reset()
	stderr.Reset()
	if err := Execute(context.Background(), []string{"start", "--home", home}, IO{
		In: &bytes.Buffer{}, Out: &output, Err: &stderr,
	}); err != nil {
		t.Fatalf("resume without endpoint: %v, stdout=%s stderr=%s", err, output.String(), stderr.String())
	}
	state, err := config.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	if state.GHCREndpoint != config.DefaultGHCREndpoint || state.PendingUpgrade != nil {
		t.Fatalf("interrupted endpoint was committed: %+v", state)
	}
	environment, err := os.ReadFile(paths.Environment)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(environment), "ghcr.nju.edu.cn") {
		t.Fatalf("interrupted endpoint remains in config.env:\n%s", environment)
	}
	logged, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(logged), " pull") {
		t.Fatalf("plain start retried candidate endpoint: %s", logged)
	}
}

func TestStartGHCREndpointPullFailureRollsBackAndPrintsRetry(t *testing.T) {
	home := filepath.Join(t.TempDir(), "cocola")
	var output, stderr bytes.Buffer
	if err := Execute(context.Background(), []string{
		"install", "--home", home, "--yes", "--version", "v0.1.0", "--admin-password", "test-password",
		"--web-port", "33601", "--gateway-port", "33602", "--llm-port", "33603", "--internal-scm-port", "33604",
	}, IO{In: &bytes.Buffer{}, Out: &output, Err: &stderr}); err != nil {
		t.Fatal(err)
	}
	paths, err := config.ResolvePaths(home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := config.MarkStarted(paths); err != nil {
		t.Fatal(err)
	}

	directory := t.TempDir()
	dockerPath := filepath.Join(directory, "docker")
	script := `#!/bin/sh
if [ "$1 $2 $3" = "compose version --short" ]; then printf '2.23.1\n'; exit 0; fi
if [ "$1" = "info" ]; then exit 0; fi
if [ "$1" = "version" ]; then printf '20.10.0\n'; exit 0; fi
if [ "$1 $2" = "image inspect" ]; then exit 1; fi
case "$*" in
  *' config --quiet') exit 0 ;;
  *' config --images') printf '%s\n' 'ghcr.nju.edu.cn/sakurs2/cocola-redis:7.4.10-alpine3.21'; exit 0 ;;
  *' pull') exit 1 ;;
  *' ps --format json') printf '[]\n'; exit 0 ;;
  *' ps -aq') exit 0 ;;
esac
exit 0
`
	if err := os.WriteFile(dockerPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("COCOLA_DOCKER_BIN", dockerPath)
	t.Setenv("COCOLA_DOCKER_SOCKET_SOURCE", "/var/run/docker.sock")
	output.Reset()
	stderr.Reset()
	err = Execute(context.Background(), []string{
		"start", "--home", home, "--ghcr-endpoint", "ghcr.nju.edu.cn",
	}, IO{In: &bytes.Buffer{}, Out: &output, Err: &stderr})
	if err == nil {
		t.Fatal("failed endpoint pull unexpectedly succeeded")
	}
	state, loadErr := config.Load(paths)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if state.GHCREndpoint != config.DefaultGHCREndpoint || state.PendingUpgrade != nil {
		t.Fatalf("endpoint rollback state = %+v", state)
	}
	environment, readErr := os.ReadFile(paths.Environment)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if strings.Contains(string(environment), "ghcr.nju.edu.cn") {
		t.Fatalf("endpoint rollback left proxy references:\n%s", environment)
	}
	combined := output.String() + stderr.String()
	if !strings.Contains(combined, "cocola start --ghcr-endpoint ghcr.nju.edu.cn") ||
		strings.Contains(combined, "cocola install --version") {
		t.Fatalf("endpoint retry instructions = %q", combined)
	}
}

func TestStartGHCREndpointPreflightFailureRollsBack(t *testing.T) {
	home := filepath.Join(t.TempDir(), "cocola")
	var output, stderr bytes.Buffer
	if err := Execute(context.Background(), []string{
		"install", "--home", home, "--yes", "--version", "v0.1.0", "--admin-password", "test-password",
		"--web-port", "33701", "--gateway-port", "33702", "--llm-port", "33703", "--internal-scm-port", "33704",
	}, IO{In: &bytes.Buffer{}, Out: &output, Err: &stderr}); err != nil {
		t.Fatal(err)
	}
	paths, err := config.ResolvePaths(home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := config.MarkStarted(paths); err != nil {
		t.Fatal(err)
	}

	dockerPath := filepath.Join(t.TempDir(), "docker")
	script := `#!/bin/sh
if [ "$1 $2 $3" = "compose version --short" ]; then printf '2.23.1\n'; exit 0; fi
if [ "$1" = "info" ]; then exit 0; fi
if [ "$1" = "version" ]; then printf '20.10.0\n'; exit 0; fi
case "$*" in
  *' config --quiet') printf 'invalid candidate compose\n' >&2; exit 1 ;;
  *' ps --format json') printf '[]\n'; exit 0 ;;
esac
exit 0
`
	if err := os.WriteFile(dockerPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("COCOLA_DOCKER_BIN", dockerPath)
	t.Setenv("COCOLA_DOCKER_SOCKET_SOURCE", "/var/run/docker.sock")
	output.Reset()
	stderr.Reset()
	err = Execute(context.Background(), []string{
		"start", "--home", home, "--ghcr-endpoint", "ghcr.nju.edu.cn",
	}, IO{In: &bytes.Buffer{}, Out: &output, Err: &stderr})
	if err == nil {
		t.Fatal("invalid candidate preflight unexpectedly succeeded")
	}
	state, loadErr := config.Load(paths)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if state.GHCREndpoint != config.DefaultGHCREndpoint || state.PendingUpgrade != nil {
		t.Fatalf("preflight rollback state = %+v", state)
	}
	environment, readErr := os.ReadFile(paths.Environment)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if strings.Contains(string(environment), "ghcr.nju.edu.cn") {
		t.Fatalf("preflight rollback left proxy references:\n%s", environment)
	}
}

func TestPullFailureUsesStandardDiagnosticsWithoutRemovedSourceHint(t *testing.T) {
	home := filepath.Join(t.TempDir(), "cocola")
	var output, stderr bytes.Buffer
	if err := Execute(context.Background(), []string{
		"install", "--home", home, "--yes", "--version", "v0.1.0",
		"--admin-password", "test-password", "--web-port", "33401",
		"--gateway-port", "33402", "--llm-port", "33403", "--internal-scm-port", "33404",
	}, IO{In: &bytes.Buffer{}, Out: &output, Err: &stderr}); err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	dockerPath := filepath.Join(directory, "docker")
	logPath := filepath.Join(directory, "docker.log")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$DOCKER_ARGS_LOG"
if [ "$1 $2 $3" = "compose version --short" ]; then printf '2.23.1\n'; exit 0; fi
if [ "$1" = "info" ]; then exit 0; fi
if [ "$1" = "version" ]; then printf '20.10.0\n'; exit 0; fi
if [ "$1" = "image" ]; then exit 1; fi
case "$*" in
  *'config --images'*) printf '%s\n' 'docker.io/library/redis:7.4.10-alpine3.21' 'ghcr.io/sakurs2/cocola-web:v0.1.0'; exit 0 ;;
  *' config --quiet'*) exit 0 ;;
  *' pull') exit 1 ;;
  *' ps --format json'*) printf '[]\n'; exit 0 ;;
  *' ps -aq'*) exit 0 ;;
esac
exit 0
`
	if err := os.WriteFile(dockerPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("COCOLA_DOCKER_BIN", dockerPath)
	t.Setenv("COCOLA_DOCKER_SOCKET_SOURCE", "/var/run/docker.sock")
	t.Setenv("DOCKER_ARGS_LOG", logPath)
	output.Reset()
	stderr.Reset()
	err := Execute(context.Background(), []string{"start", "--home", home}, IO{
		In: &bytes.Buffer{}, Out: &output, Err: &stderr,
	})
	if err == nil {
		t.Fatal("image pull failure unexpectedly succeeded")
	}
	combined := output.String() + stderr.String()
	if !strings.Contains(combined, "cocola logs --tail 200") ||
		!strings.Contains(combined, "cocola doctor") || strings.Contains(combined, "--image-source") {
		t.Fatalf("pull failure diagnostics = %q", combined)
	}
	logged, readErr := os.ReadFile(logPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if strings.Contains(string(logged), "nju.edu.cn") {
		t.Fatalf("pull failure used a retired mirror:\n%s", logged)
	}
}

func TestFailedUpgradeRestoresPreviousDeploymentWithoutRestartingIt(t *testing.T) {
	home := filepath.Join(t.TempDir(), "cocola")
	webPort, gatewayPort, llmPort := 33101, 33102, 33103
	var output, errors bytes.Buffer
	if err := Execute(context.Background(), []string{
		"install", "--home", home, "--yes", "--version", "v0.1.0",
		"--admin-password", "test-password",
		"--web-port", fmt.Sprint(webPort),
		"--gateway-port", fmt.Sprint(gatewayPort),
		"--llm-port", fmt.Sprint(llmPort),
		"--internal-scm-port", "33104",
	}, IO{In: &bytes.Buffer{}, Out: &output, Err: &errors}); err != nil {
		t.Fatal(err)
	}
	paths, err := config.ResolvePaths(home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := config.MarkStarted(paths); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	errors.Reset()
	if err := Execute(context.Background(), []string{
		"install", "--home", home, "--version", "v0.2.0",
	}, IO{In: &bytes.Buffer{}, Out: &output, Err: &errors}); err != nil {
		t.Fatal(err)
	}

	directory := t.TempDir()
	dockerPath := filepath.Join(directory, "docker")
	dockerLogPath := filepath.Join(directory, "docker.log")
	script := strings.Join([]string{
		"#!/bin/sh",
		"printf '%s\n' \"$*\" >> \"$DOCKER_ARGS_LOG\"",
		"if [ \"$1 $2 $3\" = \"compose version --short\" ]; then printf '2.23.1\n'; exit 0; fi",
		"if [ \"$1\" = \"info\" ]; then exit 0; fi",
		"if [ \"$1\" = \"version\" ]; then printf '20.10.0\n'; exit 0; fi",
		"if [ \"$1\" = \"ps\" ]; then printf 'container-id\n'; exit 0; fi",
		"if [ \"$1\" = \"volume\" ]; then printf 'Error: No such volume\n' >&2; exit 1; fi",
		"case \"$*\" in",
		"  *' ps -aq'*) printf 'container-id\n'; exit 0 ;;",
		"  *' pull') exit 0 ;;",
		"  *' up -d --wait opensandbox-server'*) exit 1 ;;",
		"esac",
		"exit 0",
		"",
	}, "\n")
	if err := os.WriteFile(dockerPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("COCOLA_DOCKER_BIN", dockerPath)
	t.Setenv("COCOLA_DOCKER_SOCKET_SOURCE", "/var/run/docker.sock")
	t.Setenv("DOCKER_ARGS_LOG", dockerLogPath)
	output.Reset()
	errors.Reset()
	err = Execute(context.Background(), []string{"start", "--home", home}, IO{
		In: &bytes.Buffer{}, Out: &output, Err: &errors,
	})
	if err == nil {
		t.Fatal("failed target startup unexpectedly succeeded")
	}
	state, loadErr := config.Load(paths)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if state.Version != "v0.1.0" || state.PendingUpgrade != nil {
		t.Fatalf("rollback state = %+v", state)
	}
	environment, readErr := os.ReadFile(paths.Environment)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !strings.Contains(string(environment), "COCOLA_VERSION=\"v0.1.0\"") {
		t.Fatalf("rollback environment = %s", environment)
	}
	combined := output.String() + errors.String()
	if !strings.Contains(combined, "was not restarted automatically") ||
		!strings.Contains(combined, "cocola start") ||
		!strings.Contains(combined, "cocola install --version v0.2.0") ||
		!strings.Contains(combined, "never restored automatically") {
		t.Fatalf("rollback output = %q", combined)
	}
	dockerLog, readErr := os.ReadFile(dockerLogPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if count := strings.Count(string(dockerLog), "up -d --wait opensandbox-server"); count != 1 {
		t.Fatalf("previous version was restarted automatically; start count = %d\n%s", count, dockerLog)
	}
	if !strings.Contains(string(dockerLog), "down --remove-orphans --timeout 30") {
		t.Fatalf("failed candidate containers were not removed before rollback:\n%s", dockerLog)
	}
	if strings.Contains(string(dockerLog), "down --remove-orphans --timeout 30 --volumes") {
		t.Fatalf("failed candidate cleanup unexpectedly removed data volumes:\n%s", dockerLog)
	}
}

func TestUpgradeBackupIgnoresStrayForgejoVolumeWhenPreviousTopologyHasNoForgejo(t *testing.T) {
	home := filepath.Join(t.TempDir(), "cocola")
	var output, stderr bytes.Buffer
	if err := Execute(context.Background(), []string{
		"install", "--home", home, "--yes", "--version", "v0.1.0",
		"--admin-password", "test-password",
		"--web-port", "33301", "--gateway-port", "33302", "--llm-port", "33303",
		"--internal-scm-port", "33304",
	}, IO{In: &bytes.Buffer{}, Out: &output, Err: &stderr}); err != nil {
		t.Fatal(err)
	}
	paths, err := config.ResolvePaths(home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := config.MarkStarted(paths); err != nil {
		t.Fatal(err)
	}
	if err := Execute(context.Background(), []string{
		"install", "--home", home, "--version", "v0.2.0",
	}, IO{In: &bytes.Buffer{}, Out: &output, Err: &stderr}); err != nil {
		t.Fatal(err)
	}
	state, err := config.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	if state.PendingUpgrade == nil {
		t.Fatal("upgrade did not create pending backup state")
	}

	directory := t.TempDir()
	dockerPath := filepath.Join(directory, "docker")
	logPath := filepath.Join(directory, "docker.log")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$DOCKER_ARGS_LOG"
case "$*" in
  *'config --services'*) printf '%s\n' 'postgres' 'gateway'; exit 0 ;;
  'volume inspect cocola_pgdata') exit 0 ;;
  *'up -d --wait postgres') exit 0 ;;
  *'exec -T postgres pg_dump -U cocola -d cocola --format=custom'*) printf 'postgres-dump'; exit 0 ;;
esac
printf '%s\n' 'unexpected docker command' >&2
exit 1
`
	if err := os.WriteFile(dockerPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("COCOLA_DOCKER_BIN", dockerPath)
	t.Setenv("COCOLA_DOCKER_SOCKET_SOURCE", "/var/run/docker.sock")
	t.Setenv("DOCKER_ARGS_LOG", logPath)
	app := &application{io: IO{In: &bytes.Buffer{}, Out: &output, Err: &stderr}}
	if err := app.backupUpgradeDatabase(context.Background(), paths, state.PendingUpgrade); err != nil {
		t.Fatalf("backupUpgradeDatabase() error = %v, stderr = %s", err, stderr.String())
	}
	logged, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(logged), "forgejo") {
		t.Fatalf("old topology without Forgejo unexpectedly accessed Forgejo data:\n%s", logged)
	}
	if !strings.Contains(string(logged), "pg_dump -U cocola -d cocola") {
		t.Fatalf("PostgreSQL backup was not created:\n%s", logged)
	}
}

func TestFirstStartRejectsPostgresVolumeFromDifferentConfiguration(t *testing.T) {
	home := filepath.Join(t.TempDir(), "cocola")
	webPort, gatewayPort, llmPort := 33201, 33202, 33203
	var output, stderr bytes.Buffer
	if err := Execute(context.Background(), []string{
		"install", "--home", home, "--yes", "--version", "v0.1.0",
		"--admin-password", "test-password",
		"--web-port", fmt.Sprint(webPort),
		"--gateway-port", fmt.Sprint(gatewayPort),
		"--llm-port", fmt.Sprint(llmPort),
		"--internal-scm-port", "33204",
	}, IO{In: &bytes.Buffer{}, Out: &output, Err: &stderr}); err != nil {
		t.Fatal(err)
	}

	directory := t.TempDir()
	dockerPath := filepath.Join(directory, "docker")
	script := strings.Join([]string{
		"#!/bin/sh",
		"if [ \"$1 $2 $3\" = \"compose version --short\" ]; then printf '2.23.1\\n'; exit 0; fi",
		"if [ \"$1\" = \"info\" ]; then exit 0; fi",
		"if [ \"$1\" = \"version\" ]; then printf '20.10.0\\n'; exit 0; fi",
		"if [ \"$1\" = \"ps\" ]; then exit 0; fi",
		"if [ \"$1 $2 $3\" = \"volume inspect cocola_pgdata\" ]; then exit 0; fi",
		"if [ \"$1 $2\" = \"image pull\" ]; then exit 0; fi",
		"case \"$*\" in",
		"  *' config --quiet') exit 0 ;;",
		"  *' pull') exit 0 ;;",
		"  *' up -d postgres') exit 0 ;;",
		"  *'exec -T postgres sh -ec'*) printf '%s\\n' 'password authentication failed for user \"cocola\"' >&2; exit 2 ;;",
		"  *' ps') exit 0 ;;",
		"esac",
		"exit 1",
		"",
	}, "\n")
	if err := os.WriteFile(dockerPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("COCOLA_DOCKER_BIN", dockerPath)
	t.Setenv("COCOLA_DOCKER_SOCKET_SOURCE", "/var/run/docker.sock")
	output.Reset()
	stderr.Reset()
	err := Execute(context.Background(), []string{"start", "--home", home}, IO{
		In: &bytes.Buffer{}, Out: &output, Err: &stderr,
	})
	combined := output.String() + stderr.String()
	if !errors.Is(err, compose.ErrPostgresCredentialsMismatch) {
		t.Fatalf("start error = %v, output = %q", err, combined)
	}
	if !strings.Contains(combined, "different Cocola configuration") ||
		!strings.Contains(combined, "docker volume rm cocola_pgdata") ||
		strings.Contains(combined, "Cocola is ready") {
		t.Fatalf("PostgreSQL mismatch output = %q", combined)
	}
}

func TestCheckPortAvailableReportsCollision(t *testing.T) {
	previous := listenTCP
	var network, address string
	listenTCP = func(gotNetwork, gotAddress string) (net.Listener, error) {
		network, address = gotNetwork, gotAddress
		return nil, errors.New("address already in use")
	}
	defer func() { listenTCP = previous }()
	if err := checkPortAvailable("127.0.0.1", 33001); err == nil || !strings.Contains(err.Error(), "address already in use") {
		t.Fatalf("port collision error = %v", err)
	}
	if network != "tcp4" || address != "127.0.0.1:33001" {
		t.Fatalf("listen address = %s %s", network, address)
	}
}

func TestInternalSCMPreflightUsesLoopbackBinding(t *testing.T) {
	bindings := startPortBindings(config.State{
		WebPort: 3000, GatewayPort: 8080, LLMPort: 18091,
		InternalSCM: config.InternalSCMEndpoint{HostPort: 3001},
	})
	for _, binding := range bindings {
		if binding.service != "forgejo" {
			continue
		}
		if binding.bindHost != "127.0.0.1" || binding.port != 3001 {
			t.Fatalf("Internal SCM binding = %+v", binding)
		}
		return
	}
	t.Fatal("Internal SCM preflight binding is missing")
}

func TestPrepareSandboxRootCreatesWritableDirectory(t *testing.T) {
	root := filepath.Join(t.TempDir(), "sandboxes")
	if err := prepareSandboxRoot(root); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(root)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() {
		t.Fatalf("sandbox root is not a directory: %s", root)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("sandbox root contains write-check artifacts: %v", entries)
	}
}

func TestPrepareSandboxRootRejectsRelativePath(t *testing.T) {
	if err := prepareSandboxRoot("relative/sandboxes"); err == nil || !strings.Contains(err.Error(), "must be absolute") {
		t.Fatalf("prepareSandboxRoot() error = %v", err)
	}
}
