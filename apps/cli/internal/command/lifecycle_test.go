package command

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cocola-project/cocola/apps/cli/internal/config"
)

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
	}, IO{In: &bytes.Buffer{}, Out: &installOutput, Err: &installErrors}); err != nil {
		t.Fatalf("install: %v, stderr=%s", err, installErrors.String())
	}

	directory := t.TempDir()
	dockerPath := filepath.Join(directory, "docker")
	logPath := filepath.Join(directory, "docker.log")
	script := strings.Join([]string{
		"#!/bin/sh",
		"printf '%s\n' \"$*\" >> \"$DOCKER_ARGS_LOG\"",
		"if [ \"$1 $2 $3\" = \"compose version --short\" ]; then printf '2.1.1\n'; exit 0; fi",
		"if [ \"$1\" = \"info\" ]; then exit 0; fi",
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
		!strings.Contains(output.String(), "/admin/models") {
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
		"if [ \"$1 $2 $3\" = \"compose version --short\" ]; then printf '2.1.1\n'; exit 0; fi",
		"if [ \"$1\" = \"info\" ]; then exit 0; fi",
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
}

func TestCheckPortAvailableReportsCollision(t *testing.T) {
	previous := listenTCP
	listenTCP = func(_, _ string) (net.Listener, error) {
		return nil, errors.New("address already in use")
	}
	defer func() { listenTCP = previous }()
	if err := checkPortAvailable(33001); err == nil || !strings.Contains(err.Error(), "address already in use") {
		t.Fatalf("port collision error = %v", err)
	}
}
