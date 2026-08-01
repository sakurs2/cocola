package compose

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/cocola-project/cocola/apps/cli/internal/config"
)

// Compose 2.1.1 introduced `docker compose up --wait`, the newest Compose
// capability used by the generated deployment. Keep this floor tied to an
// actual runtime requirement rather than newer convenience syntax.
const MinimumComposeVersion = "2.1.1"

var composeVersionPattern = regexp.MustCompile(`(?i)(?:^|\s)v?(\d+)\.(\d+)\.(\d+)(?:\D|$)`)

type Runner struct {
	Paths  config.Paths
	State  config.State
	In     io.Reader
	Out    io.Writer
	Err    io.Writer
	docker string
}

func CheckDocker(ctx context.Context) error {
	docker, err := DockerBinary()
	if err != nil {
		return err
	}
	if output, err := runCheck(ctx, docker, "info"); err != nil {
		detail := firstDiagnostic(output)
		if detail == "" {
			detail = err.Error()
		}
		return fmt.Errorf("docker daemon is unavailable: %s", detail)
	}
	if _, err := ComposeVersion(ctx, docker); err != nil {
		return err
	}
	return nil
}

func ComposeVersion(ctx context.Context, docker string) (string, error) {
	checkContext, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	output, err := exec.CommandContext(checkContext, docker, "compose", "version", "--short").Output()
	if err != nil {
		return "", errors.New("Docker Compose v2 is unavailable")
	}
	version, parts, err := parseComposeVersion(string(output))
	if err != nil {
		return "", err
	}
	minimum, minimumParts, _ := parseComposeVersion(MinimumComposeVersion)
	if compareVersion(parts, minimumParts) < 0 {
		return version, fmt.Errorf(
			"Docker Compose %s is unsupported; version %s or newer is required",
			version,
			minimum,
		)
	}
	return version, nil
}

func parseComposeVersion(raw string) (string, [3]int, error) {
	match := composeVersionPattern.FindStringSubmatch(raw)
	if len(match) != 4 {
		return "", [3]int{}, fmt.Errorf("cannot parse Docker Compose version from %q", raw)
	}
	var parts [3]int
	for index := range parts {
		value, err := strconv.Atoi(match[index+1])
		if err != nil {
			return "", [3]int{}, fmt.Errorf("parse Docker Compose version: %w", err)
		}
		parts[index] = value
	}
	return fmt.Sprintf("%d.%d.%d", parts[0], parts[1], parts[2]), parts, nil
}

func compareVersion(left, right [3]int) int {
	for index := range left {
		if left[index] < right[index] {
			return -1
		}
		if left[index] > right[index] {
			return 1
		}
	}
	return 0
}

func runCheck(ctx context.Context, command string, args ...string) ([]byte, error) {
	checkContext, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return exec.CommandContext(checkContext, command, args...).CombinedOutput()
}

func firstDiagnostic(output []byte) string {
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}

func DockerBinary() (string, error) {
	if configured := os.Getenv("COCOLA_DOCKER_BIN"); configured != "" {
		if info, err := os.Stat(configured); err == nil && executable(info) {
			return configured, nil
		}
		return "", fmt.Errorf("COCOLA_DOCKER_BIN does not point to an executable file: %s", configured)
	}
	if path, err := exec.LookPath("docker"); err == nil {
		return path, nil
	}
	for _, candidate := range []string{
		"/usr/local/bin/docker",
		"/opt/homebrew/bin/docker",
		"/Applications/OrbStack.app/Contents/MacOS/xbin/docker",
	} {
		if info, err := os.Stat(candidate); err == nil && executable(info) {
			return candidate, nil
		}
	}
	return "", errors.New("docker is not installed or not in PATH")
}

func executable(info os.FileInfo) bool {
	return !info.IsDir() && info.Mode().Perm()&0o111 != 0
}

func New(paths config.Paths, input io.Reader, output, errors io.Writer) (*Runner, error) {
	state, err := config.Load(paths)
	if err != nil {
		return nil, err
	}
	docker, err := DockerBinary()
	if err != nil {
		return nil, err
	}
	return &Runner{Paths: paths, State: state, In: input, Out: output, Err: errors, docker: docker}, nil
}

func (r *Runner) Pull(ctx context.Context) error {
	return r.run(ctx, "pull")
}

// ImagesPresent reports whether every image selected by the generated Compose
// topology is already available locally. It is used only as a fallback after a
// registry pull fails; it never treats an unreachable daemon as a cache miss.
func (r *Runner) ImagesPresent(ctx context.Context) (bool, error) {
	output, err := r.capture(ctx, "config", "--images")
	if err != nil {
		return false, err
	}
	seen := make(map[string]struct{})
	images := make([]string, 0)
	for _, image := range strings.Fields(string(output)) {
		if _, ok := seen[image]; ok {
			continue
		}
		seen[image] = struct{}{}
		images = append(images, image)
	}
	if len(images) == 0 {
		return false, errors.New("docker compose did not resolve any deployment images")
	}
	command := exec.CommandContext(ctx, r.docker, append([]string{"image", "inspect"}, images...)...)
	command.Stdout = io.Discard
	command.Stderr = r.Err
	if err := command.Run(); err != nil {
		return false, nil
	}
	return true, nil
}

func (r *Runner) ServiceRunning(ctx context.Context, service string) (bool, error) {
	command := exec.CommandContext(
		ctx,
		r.docker,
		"ps", "-q",
		"--filter", "label=com.docker.compose.project=cocola",
		"--filter", "label=com.docker.compose.service="+service,
	)
	command.Stderr = r.Err
	output, err := command.Output()
	if err != nil {
		return false, fmt.Errorf("inspect running %s container: %w", service, err)
	}
	return strings.TrimSpace(string(output)) != "", nil
}

