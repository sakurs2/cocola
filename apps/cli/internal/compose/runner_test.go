package compose

import (
	"bytes"
	"context"
	"errors"
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
	t.Setenv("COCOLA_DOCKER_SOCKET_SOURCE", "/var/run/docker.sock")
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
		{"older", "2.23.0", true},
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
			if test.wantErr && (!strings.Contains(err.Error(), "too old") ||
				!strings.Contains(err.Error(), "Upgrade Docker Compose")) {
				t.Fatalf("ComposeVersion() error is not actionable: %v", err)
			}
		})
	}
}

func TestComposeVersionReportsUnavailableDiagnostic(t *testing.T) {
	directory := t.TempDir()
	dockerPath := filepath.Join(directory, "docker")
	script := "#!/bin/sh\nprintf '%s\\n' \"docker: 'compose' is not a docker command.\" >&2\nexit 1\n"
	if err := os.WriteFile(dockerPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := ComposeVersion(context.Background(), dockerPath)
	if err == nil || !strings.Contains(err.Error(), "docker: 'compose' is not a docker command") ||
		!strings.Contains(err.Error(), "Install Docker Compose 2.23.1 or newer") {
		t.Fatalf("ComposeVersion() error = %v", err)
	}
}

func TestImagesPresentUsesResolvedComposeImages(t *testing.T) {
	directory := t.TempDir()
	logPath := filepath.Join(directory, "args.log")
	dockerPath := filepath.Join(directory, "docker")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$DOCKER_ARGS_LOG"
case "$*" in
  *'config --images'*) printf '%s\n' 'redis:7.4.10-alpine3.21' 'ghcr.io/sakurs2/cocola-web:v0.2.0' ;;
  'image inspect '*) [ "$IMAGES_MISSING" != 1 ] ;;
esac
`
	if err := os.WriteFile(dockerPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("COCOLA_DOCKER_BIN", dockerPath)
	t.Setenv("DOCKER_ARGS_LOG", logPath)
	paths := writeRunnerState(t, directory, true)
	runner, err := New(paths, nil, &bytes.Buffer{}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	available, err := runner.ImagesPresent(context.Background())
	if err != nil || !available {
		t.Fatalf("ImagesPresent() = %v, %v", available, err)
	}
	t.Setenv("IMAGES_MISSING", "1")
	available, err = runner.ImagesPresent(context.Background())
	if err != nil || available {
		t.Fatalf("ImagesPresent() with a cache miss = %v, %v", available, err)
	}
}

func TestPullIncludesManagedSandboxRuntimeImages(t *testing.T) {
	runner, logPath, paths := newRecordingRunner(t, true)
	if err := runner.Pull(context.Background()); err != nil {
		t.Fatal(err)
	}
	prefix := "compose --project-name cocola --env-file " + paths.Environment +
		" --file " + paths.Compose + " --profile managed "
	assertRecordedCommands(t, logPath, []string{
		prefix + "pull",
		"image pull ghcr.io/sakurs2/cocola-sandbox-runtime:v1",
		"image pull " + managedOpenSandboxExecdImage,
		"image pull " + managedOpenSandboxEgressImage,
	})
}

func TestExternalOpenSandboxDoesNotPullRuntimeImagesLocally(t *testing.T) {
	runner, logPath, paths := newRecordingRunner(t, false)
	if err := runner.Pull(context.Background()); err != nil {
		t.Fatal(err)
	}
	prefix := "compose --project-name cocola --env-file " + paths.Environment +
		" --file " + paths.Compose + " "
	assertRecordedCommands(t, logPath, []string{prefix + "pull"})
}

func TestPrepareExistingPostgresRejectsCredentialMismatch(t *testing.T) {
	directory := t.TempDir()
	dockerPath := filepath.Join(directory, "docker")
	script := `#!/bin/sh
case "$*" in
  *'up -d postgres') exit 0 ;;
  *'exec -T postgres sh -ec'*) printf '%s\n' 'psql: error: password authentication failed for user "cocola"' >&2; exit 2 ;;
