package doctor

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/cocola-project/cocola/apps/cli/internal/compose"
	"github.com/cocola-project/cocola/apps/cli/internal/config"
	"github.com/cocola-project/cocola/apps/cli/internal/host"
)

type Status string

const (
	StatusPass    Status = "pass"
	StatusWarning Status = "warning"
	StatusFail    Status = "fail"
)

type Check struct {
	Name    string `json:"name"`
	OK      bool   `json:"ok"`
	Status  Status `json:"status"`
	Message string `json:"message"`
}

type Report struct {
	OK     bool    `json:"ok"`
	Checks []Check `json:"checks"`
}

func Run(ctx context.Context, paths config.Paths) Report {
	report := Report{OK: true}
	add := func(name string, status Status, message string) {
		check := Check{Name: name, OK: status != StatusFail, Status: status, Message: message}
		report.Checks = append(report.Checks, check)
		if status == StatusFail {
			report.OK = false
		}
	}

	docker, err := compose.DockerBinary()
	if err != nil {
		add("docker", StatusFail, err.Error())
		return report
	}
	add("docker", StatusPass, "command found")

	daemonAvailable := true
	if err := run(ctx, docker, "info"); err != nil {
		daemonAvailable = false
		add("docker daemon", StatusFail, "unavailable: "+err.Error())
	} else {
		add("docker daemon", StatusPass, "available")
	}
	if version, err := compose.ComposeVersion(ctx, docker); err != nil {
		add("docker compose", StatusFail, err.Error())
	} else {
		add("docker compose", StatusPass, "version "+version+" (minimum "+compose.MinimumComposeVersion+")")
	}
	if source, err := compose.DockerSocketSource(ctx, docker); err != nil {
		add("docker endpoint", StatusFail, err.Error())
	} else {
		add("docker endpoint", StatusPass, source)
	}

	if _, err := os.Stat(paths.Environment); err != nil {
		add("installation", StatusFail, "not installed in "+paths.Home)
		return report
	}
	state, err := config.Load(paths)
	if err != nil {
		add("configuration schema", StatusFail, err.Error())
		return report
	}
	if state.ConfigSchemaVersion != config.CurrentSchemaVersion {
		add("configuration schema", StatusFail, "outdated; run cocola install to migrate the deployment configuration")
	} else {
		add("configuration schema", StatusPass, fmt.Sprintf("version %d", config.CurrentSchemaVersion))
		endpoint, endpointErr := config.EffectiveGHCREndpoint(state)
		if endpointErr != nil {
			add("GHCR endpoint", StatusFail, endpointErr.Error())
		} else if imageErr := config.ValidateConfiguredImages(paths, state); imageErr != nil {
			add("GHCR endpoint", StatusFail, endpoint+": "+imageErr.Error())
		} else {
			message := endpoint
			if pending := state.PendingUpgrade; pending != nil && endpoint != state.GHCREndpoint {
				message += " pending; current " + state.GHCREndpoint
			}
			add("GHCR endpoint", StatusPass, message)
		}
	}
	runner, err := compose.New(paths, nil, io.Discard, io.Discard)
	if err != nil {
		add("installation", StatusFail, err.Error())
		return report
	}
	add("installation", StatusPass, paths.Home)
	checkContext, cancel := context.WithTimeout(ctx, 10*time.Second)
	if err := runner.Validate(checkContext); err != nil {
		add("compose config", StatusFail, err.Error())
	} else {
		add("compose config", StatusPass, "valid")
	}
	cancel()

	addDiskCheck(add, "installation disk", paths.Home)
	if !daemonAvailable {
		return report
	}
	if root, err := compose.DockerRootDir(ctx, docker); err != nil {
		add("docker storage disk", StatusWarning, err.Error())
	} else if _, err := os.Stat(root); err != nil {
		add("docker storage disk", StatusWarning, "Docker stores images in "+root+", but that path is not visible from this host")
	} else {
		addDiskCheck(add, "docker storage disk", root)
	}

	volumePresent := inspectVolumes(ctx, runner, state, add)
	statuses := inspectServices(ctx, runner, state, add)
	inspectInternalSCMEndpoint(ctx, runner, state, statuses, add)
	inspectPostgresCredentials(ctx, runner, volumePresent["pgdata"], statuses, add)
	inspectImages(ctx, runner, add)
	return report
}

func addDiskCheck(add func(string, Status, string), name, path string) {
	available, err := host.AvailableDiskBytes(path)
	if err != nil {
		add(name, StatusWarning, "available space could not be determined: "+err.Error())
		return
	}
	message := fmt.Sprintf("%.1f GiB available at %s", float64(available)/(1024*1024*1024), path)
	switch {
	case available < host.MinimumFreeDiskBytes:
		add(name, StatusFail, message+"; free at least 2 GiB before starting Cocola")
	case available < host.WarnFreeDiskBytes:
		add(name, StatusWarning, message+"; sandbox images and workspaces may require more space")
	default:
		add(name, StatusPass, message)
	}
}

