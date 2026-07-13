package compose

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cocola-project/cocola/apps/cli/internal/config"
)

func TestRunnerUsesManagedProfileAndStartsOpenSandboxFirst(t *testing.T) {
	directory := t.TempDir()
	logPath := filepath.Join(directory, "args.log")
	dockerPath := filepath.Join(directory, "docker")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$DOCKER_ARGS_LOG\"\n"
	if err := os.WriteFile(dockerPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("COCOLA_DOCKER_BIN", dockerPath)
	t.Setenv("DOCKER_ARGS_LOG", logPath)
	paths := config.Paths{
		Home: directory, Environment: filepath.Join(directory, "config.env"),
		Compose: filepath.Join(directory, "compose.yaml"), State: filepath.Join(directory, "state.json"),
	}
	if err := os.WriteFile(paths.State, []byte(`{"version":"v1","managed_opensandbox":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	runner, err := New(paths, nil, &bytes.Buffer{}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(contents)), "\n")
	if len(lines) != 2 {
		t.Fatalf("commands = %q", lines)
	}
	if !strings.Contains(lines[0], "--profile managed up -d --wait opensandbox-server") {
		t.Fatalf("first command = %q", lines[0])
	}
	if !strings.Contains(lines[1], "--profile managed up -d --remove-orphans --wait") {
		t.Fatalf("second command = %q", lines[1])
	}
}

func TestManagedDownStopsWorkersCleansSandboxesThenRemovesStack(t *testing.T) {
	directory := t.TempDir()
	logPath := filepath.Join(directory, "args.log")
	dockerPath := filepath.Join(directory, "docker")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$DOCKER_ARGS_LOG\"\n" +
		"if [ \"$1 $2\" = \"ps -aq\" ]; then printf 'sandbox-1\\nsandbox-2\\n'; fi\n"
	if err := os.WriteFile(dockerPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("COCOLA_DOCKER_BIN", dockerPath)
	t.Setenv("DOCKER_ARGS_LOG", logPath)
	paths := config.Paths{
		Home: directory, Environment: filepath.Join(directory, "config.env"),
		Compose: filepath.Join(directory, "compose.yaml"), State: filepath.Join(directory, "state.json"),
	}
	state := `{"version":"v1","managed_opensandbox":true,"sandbox_image":"registry/cocola-sandbox-runtime:v1"}`
	if err := os.WriteFile(paths.State, []byte(state), 0o600); err != nil {
		t.Fatal(err)
	}
	runner, err := New(paths, nil, &bytes.Buffer{}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.Down(context.Background()); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(contents)), "\n")
	want := []string{
		"compose --project-name cocola --env-file " + paths.Environment + " --file " + paths.Compose + " --profile managed stop --timeout 30 web gateway agent-runtime",
		"compose --project-name cocola --env-file " + paths.Environment + " --file " + paths.Compose + " --profile managed stop --timeout 45 sandbox-manager",
		"ps -aq --filter ancestor=registry/cocola-sandbox-runtime:v1",
		"rm -f sandbox-1 sandbox-2",
		"compose --project-name cocola --env-file " + paths.Environment + " --file " + paths.Compose + " --profile managed down --remove-orphans",
	}
	if len(lines) != len(want) {
		t.Fatalf("commands = %q", lines)
	}
	for index := range want {
		if lines[index] != want[index] {
			t.Fatalf("command %d = %q, want %q", index, lines[index], want[index])
		}
	}
}

func TestManagedDownContinuesAfterAnEarlierPhaseFails(t *testing.T) {
	directory := t.TempDir()
	logPath := filepath.Join(directory, "args.log")
	dockerPath := filepath.Join(directory, "docker")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$DOCKER_ARGS_LOG\"\n" +
		"case \"$*\" in *'stop --timeout 30'*) exit 1 ;; esac\n"
	if err := os.WriteFile(dockerPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("COCOLA_DOCKER_BIN", dockerPath)
	t.Setenv("DOCKER_ARGS_LOG", logPath)
	paths := config.Paths{
		Home: directory, Environment: filepath.Join(directory, "config.env"),
		Compose: filepath.Join(directory, "compose.yaml"), State: filepath.Join(directory, "state.json"),
	}
	state := `{"version":"v1","managed_opensandbox":true,"sandbox_image":"registry/runtime:v1"}`
	if err := os.WriteFile(paths.State, []byte(state), 0o600); err != nil {
		t.Fatal(err)
	}
	runner, err := New(paths, nil, &bytes.Buffer{}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.Down(context.Background()); err == nil {
		t.Fatal("expected the first stop failure to be reported")
	}
	contents, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	commands := string(contents)
	if !strings.Contains(commands, "stop --timeout 45 sandbox-manager") ||
		!strings.Contains(commands, "down --remove-orphans") {
		t.Fatalf("teardown did not continue: %s", commands)
	}
}
