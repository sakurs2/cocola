package compose

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/cocola-project/cocola/apps/cli/internal/config"
)

// Compose 2.23.1 introduced configs.content, which keeps the generated
// OpenSandbox configuration inside the Compose file.
const MinimumComposeVersion = "2.23.1"

const (
	managedOpenSandboxExecdImage  = "sandbox-registry.cn-zhangjiakou.cr.aliyuncs.com/opensandbox/execd:v1.0.19"
	managedOpenSandboxEgressImage = "opensandbox/egress:v1.1.2"
)

var ErrPostgresCredentialsMismatch = errors.New("the existing PostgreSQL volume does not match the current Cocola configuration")

var composeVersionPattern = regexp.MustCompile(`(?i)(?:^|\s)v?(\d+)\.(\d+)\.(\d+)(?:\D|$)`)

type Runner struct {
	Paths              config.Paths
	State              config.State
	In                 io.Reader
	Out                io.Writer
	Err                io.Writer
	docker             string
	dockerSocketSource string
}

type ServiceStatus struct {
	ID       string `json:"ID"`
	Name     string `json:"Name"`
	Service  string `json:"Service"`
	State    string `json:"State"`
	Health   string `json:"Health"`
	Status   string `json:"Status"`
	ExitCode int    `json:"ExitCode"`
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
	if _, err := DockerSocketSource(ctx, docker); err != nil {
		return err
	}
	return nil
}

// DockerSocketSource resolves the local Unix socket used by the active Docker
// endpoint. Compose bind mounts depend on a local daemon because Cocola session
// storage is shared with sandboxes through host paths.
func DockerSocketSource(ctx context.Context, docker string) (string, error) {
	if source := strings.TrimSpace(os.Getenv("COCOLA_DOCKER_SOCKET_SOURCE")); source != "" {
		return cleanDockerSocketPath(source)
	}

	endpoint := ""
	contextName := strings.TrimSpace(os.Getenv("DOCKER_CONTEXT"))
	if contextName == "" {
		endpoint = strings.TrimSpace(os.Getenv("DOCKER_HOST"))
	}
	if endpoint == "" {
		args := []string{"context", "inspect"}
		if contextName != "" {
			args = append(args, contextName)
		}
		args = append(args, "--format", "{{.Endpoints.docker.Host}}")
		output, err := runCheck(ctx, docker, args...)
		if err != nil {
			detail := firstDiagnostic(output)
			if detail == "" {
				detail = err.Error()
			}
			return "", fmt.Errorf("inspect the active Docker endpoint: %s", detail)
		}
		endpoint = strings.TrimSpace(string(output))
	}
	if endpoint == "" {
		return "", errors.New("the active Docker endpoint is empty")
	}

	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("parse the active Docker endpoint: %w", err)
	}
	if parsed.Scheme != "unix" {
		return "", fmt.Errorf(
			"Cocola Compose deployment requires a local Unix Docker daemon because sandbox storage uses host bind mounts; active Docker endpoint uses %q. Switch to a local Docker context and retry",
			parsed.Scheme,
		)
	}
	return cleanDockerSocketPath(parsed.Path)
}

func cleanDockerSocketPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" || !filepath.IsAbs(path) {
		return "", fmt.Errorf("Docker socket path must be absolute: %q", path)
	}
	return filepath.Clean(path), nil
}

func DockerRootDir(ctx context.Context, docker string) (string, error) {
	output, err := runCheck(ctx, docker, "info", "--format", "{{.DockerRootDir}}")
	if err != nil {
		return "", fmt.Errorf("inspect Docker storage directory: %w", err)
	}
	root := strings.TrimSpace(string(output))
	if root == "" {
		return "", errors.New("Docker did not report its storage directory")
	}
	return root, nil
}