func inspectVolumes(
	ctx context.Context,
	runner *compose.Runner,
	state config.State,
	add func(string, Status, string),
) map[string]bool {
	result := make(map[string]bool)
	for _, name := range []string{"pgdata", "redisdata", "miniodata", "forgejodata"} {
		present, err := runner.VolumePresent(ctx, name)
		if err != nil {
			add("volume "+name, StatusFail, err.Error())
			continue
		}
		result[name] = present
		if present {
			status := StatusPass
			message := "present"
			if name == "pgdata" && state.LastSuccessfulVersion == "" {
				status = StatusWarning
				message = "present before the first successful start; credentials will be checked when PostgreSQL is running"
			}
			add("volume "+name, status, message)
			continue
		}
		if state.LastSuccessfulVersion == "" {
			add("volume "+name, StatusWarning, "not created yet")
		} else {
			add("volume "+name, StatusFail, "missing from an installation that previously started successfully")
		}
	}
	return result
}

func inspectServices(
	ctx context.Context,
	runner *compose.Runner,
	state config.State,
	add func(string, Status, string),
) map[string]compose.ServiceStatus {
	statuses, err := runner.ServiceStatuses(ctx)
	if err != nil {
		add("services", StatusFail, err.Error())
		return nil
	}
	if len(statuses) == 0 {
		status := StatusWarning
		if state.LastSuccessfulVersion != "" {
			status = StatusFail
		}
		add("services", status, "no Cocola containers found; run cocola start")
		return nil
	}
	sort.Slice(statuses, func(left, right int) bool {
		return statuses[left].Service < statuses[right].Service
	})
	observed := make(map[string]compose.ServiceStatus, len(statuses))
	for _, service := range statuses {
		observed[service.Service] = service
		name := "service " + service.Service
		stateName := strings.ToLower(service.State)
		health := strings.ToLower(service.Health)
		message := service.Status
		if message == "" {
			message = service.State
		}
		switch {
		case (service.Service == "minio-init" || service.Service == "forgejo-db-init" || service.Service == "forgejo-init") && stateName == "exited" && service.ExitCode == 0:
			add(name, StatusPass, "completed successfully")
		case stateName == "running" && (health == "" || health == "healthy"):
			add(name, StatusPass, message)
		case stateName == "running" && health == "starting":
			add(name, StatusWarning, message)
		case stateName == "exited" && service.ExitCode == 0:
			add(name, StatusWarning, "stopped; run cocola start to resume it")
		default:
			add(name, StatusFail, message+"; inspect with cocola logs "+service.Service+" --tail 200")
		}
	}
	expected := []string{
		"redis", "postgres", "forgejo-db-init", "forgejo", "forgejo-init",
		"minio", "minio-init", "sandbox-manager", "host-agent",
		"llm-gateway", "admin-api", "agent-runtime", "gateway", "web",
	}
	if state.ManagedOpenSandbox {
		expected = append(expected, "opensandbox-server")
	}
	missing := make([]string, 0)
	for _, service := range expected {
		if _, ok := observed[service]; !ok {
			missing = append(missing, service)
		}
	}
	if len(missing) > 0 {
		status := StatusWarning
		if state.LastSuccessfulVersion != "" {
			status = StatusFail
		}
		add("services", status, "containers not created: "+strings.Join(missing, ", "))
	}
	return observed
}

func inspectInternalSCMEndpoint(
	ctx context.Context,
	runner *compose.Runner,
	state config.State,
	statuses map[string]compose.ServiceStatus,
	add func(string, Status, string),
) {
	forgejo, ok := statuses["forgejo"]
	if !ok || strings.ToLower(forgejo.State) != "running" {
		add("internal SCM endpoint", StatusWarning, "not checked because Internal SCM is not running")
		return
	}
	checkContext, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	owned, err := runner.ServiceOwnsPublishedPort(
		checkContext, "forgejo", 3000, state.InternalSCM.HostPort,
	)
	if err != nil {
		add("internal SCM endpoint", StatusFail, err.Error())
		return
	}
	if !owned {
		add(
			"internal SCM endpoint",
			StatusFail,
			fmt.Sprintf("configured host port %d is not published by the Forgejo container", state.InternalSCM.HostPort),
		)
		return
	}
	add(
		"internal SCM endpoint",
		StatusPass,
		fmt.Sprintf("host port %d is owned by the Forgejo container", state.InternalSCM.HostPort),
	)
}

func inspectPostgresCredentials(
	ctx context.Context,
	runner *compose.Runner,
	volumePresent bool,
	statuses map[string]compose.ServiceStatus,
	add func(string, Status, string),
) {
	if !volumePresent {
		return
	}
	postgres, ok := statuses["postgres"]
	if !ok || strings.ToLower(postgres.State) != "running" {
		add("postgres credentials", StatusWarning, "not checked because PostgreSQL is not running")
		return
	}
	checkContext, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := runner.CheckPostgresCredentials(checkContext); err != nil {
		add("postgres credentials", StatusFail, err.Error())
		return
	}
	add("postgres credentials", StatusPass, "current configuration can authenticate")
}

func inspectImages(ctx context.Context, runner *compose.Runner, add func(string, Status, string)) {
	checkContext, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	missing, err := runner.MissingImages(checkContext)
	if err != nil {
		add("required images", StatusFail, err.Error())
		return
	}
	if len(missing) > 0 {
		add("required images", StatusWarning, "not cached locally: "+strings.Join(missing, ", ")+"; cocola start will pull them")
		return
	}
	add("required images", StatusPass, "all service and managed sandbox runtime images are cached locally")
}

func run(ctx context.Context, command string, args ...string) error {
	checkContext, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return exec.CommandContext(checkContext, command, args...).Run()
}
