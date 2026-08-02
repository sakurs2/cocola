package command

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"

	"github.com/cocola-project/cocola/apps/cli/internal/compose"
	"github.com/cocola-project/cocola/apps/cli/internal/config"
	"github.com/cocola-project/cocola/apps/cli/internal/host"
)

var listenTCP = net.Listen

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
	ports := []struct {
		name    string
		service string
		port    int
	}{
		{name: "Web", service: "web", port: runner.State.WebPort},
		{name: "Gateway", service: "gateway", port: runner.State.GatewayPort},
		{name: "LLM Gateway", service: "llm-gateway", port: runner.State.LLMPort},
	}
	for _, candidate := range ports {
		if err := checkPortAvailable(candidate.port); err != nil {
			running, inspectErr := runner.ServiceRunning(ctx, candidate.service)
			if inspectErr != nil {
				return nil, inspectErr
			}
			if !running {
				return nil, fmt.Errorf("%s port %d is unavailable: %w", candidate.name, candidate.port, err)
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

func checkPortAvailable(port int) error {
	if port < 1 || port > 65535 {
		return errors.New("configuration must specify a port between 1 and 65535")
	}
	listener, err := listenTCP("tcp4", fmt.Sprintf("0.0.0.0:%d", port))
	if err != nil {
		return err
	}
	return listener.Close()
}
