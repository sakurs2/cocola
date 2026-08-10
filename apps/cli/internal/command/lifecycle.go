package command

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cocola-project/cocola/apps/cli/internal/compose"
	"github.com/cocola-project/cocola/apps/cli/internal/config"
	"github.com/cocola-project/cocola/apps/cli/internal/ui"
	"github.com/spf13/cobra"
)

func (a *application) lifecycleCommand(action string) *cobra.Command {
	short := map[string]string{
		"start": "Create, update, or start Cocola",
		"stop":  "Stop Cocola and preserve its containers",
	}
	return &cobra.Command{
		Use:   action,
		Short: short[action],
		RunE: func(command *cobra.Command, _ []string) error {
			paths, err := a.paths()
			if err != nil {
				return err
			}
			return withOperationLock(paths, "cocola "+action, func() error {
				runner, err := a.runnerAt(paths, false)
				if err != nil {
					return err
				}
				if action == "start" {
					if err := compose.CheckDocker(command.Context()); err != nil {
						return err
					}
				}
				printer := a.printer()
				switch action {
				case "start":
					return a.start(command.Context(), runner)
				case "stop":
					printer.Info("Stopping Cocola and preserving its containers")
					err = runner.Stop(command.Context())
				default:
					return errors.New("unsupported lifecycle action")
				}
				if err != nil {
					return err
				}
				if a.json {
					return printer.Encode(map[string]string{"status": action + " complete"})
				}
				printer.Success("Cocola " + action + " complete")
				return nil
			})
		},
	}
}

type lifecycleResult struct {
	Status          string             `json:"status"`
	Version         string             `json:"version"`
	PreviousVersion string             `json:"previous_version,omitempty"`
	ImageSource     config.ImageSource `json:"image_source"`
	WebURL          string             `json:"web_url"`
	AdminURL        string             `json:"admin_url"`
	ModelSetupURL   string             `json:"model_setup_url"`
	BackupDir       string             `json:"backup_dir,omitempty"`
}

func (a *application) start(ctx context.Context, runner *compose.Runner) error {
	printer := a.printer()
	printer.Info("Checking the Cocola host and deployment configuration")
	warnings, err := runStartPreflight(ctx, runner)
	if err != nil {
		return err
	}
	for _, warning := range warnings {
		printer.Warn(warning)
	}

	pending := runner.State.PendingUpgrade
	backupDir := ""
	if pending != nil {
		backupDir = pending.BackupDir
		if pending.FromVersion != pending.ToVersion {
			printer.Info("Backing up the existing Cocola and Internal SCM data")
			if err := a.backupUpgradeDatabase(ctx, runner.Paths, pending); err != nil {
				return err
			}
		}
	}

	needsPull := pending != nil || runner.State.LastSuccessfulVersion == ""
	if needsPull {
		printer.Info("Pulling images through " + runner.State.ImageSource.DisplayName())
		if pullErr := runner.Pull(ctx); pullErr != nil {
			cached, inspectErr := runner.ImagesPresent(ctx)
			if inspectErr != nil {
				pullErr = errors.Join(pullErr, inspectErr)
			}
			if !cached {
				a.printImageSourceFailureHint(runner.State.ImageSource)
				if pending != nil {
					return a.restoreFailedUpgrade(ctx, runner.Paths, pending, pullErr)
				}
				a.printStartFailure(ctx, runner)
				return fmt.Errorf("pull required images: %w", pullErr)
			}
			printer.Warn("The image registry is unavailable; all required images are cached locally, so startup will continue.")
		}
	} else {
		printer.Info("Using the installed Cocola images")
	}
	if runner.State.LastSuccessfulVersion == "" {
		hasDatabase, inspectErr := runner.VolumePresent(ctx, "pgdata")
		if inspectErr != nil {
			return inspectErr
		}
		if hasDatabase {
			printer.Info("Verifying the existing PostgreSQL volume against the current configuration")
			if prepareErr := runner.PrepareExistingPostgres(ctx); prepareErr != nil {
				if errors.Is(prepareErr, compose.ErrPostgresCredentialsMismatch) {
					a.printPostgresMismatch()
				}
				a.printStartFailure(ctx, runner)
				return prepareErr
			}
		}
	}

	printer.Info("Starting Cocola and waiting for service health checks")
	if err := runner.Start(ctx); err != nil {
		if pending != nil {
			cleanupContext, cancelCleanup := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
			cleanupErr := runner.RemoveFailedStart(cleanupContext)
			cancelCleanup()
			return a.restoreFailedUpgrade(ctx, runner.Paths, pending, errors.Join(err, cleanupErr))
		}
		a.printStartFailure(ctx, runner)
		return err
	}
	state, err := config.MarkStarted(runner.Paths)
	if err != nil {
		return fmt.Errorf("services are healthy but the deployment state could not be committed: %w", err)
	}
	webURL := stateWebURL(state)
	result := lifecycleResult{
		Status: "ready", Version: state.Version, ImageSource: state.ImageSource, WebURL: webURL,
		AdminURL:      strings.TrimRight(webURL, "/") + "/admin",
		ModelSetupURL: strings.TrimRight(webURL, "/") + "/admin/models",
		BackupDir:     backupDir,
	}
	if pending != nil {
		result.PreviousVersion = pending.FromVersion
	}
	if a.json {
		return printer.Encode(result)
	}
	printStartSummary(printer, state, pending, backupDir)
	return nil
}