esac
exit 1
`
	if err := os.WriteFile(dockerPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("COCOLA_DOCKER_BIN", dockerPath)
	paths := writeRunnerState(t, directory, false)
	runner, err := New(paths, nil, &bytes.Buffer{}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	err = runner.PrepareExistingPostgres(context.Background())
	if !errors.Is(err, ErrPostgresCredentialsMismatch) {
		t.Fatalf("PrepareExistingPostgres() error = %v", err)
	}
}

func TestServiceStatusesAcceptsComposeJSONArray(t *testing.T) {
	directory := t.TempDir()
	dockerPath := filepath.Join(directory, "docker")
	script := `#!/bin/sh
case "$*" in
  *'ps --all --format json'*) printf '%s\n' '[{"Service":"postgres","State":"running","Health":"healthy","Status":"Up"}]'; exit 0 ;;
esac
exit 1
`
	if err := os.WriteFile(dockerPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("COCOLA_DOCKER_BIN", dockerPath)
	paths := writeRunnerState(t, directory, false)
	runner, err := New(paths, nil, &bytes.Buffer{}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	statuses, err := runner.ServiceStatuses(context.Background())
	if err != nil || len(statuses) != 1 || statuses[0].Service != "postgres" || statuses[0].Health != "healthy" {
		t.Fatalf("ServiceStatuses() = %+v, %v", statuses, err)
	}
}

func TestBackupDatabaseIsAtomicAndPrivate(t *testing.T) {
	directory := t.TempDir()
	dockerPath := filepath.Join(directory, "docker")
	script := `#!/bin/sh
case "$*" in
  *'exec -T postgres pg_dump -U cocola -d cocola --format=custom'*) printf 'postgres-dump' ;;
  *) exit 1 ;;
esac
`
	if err := os.WriteFile(dockerPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("COCOLA_DOCKER_BIN", dockerPath)
	paths := writeRunnerState(t, directory, false)
	runner, err := New(paths, nil, &bytes.Buffer{}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(directory, "postgres.dump")
	if err := runner.BackupDatabase(context.Background(), destination); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "postgres-dump" {
		t.Fatalf("backup = %q", contents)
	}
	info, err := os.Stat(destination)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("backup mode = %o", info.Mode().Perm())
	}
}

func TestVolumePresentFindsDataWithoutAServiceContainer(t *testing.T) {
	directory := t.TempDir()
	dockerPath := filepath.Join(directory, "docker")
	script := "#!/bin/sh\n[ \"$1 $2\" = \"volume inspect\" ] || exit 1\nif [ \"$3\" = \"cocola_pgdata\" ]; then exit 0; fi\nprintf '%s\\n' 'Error: No such volume' >&2\nexit 1\n"
	if err := os.WriteFile(dockerPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("COCOLA_DOCKER_BIN", dockerPath)
	paths := writeRunnerState(t, directory, false)
	runner, err := New(paths, nil, &bytes.Buffer{}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	present, err := runner.VolumePresent(context.Background(), "pgdata")
	if err != nil || !present {
		t.Fatalf("VolumePresent(pgdata) = %v, %v", present, err)
	}
	present, err = runner.VolumePresent(context.Background(), "missing")
	if err != nil || present {
		t.Fatalf("VolumePresent(missing) = %v, %v", present, err)
	}
}

func TestCheckDockerIncludesDaemonDiagnostic(t *testing.T) {
	directory := t.TempDir()
	dockerPath := filepath.Join(directory, "docker")
	script := "#!/bin/sh\nif [ \"$1\" = info ]; then printf '%s\\n' 'permission denied while connecting to Docker' >&2; exit 1; fi\n"
	if err := os.WriteFile(dockerPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("COCOLA_DOCKER_BIN", dockerPath)
	err := CheckDocker(context.Background())
	if err == nil || !strings.Contains(err.Error(), "permission denied while connecting to Docker") {
		t.Fatalf("CheckDocker() error = %v", err)
	}
}

func TestDockerSocketSourceUsesDockerHost(t *testing.T) {
	t.Setenv("COCOLA_DOCKER_SOCKET_SOURCE", "")
	t.Setenv("DOCKER_CONTEXT", "")
	t.Setenv("DOCKER_HOST", "unix:///run/user/1000/docker.sock")

	got, err := DockerSocketSource(context.Background(), "docker")
	if err != nil {
		t.Fatal(err)
	}
	if got != "/run/user/1000/docker.sock" {
		t.Fatalf("DockerSocketSource() = %q", got)
	}
}

func TestDockerSocketSourcePrefersDockerContext(t *testing.T) {
	directory := t.TempDir()
	dockerPath := filepath.Join(directory, "docker")
	script := `#!/bin/sh
