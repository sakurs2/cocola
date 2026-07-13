package compose

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/cocola-project/cocola/apps/cli/internal/config"
)

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
	if err := runCheck(ctx, docker, "info"); err != nil {
		return errors.New("docker daemon is unavailable")
	}
	if err := runCheck(ctx, docker, "compose", "version"); err != nil {
		return errors.New("Docker Compose v2 is unavailable")
	}
	return nil
}

func runCheck(ctx context.Context, command string, args ...string) error {
	checkContext, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return exec.CommandContext(checkContext, command, args...).Run()
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

func (r *Runner) Up(ctx context.Context) error {
	if r.State.ManagedOpenSandbox {
		if err := r.run(ctx, "up", "-d", "--wait", "opensandbox-server"); err != nil {
			return err
		}
	}
	return r.run(ctx, "up", "-d", "--remove-orphans", "--wait")
}

func (r *Runner) Down(ctx context.Context) error {
	if !r.State.ManagedOpenSandbox {
		return r.run(ctx, "down", "--remove-orphans")
	}

	// Keep the dedicated OpenSandbox server alive until app workers have stopped:
	// sandbox-manager uses it during SIGTERM to checkpoint active sessions and
	// drain the warm pool. Teardown is best-effort but exhaustive, so one failed
	// phase cannot leave the rest of the stack in an avoidable intermediate state.
	var failures []error
	if err := r.run(ctx, "stop", "--timeout", "30", "web", "gateway", "agent-runtime"); err != nil {
		failures = append(failures, err)
	}
	if err := r.run(ctx, "stop", "--timeout", "45", "sandbox-manager"); err != nil {
		failures = append(failures, err)
	}
	if err := r.cleanupSandboxes(ctx); err != nil {
		failures = append(failures, err)
	}
	if err := r.run(ctx, "down", "--remove-orphans"); err != nil {
		failures = append(failures, err)
	}
	return errors.Join(failures...)
}

func (r *Runner) cleanupSandboxes(ctx context.Context) error {
	image := strings.TrimSpace(r.State.SandboxImage)
	if image == "" {
		return errors.New("sandbox image is missing from installation state")
	}
	var output bytes.Buffer
	list := exec.CommandContext(ctx, r.docker, "ps", "-aq", "--filter", "ancestor="+image)
	list.Stdout = &output
	list.Stderr = r.Err
	if err := list.Run(); err != nil {
		return fmt.Errorf("list managed sandbox containers: %w", err)
	}
	ids := strings.Fields(output.String())
	for len(ids) > 0 {
		count := min(len(ids), 100)
		args := append([]string{"rm", "-f"}, ids[:count]...)
		remove := exec.CommandContext(ctx, r.docker, args...)
		remove.Stdout = io.Discard
		remove.Stderr = r.Err
		if err := remove.Run(); err != nil {
			return fmt.Errorf("remove managed sandbox containers: %w", err)
		}
		ids = ids[count:]
	}
	return nil
}

func (r *Runner) Restart(ctx context.Context) error {
	return r.run(ctx, "restart")
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
	base := []string{
		"compose", "--project-name", "cocola", "--env-file", r.Paths.Environment,
		"--file", r.Paths.Compose,
	}
	if r.State.ManagedOpenSandbox {
		base = append(base, "--profile", "managed")
	}
	command := exec.CommandContext(ctx, r.docker, append(base, args...)...)
	command.Stdin = r.In
	command.Stdout = r.Out
	command.Stderr = r.Err
	if err := command.Run(); err != nil {
		return fmt.Errorf("docker compose %s: %w", args[0], err)
	}
	return nil
}