func (a *application) backupUpgradeDatabase(
	ctx context.Context,
	paths config.Paths,
	pending *config.PendingUpgrade,
) (resultErr error) {
	backupPaths, err := config.BackupPaths(paths, pending.BackupDir)
	if err != nil {
		return err
	}
	backupRunner, err := compose.New(backupPaths, a.io.In, a.io.Out, a.io.Err)
	if err != nil {
		return fmt.Errorf("open previous Cocola deployment: %w", err)
	}
	hasDatabase, err := backupRunner.VolumePresent(ctx, "pgdata")
	if err != nil {
		return err
	}
	hasForgejoService, err := backupRunner.ServiceDefined(ctx, "forgejo")
	if err != nil {
		return err
	}
	hasForgejoData := false
	if hasForgejoService {
		hasForgejoData, err = backupRunner.VolumePresent(ctx, "forgejodata")
		if err != nil {
			return err
		}
	}
	if !hasDatabase {
		if hasForgejoData {
			return errors.New("Forgejo data volume exists but the PostgreSQL volume is missing")
		}
		return nil
	}
	postgresDestination := config.DatabaseBackupPath(pending.BackupDir)
	postgresMissing, err := backupFileMissing(postgresDestination)
	if err != nil {
		return err
	}
	forgejoDatabaseDestination := config.ForgejoDatabaseBackupPath(pending.BackupDir)
	forgejoDataDestination := config.ForgejoDataBackupPath(pending.BackupDir)
	forgejoDatabaseMissing, forgejoDataMissing := false, false
	if hasForgejoData {
		forgejoDatabaseMissing, err = backupFileMissing(forgejoDatabaseDestination)
		if err != nil {
			return err
		}
		forgejoDataMissing, err = backupFileMissing(forgejoDataDestination)
		if err != nil {
			return err
		}
	}
	if !postgresMissing && !forgejoDatabaseMissing && !forgejoDataMissing {
		return nil
	}
	if err := backupRunner.StartService(ctx, "postgres"); err != nil {
		return fmt.Errorf("start PostgreSQL for backup: %w", err)
	}
	stoppedServices := make([]string, 0, 3)
	if hasForgejoData && (forgejoDatabaseMissing || forgejoDataMissing) {
		defer func() {
			for index := len(stoppedServices) - 1; index >= 0; index-- {
				if restartErr := backupRunner.StartService(context.WithoutCancel(ctx), stoppedServices[index]); restartErr != nil {
					resultErr = errors.Join(resultErr, fmt.Errorf("restart %s after backup: %w", stoppedServices[index], restartErr))
				}
			}
		}()
		for _, service := range []string{"gateway", "agent-runtime", "forgejo"} {
			running, runningErr := backupRunner.ServiceRunning(ctx, service)
			if runningErr != nil {
				return runningErr
			}
			if !running {
				continue
			}
			if stopErr := backupRunner.StopService(ctx, service); stopErr != nil {
				return fmt.Errorf("stop %s for a consistent backup: %w", service, stopErr)
			}
			stoppedServices = append(stoppedServices, service)
		}
	}
	if postgresMissing {
		if err := backupRunner.BackupDatabase(ctx, postgresDestination); err != nil {
			return err
		}
	}
	if forgejoDatabaseMissing {
		if err := backupRunner.BackupForgejoDatabase(ctx, forgejoDatabaseDestination); err != nil {
			return err
		}
	}
	if forgejoDataMissing {
		if err := backupRunner.BackupForgejoData(ctx, forgejoDataDestination); err != nil {
			return err
		}
	}
	return nil
}