func ComposeVersion(ctx context.Context, docker string) (string, error) {
	checkContext, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	output, err := exec.CommandContext(checkContext, docker, "compose", "version", "--short").CombinedOutput()
	if err != nil {
		detail := firstDiagnostic(output)
		if detail == "" {
			detail = err.Error()
		}
		return "", fmt.Errorf(
			"Docker Compose is unavailable: %s. Install Docker Compose %s or newer and rerun the command",
			detail,
			MinimumComposeVersion,
		)
	}
	version, parts, err := parseComposeVersion(string(output))
	if err != nil {
		return "", err
	}
	minimum, minimumParts, _ := parseComposeVersion(MinimumComposeVersion)
	if compareVersion(parts, minimumParts) < 0 {
		return version, fmt.Errorf(
			"Docker Compose %s is too old; Cocola requires version %s or newer because its deployment uses inline configs. Upgrade Docker Compose and rerun the command",
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
	if err := r.run(ctx, "pull"); err != nil {
		return err
	}
	for _, image := range r.runtimeImages() {
		command := exec.CommandContext(ctx, r.docker, "image", "pull", image)
		command.Stdout = r.Out
		command.Stderr = r.Err
		if err := command.Run(); err != nil {
			return fmt.Errorf("pull runtime image %s: %w", image, err)
		}
	}
	return nil
}

func (r *Runner) RequiredImages(ctx context.Context) ([]string, error) {
	output, err := r.capture(ctx, "config", "--images")
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{})
	images := make([]string, 0)
	for _, image := range append(strings.Fields(string(output)), r.runtimeImages()...) {
		image = strings.TrimSpace(image)
		if image == "" {
			continue
		}
		if _, ok := seen[image]; ok {
			continue
		}
		seen[image] = struct{}{}
		images = append(images, image)
	}
	if len(images) == 0 {
		return nil, errors.New("docker compose did not resolve any deployment images")
	}
	return images, nil
}

// ImagesPresent reports whether every service and managed sandbox runtime
// image is already available locally. It is used only as a fallback after a
// registry pull fails; it never silently ignores a missing runtime sidecar.
func (r *Runner) ImagesPresent(ctx context.Context) (bool, error) {
	images, err := r.RequiredImages(ctx)
	if err != nil {
		return false, err
	}
	command := exec.CommandContext(ctx, r.docker, append([]string{"image", "inspect"}, images...)...)
	command.Stdout = io.Discard
	command.Stderr = r.Err
	if err := command.Run(); err != nil {
		return false, nil
	}
	return true, nil
}

func (r *Runner) MissingImages(ctx context.Context) ([]string, error) {
	images, err := r.RequiredImages(ctx)
	if err != nil {
		return nil, err
	}
	missing := make([]string, 0)
	for _, image := range images {
		command := exec.CommandContext(ctx, r.docker, "image", "inspect", image)
		command.Stdout = io.Discard
		command.Stderr = io.Discard
		if err := command.Run(); err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			missing = append(missing, image)
		}
	}
	return missing, nil
}

func (r *Runner) runtimeImages() []string {
	if !r.State.ManagedOpenSandbox {
		return nil
	}
	return []string{
		r.State.SandboxImage,
		managedOpenSandboxExecdImage,
		managedOpenSandboxEgressImage,
	}
}

func (r *Runner) ServiceRunning(ctx context.Context, service string) (bool, error) {
	ids, err := r.serviceContainerIDs(ctx, service)
	return len(ids) > 0, err
}

func (r *Runner) serviceContainerIDs(ctx context.Context, service string) ([]string, error) {
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
		return nil, fmt.Errorf("inspect running %s container: %w", service, err)
	}
	return strings.Fields(string(output)), nil
}

