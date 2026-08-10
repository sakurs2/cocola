package command

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"

	"github.com/cocola-project/cocola/apps/cli/internal/compose"
	"github.com/cocola-project/cocola/apps/cli/internal/config"
	"github.com/cocola-project/cocola/apps/cli/internal/host"
)

var listenTCP = net.Listen

type startPortBinding struct {
	name          string
	service       string
	bindHost      string
	containerPort int
	port          int
}

func runStartPreflight(ctx context.Context, runner *compose.Runner) ([]string, error) {
	if runner.State.ConfigSchemaVersion != config.CurrentSchemaVersion {
		return nil, errors.New("deployment configuration is outdated; run cocola install to migrate it before starting Cocola")
	}
	if err := prepareSandboxRoot(runner.Paths.SandboxRoot); err != nil {
		return nil, err
	}
	if err := runner.Validate(ctx); err != nil {
		return nil, fmt.Errorf("validate deployment configuration: %w", err)
	}
	for _, candidate := range startPortBindings(runner.State) {
		if err := checkPortAvailable(candidate.bindHost, candidate.port); err != nil {
			owned, inspectErr := runner.ServiceOwnsPublishedPort(
				ctx, candidate.service, candidate.containerPort, candidate.port,
			)
			if inspectErr != nil {
				return nil, inspectErr
			}
			if !owned {
				return nil, fmt.Errorf(
					"%s port %d is unavailable and is not owned by the configured Cocola service: %w",
					candidate.name, candidate.port, err,
				)
			}
		}
	}
	available, err := host.AvailableDiskBytes(runner.Paths.Home)
	if err != nil {
		return []string{"Available disk space could not be determined: " + err.Error()}, nil
	}
	if available < host.MinimumFreeDiskBytes {
		return nil, fmt.Errorf(
			"only %.1f GiB of disk space is available; free at least 2 GiB before starting Cocola",
			float64(available)/(1024*1024*1024),
		)
	}
	if available < host.WarnFreeDiskBytes {
		return []string{fmt.Sprintf(
			"Only %.1f GiB of disk space is available. Sandbox images and workspaces may require more space.",
			float64(available)/(1024*1024*1024),
		)}, nil
	}
	return nil, nil
}

func startPortBindings(state config.State) []startPortBinding {
	return []startPortBinding{
		{name: "Web", service: "web", bindHost: "0.0.0.0", containerPort: 3000, port: state.WebPort},
		{name: "Gateway", service: "gateway", bindHost: "0.0.0.0", containerPort: 8080, port: state.GatewayPort},
		{name: "LLM Gateway", service: "llm-gateway", bindHost: "0.0.0.0", containerPort: 8080, port: state.LLMPort},
		{name: "Internal SCM", service: "forgejo", bindHost: "127.0.0.1", containerPort: 3000, port: state.InternalSCM.HostPort},
	}
}

func prepareSandboxRoot(path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("sandbox storage path must be absolute: %q", path)
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return fmt.Errorf("create sandbox storage directory: %w", err)
	}
	probe, err := os.CreateTemp(path, ".cocola-write-check-*")
	if err != nil {
		return fmt.Errorf("sandbox storage directory is not writable: %w", err)
	}
	probePath := probe.Name()
	if err := probe.Close(); err != nil {
		_ = os.Remove(probePath)
		return fmt.Errorf("verify sandbox storage directory: %w", err)
	}
	if err := os.Remove(probePath); err != nil {
		return fmt.Errorf("clean up sandbox storage write check: %w", err)
	}
	return nil
}

func checkPortAvailable(host string, port int) error {
	if port < 1 || port > 65535 {
		return errors.New("configuration must specify a port between 1 and 65535")
	}
	listener, err := listenTCP("tcp4", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return err
	}
	return listener.Close()
}