func backupFileMissing(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return false, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	return false, fmt.Errorf("inspect backup %s: %w", filepath.Base(path), err)
}

func (a *application) restoreFailedUpgrade(
	ctx context.Context,
	paths config.Paths,
	pending *config.PendingUpgrade,
	cause error,
) error {
	printer := a.printer()
	printer.Warn("The Cocola deployment update failed. Restoring the previous deployment configuration.")
	backupDir, rollbackErr := config.RollbackUpgrade(paths)
	if rollbackErr != nil {
		printer.Warn("Automatic rollback failed. The deployment backup is still available at:")
		printer.Path(pending.BackupDir)
		return errors.Join(cause, fmt.Errorf("restore previous deployment: %w", rollbackErr))
	}
	printer.Warn("The previous deployment configuration was restored. Cocola was not restarted automatically.")
	printer.Info("Recover the previous deployment with:")
	printer.Command("cocola start")
	printer.Info("Retry the deployment update after resolving the error with:")
	retry := "cocola install --version " + pending.ToVersion
	if pending.ToImageSource.Valid() {
		retry += " --image-source " + string(pending.ToImageSource)
	}
	if pending.ToImageRegistry != "" && pending.ToImageRegistry != pending.ToImageSource.Registry() {
		retry += " --registry " + pending.ToImageRegistry
	}
	printer.Command(retry)
	printer.Command("cocola start")
	if pending.FromVersion != pending.ToVersion {
		printer.Info("Deployment and PostgreSQL backups are retained for manual recovery. The database dump is never restored automatically.")
	} else {
		printer.Info("The deployment configuration backup is retained for manual recovery.")
	}
	printer.Path(backupDir)
	restoredRunner, runnerErr := compose.New(paths, a.io.In, a.io.Out, a.io.Err)
	if runnerErr != nil {
		return errors.Join(cause, fmt.Errorf("load restored deployment: %w", runnerErr))
	}
	a.printStartFailure(ctx, restoredRunner)
	return cause
}

func (a *application) printImageSourceFailureHint(source config.ImageSource) {
	if a.json || source != config.ImageSourceCNMirror {
		return
	}
	printer := a.printer()
	printer.Info("The selected acceleration source could not provide every required image. Switch to direct download with:")
	printer.Command("cocola install --image-source direct")
	printer.Command("cocola start")
}

func (a *application) printPostgresMismatch() {
	if a.json {
		return
	}
	printer := a.printer()
	printer.Warn("The existing PostgreSQL data was created with credentials from a different Cocola configuration.")
	printer.Info("If the data is important, restore the original Cocola configuration and run:")
	printer.Command("cocola start")
	printer.Info("If the data can be discarded, stop Cocola, remove the cocola_pgdata volume, and run:")
	printer.Command("docker volume rm cocola_pgdata")
	printer.Command("cocola start")
}