if [ "$1 $2 $3" = "context inspect rootless" ]; then
  printf '%s\n' 'unix:///run/user/1000/docker.sock'
  exit 0
fi
exit 1
`
	if err := os.WriteFile(dockerPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("COCOLA_DOCKER_SOCKET_SOURCE", "")
	t.Setenv("DOCKER_CONTEXT", "rootless")
	t.Setenv("DOCKER_HOST", "tcp://remote.example.com:2376")

	got, err := DockerSocketSource(context.Background(), dockerPath)
	if err != nil {
		t.Fatal(err)
	}
	if got != "/run/user/1000/docker.sock" {
		t.Fatalf("DockerSocketSource() = %q", got)
	}
}

func TestDockerSocketSourceRejectsRemoteDocker(t *testing.T) {
	t.Setenv("COCOLA_DOCKER_SOCKET_SOURCE", "")
	t.Setenv("DOCKER_CONTEXT", "")
	t.Setenv("DOCKER_HOST", "ssh://docker.example.com")

	_, err := DockerSocketSource(context.Background(), "docker")
	if err == nil || !strings.Contains(err.Error(), "requires a local Unix Docker daemon") ||
		!strings.Contains(err.Error(), "Switch to a local Docker context") {
		t.Fatalf("DockerSocketSource() error = %v", err)
	}
}

func TestComposeCommandInjectsResolvedDockerSocket(t *testing.T) {
	directory := t.TempDir()
	dockerPath := filepath.Join(directory, "docker")
	if err := os.WriteFile(dockerPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("COCOLA_DOCKER_BIN", dockerPath)
	paths := writeRunnerState(t, directory, false)
	t.Setenv("COCOLA_DOCKER_SOCKET_SOURCE", "/run/user/1000/docker.sock")
	runner, err := New(paths, nil, &bytes.Buffer{}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	command, err := runner.command(context.Background(), "config", "--quiet")
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range command.Env {
		if item == "COCOLA_DOCKER_SOCKET_SOURCE=/run/user/1000/docker.sock" {
			return
		}
	}
	t.Fatalf("resolved Docker socket missing from command environment: %q", command.Env)
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
	paths := writeRunnerState(t, directory, managed)
	runner, err := New(paths, nil, &bytes.Buffer{}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	return runner, logPath, paths
}

func writeRunnerState(t *testing.T, directory string, managed bool) config.Paths {
	t.Helper()
	t.Setenv("COCOLA_DOCKER_SOCKET_SOURCE", "/var/run/docker.sock")
	paths := config.Paths{
		Home: directory, Environment: filepath.Join(directory, "config.env"),
		Compose: filepath.Join(directory, "compose.yaml"), State: filepath.Join(directory, "state.json"),
	}
	state := `{"version":"v1","managed_opensandbox":false,"sandbox_image":"ghcr.io/sakurs2/cocola-sandbox-runtime:v1"}`
	if managed {
		state = `{"version":"v1","managed_opensandbox":true,"sandbox_image":"ghcr.io/sakurs2/cocola-sandbox-runtime:v1"}`
	}
	if err := os.WriteFile(paths.State, []byte(state), 0o600); err != nil {
		t.Fatal(err)
	}
	return paths
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