// ServiceOwnsPublishedPort verifies that an occupied host port belongs to the
// running Cocola service and still maps to the expected container port. This
// prevents a stale container or an unrelated process from bypassing preflight.
func (r *Runner) ServiceOwnsPublishedPort(
	ctx context.Context,
	service string,
	containerPort int,
	hostPort int,
) (bool, error) {
	ids, err := r.serviceContainerIDs(ctx, service)
	if err != nil || len(ids) == 0 {
		return false, err
	}
	portSpec := strconv.Itoa(containerPort) + "/tcp"
	for _, id := range ids {
		command := exec.CommandContext(ctx, r.docker, "port", id, portSpec)
		var diagnostic bytes.Buffer
		command.Stderr = &diagnostic
		output, commandErr := command.Output()
		if commandErr != nil {
			message := strings.TrimSpace(diagnostic.String())
			if strings.Contains(strings.ToLower(message), "no public port") {
				continue
			}
			if message == "" {
				message = commandErr.Error()
			}
			return false, fmt.Errorf("inspect published port for %s container: %s", service, message)
		}
		for _, binding := range strings.Fields(string(output)) {
			_, rawPort, splitErr := net.SplitHostPort(binding)
			if splitErr != nil {
				continue
			}
			publishedPort, parseErr := strconv.Atoi(rawPort)
			if parseErr == nil && publishedPort == hostPort {
				return true, nil
			}
		}
	}
	return false, nil
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

func (r *Runner) StopService(ctx context.Context, service string) error {
	return r.run(ctx, "stop", "--timeout", "30", service)
}

// PrepareExistingPostgres starts an already-created PostgreSQL volume without
// waiting on its healthcheck, then verifies that the current deployment secret
// can authenticate. This distinguishes a resumable partial first start from a
// stale volume created by a different Cocola configuration.
func (r *Runner) PrepareExistingPostgres(ctx context.Context) error {
	if err := r.run(ctx, "up", "-d", "postgres"); err != nil {
		return fmt.Errorf("start existing PostgreSQL service: %w", err)
	}
	checkContext, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	var lastErr error
	for {
		err := r.CheckPostgresCredentials(checkContext)
		if err == nil {
			return nil
		}
		if errors.Is(err, ErrPostgresCredentialsMismatch) {
			return err
		}
		lastErr = err
		select {
		case <-checkContext.Done():
			return fmt.Errorf("verify existing PostgreSQL credentials: %w", errors.Join(lastErr, checkContext.Err()))
		case <-ticker.C:
		}
	}
}

func (r *Runner) CheckPostgresCredentials(ctx context.Context) error {
	command, err := r.command(
		ctx,
		"exec", "-T", "postgres", "sh", "-ec",
		`PGPASSWORD="$POSTGRES_PASSWORD" psql -h 127.0.0.1 -U "$POSTGRES_USER" -d "$POSTGRES_DB" -tAc 'SELECT 1' >/dev/null`,
	)
	if err != nil {
		return err
	}
	var diagnostic bytes.Buffer
	command.Stdout = io.Discard
	command.Stderr = &diagnostic
	if err := command.Run(); err != nil {
		detail := strings.TrimSpace(diagnostic.String())
		if strings.Contains(strings.ToLower(detail), "password authentication failed") {
			return fmt.Errorf("%w: %s", ErrPostgresCredentialsMismatch, detail)
		}
		if detail == "" {
			detail = err.Error()
		}
		return fmt.Errorf("authenticate to PostgreSQL: %s", detail)
	}
	return nil
}

// BackupDatabase writes a compressed pg_dump from the existing PostgreSQL
// service. The destination is installed atomically with owner-only access.
func (r *Runner) BackupDatabase(ctx context.Context, destination string) error {
	return r.backupStream(ctx, destination, "PostgreSQL", []string{
		"exec", "-T", "postgres", "pg_dump", "-U", "cocola", "-d", "cocola", "--format=custom",
	})
}

func (r *Runner) BackupForgejoDatabase(ctx context.Context, destination string) error {
	return r.backupStream(ctx, destination, "Forgejo PostgreSQL", []string{
		"exec", "-T", "postgres", "pg_dump", "-U", "cocola", "-d", "forgejo", "--format=custom",
	})
}

func (r *Runner) BackupForgejoData(ctx context.Context, destination string) error {
	return r.backupStream(ctx, destination, "Forgejo data", []string{
		"run", "--rm", "--no-deps", "--entrypoint", "tar", "forgejo", "-C", "/data", "-czf", "-", ".",
	})
}

func (r *Runner) backupStream(ctx context.Context, destination, label string, args []string) error {
	temporary, err := os.CreateTemp(filepath.Dir(destination), ".cocola-backup-*")
	if err != nil {
		return fmt.Errorf("create %s backup: %w", label, err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return fmt.Errorf("secure %s backup: %w", label, err)
	}
	command, err := r.command(ctx, args...)
	if err != nil {
		temporary.Close()
		return err
	}
	command.Stdout = temporary
	command.Stderr = r.Err
	if err := command.Run(); err != nil {
		temporary.Close()
		return fmt.Errorf("back up %s: %w", label, err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync %s backup: %w", label, err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close %s backup: %w", label, err)
	}
	if err := os.Rename(temporaryPath, destination); err != nil {
		return fmt.Errorf("install %s backup: %w", label, err)
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

func (r *Runner) ServiceStatuses(ctx context.Context) ([]ServiceStatus, error) {
	output, err := r.capture(ctx, "ps", "--all", "--format", "json")
	if err != nil {
		return nil, err
	}
	trimmed := bytes.TrimSpace(output)
	if len(trimmed) == 0 {
		return nil, nil
	}
	var statuses []ServiceStatus
	if trimmed[0] == '[' {
		if err := json.Unmarshal(trimmed, &statuses); err != nil {
			return nil, fmt.Errorf("decode Docker Compose service status: %w", err)
		}
		return statuses, nil
	}
	for _, line := range bytes.Split(trimmed, []byte{'\n'}) {
		var status ServiceStatus
		if err := json.Unmarshal(line, &status); err != nil {
			return nil, fmt.Errorf("decode Docker Compose service status: %w", err)
		}
		statuses = append(statuses, status)
	}
	return statuses, nil
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
	command, err := r.command(ctx, args...)
	if err != nil {
		return err
	}
	command.Stdout = r.Out
	command.Stderr = r.Err
	if err = command.Run(); err != nil {
		return fmt.Errorf("docker compose %s: %w", args[0], err)
	}
	return nil
}

func (r *Runner) capture(ctx context.Context, args ...string) ([]byte, error) {
	command, err := r.command(ctx, args...)
	if err != nil {
		return nil, err
	}
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = r.Err
	if err = command.Run(); err != nil {
		return nil, fmt.Errorf("docker compose %s: %w", args[0], err)
	}
	return output.Bytes(), nil
}

func (r *Runner) command(ctx context.Context, args ...string) (*exec.Cmd, error) {
	socketSource, err := r.resolveDockerSocketSource(ctx)
	if err != nil {
		return nil, err
	}
	base := []string{
		"compose", "--project-name", "cocola", "--env-file", r.Paths.Environment,
		"--file", r.Paths.Compose,
	}
	if r.State.ManagedOpenSandbox {
		base = append(base, "--profile", "managed")
	}
	command := exec.CommandContext(ctx, r.docker, append(base, args...)...)
	command.Stdin = r.In
	command.Env = environmentWith("COCOLA_DOCKER_SOCKET_SOURCE", socketSource)
	return command, nil
}

func environmentWith(key, value string) []string {
	prefix := key + "="
	environment := make([]string, 0, len(os.Environ())+1)
	for _, item := range os.Environ() {
		if !strings.HasPrefix(item, prefix) {
			environment = append(environment, item)
		}
	}
	return append(environment, prefix+value)
}

func (r *Runner) resolveDockerSocketSource(ctx context.Context) (string, error) {
	if r.dockerSocketSource != "" {
		return r.dockerSocketSource, nil
	}
	source, err := DockerSocketSource(ctx, r.docker)
	if err != nil {
		return "", err
	}
	r.dockerSocketSource = source
	return source, nil
}