func (a *application) printStartFailure(ctx context.Context, runner *compose.Runner) {
	if a.json {
		return
	}
	printer := a.printer()
	printer.Section("Current service status")
	_ = runner.Status(ctx, false)
	printer.Info("Inspect the failure with:")
	printer.Command("cocola logs --tail 200")
	printer.Command("cocola doctor")
}

func printStartSummary(printer ui.Printer, state config.State, pending *config.PendingUpgrade, backupDir string) {
	webURL := stateWebURL(state)
	printer.Success("Cocola is ready.")
	printer.Section("Deployment")
	values := make([][2]string, 0, 5)
	if pending != nil && pending.FromVersion != state.Version {
		values = append(values, [2]string{"Before version", pending.FromVersion})
	}
	values = append(values, [2]string{"Current version", state.Version})
	if pending != nil && pending.FromImageSource != state.ImageSource {
		values = append(values, [2]string{"Before image source", pending.FromImageSource.DisplayName()})
	}
	values = append(values, [2]string{"Image source", state.ImageSource.DisplayName()})
	if pending != nil && pending.FromImageRegistry != pending.ToImageRegistry {
		values = append(values,
			[2]string{"Before image registry", pending.FromImageRegistry},
			[2]string{"Current image registry", pending.ToImageRegistry},
		)
	}
	printer.KeyValues(values)
	printer.Section("Access Cocola")
	printer.KeyValues([][2]string{
		{"Web app", webURL},
		{"Admin console", strings.TrimRight(webURL, "/") + "/admin"},
		{"Model setup", strings.TrimRight(webURL, "/") + "/admin/models"},
	})
	if strings.Contains(webURL, "localhost") || strings.Contains(webURL, "127.0.0.1") {
		printer.Info(fmt.Sprintf("From another device, open http://<server-ip>:%d", state.WebPort))
	}
	printer.Info("Before starting your first conversation, configure a Provider and default model in Admin → Models.")
	if backupDir != "" {
		printer.Info("The pre-update deployment backup is available at:")
		printer.Path(backupDir)
	}
}

func stateWebURL(state config.State) string {
	if state.PublicURL != "" {
		return state.PublicURL
	}
	return fmt.Sprintf("http://localhost:%d", state.WebPort)
}

func (a *application) statusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show Cocola service status",
		RunE: func(command *cobra.Command, _ []string) error {
			runner, err := a.runner(true)
			if err != nil {
				return err
			}
			if !a.json {
				a.printer().Section("Cocola services")
			}
			return runner.Status(command.Context(), a.json)
		},
	}
}

func (a *application) logsCommand() *cobra.Command {
	var follow bool
	var tail int
	command := &cobra.Command{
		Use:   "logs [service]",
		Short: "Show raw Docker logs",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			if a.json {
				return errors.New("logs produces a raw stream and does not support --json")
			}
			runner, err := a.runner(true)
			if err != nil {
				return err
			}
			service := ""
			if len(args) == 1 {
				service = args[0]
			}
			return runner.Logs(command.Context(), service, follow, tail)
		},
	}
	command.Flags().BoolVarP(&follow, "follow", "f", false, "follow log output")
	command.Flags().IntVar(&tail, "tail", 200, "number of lines to show")
	return command
}

func (a *application) runner(rawOutput bool) (*compose.Runner, error) {
	paths, err := a.paths()
	if err != nil {
		return nil, err
	}
	return a.runnerAt(paths, rawOutput)
}

func (a *application) runnerAt(paths config.Paths, rawOutput bool) (*compose.Runner, error) {
	output := a.io.Out
	if a.json && !rawOutput {
		output = a.io.Err
	}
	return compose.New(paths, a.io.In, output, a.io.Err)
}
