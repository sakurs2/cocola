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

func TestStartUsesManagedProfileAndStartsOpenSandboxFirst(t *testing.T) {
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
	if err := runner.Start(context.Background()); err != nil {
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

func TestStopPreservesComposeTopologyAfterSafeSandboxDrain(t *testing.T) {
	runner, logPath, paths := newRecordingRunner(t, true)
	if err := runner.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	prefix := "compose --project-name cocola --env-file " + paths.Environment +
		" --file " + paths.Compose + " --profile managed "
	assertRecordedCommands(t, logPath, []string{
		prefix + "stop --timeout 30 web gateway agent-runtime",
		prefix + "stop --timeout 45 sandbox-manager",
		prefix + "stop --timeout 30",
	})
}

func TestExternalProviderStopUsesDrainOrderWithoutManagedProfile(t *testing.T) {
	runner, logPath, paths := newRecordingRunner(t, false)
	if err := runner.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	prefix := "compose --project-name cocola --env-file " + paths.Environment +
		" --file " + paths.Compose + " "
	assertRecordedCommands(t, logPath, []string{
		prefix + "stop --timeout 30 web gateway agent-runtime",
		prefix + "stop --timeout 45 sandbox-manager",
		prefix + "stop --timeout 30",
	})
}

func TestParseComposeVersion(t *testing.T) {
	tests := []struct {
		raw  string
		want string
	}{
		{"2.23.1\n", "2.23.1"},
		{"v2.24.7-desktop.1\n", "2.24.7"},
		{"Docker Compose version v2.30.0", "2.30.0"},
	}
	for _, test := range tests {
		version, _, err := parseComposeVersion(test.raw)
		if err != nil {
			t.Fatalf("parseComposeVersion(%q): %v", test.raw, err)
		}
		if version != test.want {
			t.Fatalf("parseComposeVersion(%q) = %q, want %q", test.raw, version, test.want)
		}
	}
	if _, _, err := parseComposeVersion("Docker Compose"); err == nil {
		t.Fatal("expected an unparseable version to fail")
	}
}

func TestComposeVersionRequiresMinimumVersion(t *testing.T) {
	for _, test := range []struct {
		name    string
		version string
		wantErr bool
	}{
		{"minimum", "2.23.1", false},
		{"newer", "2.30.0", false},
		{"older", "2.22.0", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			dockerPath := filepath.Join(directory, "docker")
			script := "#!/bin/sh\nif [ \"$1 $2 $3\" = \"compose version --short\" ]; then printf '%s\\n' \"$COMPOSE_VERSION\"; exit 0; fi\nexit 1\n"
			if err := os.WriteFile(dockerPath, []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("COMPOSE_VERSION", test.version)
			got, err := ComposeVersion(context.Background(), dockerPath)
			if (err != nil) != test.wantErr {
				t.Fatalf("ComposeVersion() = %q, %v", got, err)
			}
			if got != test.version {
				t.Fatalf("ComposeVersion() version = %q, want %q", got, test.version)
			}
		})
	}
}

func TestStopContinuesAfterAnEarlierPhaseFails(t *testing.T) {
	runner, logPath, paths := newRecordingRunner(t, true)
	t.Setenv("FAIL_FIRST_STOP", "1")
	if err := runner.Stop(context.Background()); err == nil {
		t.Fatal("expected the first stop failure to be reported")
	}
	prefix := "compose --project-name cocola --env-file " + paths.Environment +
		" --file " + paths.Compose + " --profile managed "
	assertRecordedCommands(t, logPath, []string{
		prefix + "stop --timeout 30 web gateway agent-runtime",
		prefix + "stop --timeout 45 sandbox-manager",
		prefix + "stop --timeout 30",
	})
}

func newRecordingRunner(t *testing.T, managed bool) (*Runner, string, config.Paths) {
	t.Helper()
	directory := t.TempDir()
	logPath := filepath.Join(directory, "args.log")
	dockerPath := filepath.Join(directory, "docker")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$DOCKER_ARGS_LOG\"\n" +
		"case \"$*\" in *'stop --timeout 30 web gateway agent-runtime'*) [ \"$FAIL_FIRST_STOP\" != 1 ] || exit 1 ;; esac\n"
	if err := os.WriteFile(dockerPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("COCOLA_DOCKER_BIN", dockerPath)
	t.Setenv("DOCKER_ARGS_LOG", logPath)
	paths := config.Paths{
		Home: directory, Environment: filepath.Join(directory, "config.env"),
		Compose: filepath.Join(directory, "compose.yaml"), State: filepath.Join(directory, "state.json"),
	}
	state := `{"version":"v1","managed_opensandbox":false}`
	if managed {
		state = `{"version":"v1","managed_opensandbox":true}`
	}
	if err := os.WriteFile(paths.State, []byte(state), 0o600); err != nil {
		t.Fatal(err)
	}
	runner, err := New(paths, nil, &bytes.Buffer{}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	return runner, logPath, paths
}

func assertRecordedCommands(t *testing.T, logPath string, want []string) {
	t.Helper()
	contents, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Split(strings.TrimSpace(string(contents)), "\n")
	if len(got) != len(want) {
		t.Fatalf("commands = %q, want %q", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("command %d = %q, want %q", index, got[index], want[index])
		}
	}
}