func (r *Runner) VolumePresent(ctx context.Context, name string) (bool, error) {
	command := exec.CommandContext(ctx, r.docker, "volume", "inspect", "cocola_"+name)
	command.Stdout = io.Discard
	var diagnostic bytes.Buffer
	command.Stderr = &diagnostic
	if err := command.Run(); err != nil {
		if strings.Contains(strings.ToLower(diagnostic.String()), "no such volume") {
			return false, nil
		}
		return false, fmt.Errorf("inspect Cocola volume %s: %w: %s", name, err, strings.TrimSpace(diagnostic.String()))
	}
	return true, nil
}

func (r *Runner) StartService(ctx context.Context, service string) error {
	return r.run(ctx, "up", "-d", "--wait", service)
}

// BackupDatabase writes a compressed pg_dump from the existing PostgreSQL
// service. The destination is installed atomically with owner-only access.
func (r *Runner) BackupDatabase(ctx context.Context, destination string) error {
	temporary, err := os.CreateTemp(filepath.Dir(destination), ".postgres-dump-*")
	if err != nil {
		return fmt.Errorf("create PostgreSQL backup: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return fmt.Errorf("secure PostgreSQL backup: %w", err)
	}
	command := r.command(ctx, "exec", "-T", "postgres", "pg_dump", "-U", "cocola", "-d", "cocola", "--format=custom")
	command.Stdout = temporary
	command.Stderr = r.Err
	if err := command.Run(); err != nil {
		temporary.Close()
		return fmt.Errorf("back up PostgreSQL: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync PostgreSQL backup: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close PostgreSQL backup: %w", err)
	}
	if err := os.Rename(temporaryPath, destination); err != nil {
		return fmt.Errorf("install PostgreSQL backup: %w", err)
	}
	return nil
}

// Start is the single create/update/resume path. Compose reuses unchanged
// containers, recreates services whose image or configuration changed, and
// creates anything missing after the first installation.
func (r *Runner) Start(ctx context.Context) error {
	if r.State.ManagedOpenSandbox {
		if err := r.run(ctx, "up", "-d", "--wait", "opensandbox-server"); err != nil {
			return err
		}
	}
	return r.run(ctx, "up", "-d", "--remove-orphans", "--wait")
}

// stopForDrain stops request/execution entrypoints before sandbox-manager so
// its SIGTERM handler can release registered compute while the OpenSandbox
// provider is still reachable. Both managed and external providers use the
// same ordering; only the managed profile changes the Compose service set.
func (r *Runner) stopForDrain(ctx context.Context) []error {
	// Keep the Sandbox Provider reachable until app workers have stopped:
	// sandbox-manager still uses it to destroy active compute sandboxes during
	// SIGTERM. In managed mode the provider server is part of this Compose
	// project; in external mode it remains outside the project. Teardown is
	// best-effort but exhaustive, so one failed phase cannot leave the rest of
	// the stack in an avoidable intermediate state.
	var failures []error
	if err := r.run(ctx, "stop", "--timeout", "30", "web", "gateway", "agent-runtime"); err != nil {
		failures = append(failures, err)
	}
	if err := r.run(ctx, "stop", "--timeout", "45", "sandbox-manager"); err != nil {
		failures = append(failures, err)
	}
	return failures
}

// Stop pauses the installed Compose topology. Service containers, the project
// network, images and data remain; registered sandbox compute is drained by
// sandbox-manager before the provider is stopped.
func (r *Runner) Stop(ctx context.Context) error {
	failures := r.stopForDrain(ctx)
	if err := r.run(ctx, "stop", "--timeout", "30"); err != nil {
		failures = append(failures, err)
	}
	return errors.Join(failures...)
}

func (r *Runner) Status(ctx context.Context, jsonOutput bool) error {
	args := []string{"ps"}
	if jsonOutput {
		args = append(args, "--format", "json")
	}
	return r.run(ctx, args...)
}

func (r *Runner) Logs(ctx context.Context, service string, follow bool, tail int) error {
	args := []string{"logs"}
	if follow {
		args = append(args, "--follow")
	}
	if tail > 0 {
		args = append(args, "--tail", fmt.Sprintf("%d", tail))
	}
	if service != "" {
		args = append(args, service)
	}
	return r.run(ctx, args...)
}

func (r *Runner) Validate(ctx context.Context) error {
	return r.run(ctx, "config", "--quiet")
}

func (r *Runner) run(ctx context.Context, args ...string) error {
	command := r.command(ctx, args...)
	command.Stdout = r.Out
	command.Stderr = r.Err
	if err := command.Run(); err != nil {
		return fmt.Errorf("docker compose %s: %w", args[0], err)
	}
	return nil
}

func (r *Runner) capture(ctx context.Context, args ...string) ([]byte, error) {
	command := r.command(ctx, args...)
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = r.Err
	if err := command.Run(); err != nil {
		return nil, fmt.Errorf("docker compose %s: %w", args[0], err)
	}
	return output.Bytes(), nil
}

func (r *Runner) command(ctx context.Context, args ...string) *exec.Cmd {
	base := []string{
		"compose", "--project-name", "cocola", "--env-file", r.Paths.Environment,
		"--file", r.Paths.Compose,
	}
	if r.State.ManagedOpenSandbox {
		base = append(base, "--profile", "managed")
	}
	command := exec.CommandContext(ctx, r.docker, append(base, args...)...)
	command.Stdin = r.In
	return command
}
