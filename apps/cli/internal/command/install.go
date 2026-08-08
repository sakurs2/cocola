package command

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/cocola-project/cocola/apps/cli/internal/assets"
	"github.com/cocola-project/cocola/apps/cli/internal/config"
	"github.com/cocola-project/cocola/apps/cli/internal/ui"
	"github.com/cocola-project/cocola/apps/cli/internal/version"
	"github.com/spf13/cobra"
)

type installResult struct {
	Status        string `json:"status"`
	Home          string `json:"home"`
	ConfigFile    string `json:"config_file"`
	WebURL        string `json:"web_url"`
	GatewayURL    string `json:"gateway_url"`
	AdminUsername string `json:"admin_username"`
	AdminEmail    string `json:"admin_email"`
	AdminPassword string `json:"admin_password"`
}

func (a *application) installCommand() *cobra.Command {
	options := config.Defaults(version.ImageTag())
	var yes bool
	command := &cobra.Command{
		Use:   "install",
		Short: "Create or upgrade the Cocola deployment configuration",
		RunE: func(_ *cobra.Command, _ []string) error {
			options.Home = a.home
			paths, err := config.ResolvePaths(options.Home)
			if err != nil {
				return err
			}
			if _, err := os.Stat(paths.Environment); err == nil {
				return withOperationLock(paths, "cocola install", func() error {
					result, err := config.PrepareUpgrade(paths, options.Version, assets.Compose)
					if err != nil {
						return err
					}
					if a.json {
						status := "up_to_date"
						if result.Updated {
							status = "upgrade_prepared"
						}
						return a.printer().Encode(map[string]any{"status": status, "upgrade": result})
					}
					printUpgradeSummary(a.printer(), result)
					return nil
				})
			} else if !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("inspect existing installation: %w", err)
			}
			if options.ExternalOpenSandboxURL != "" {
				options.ManagedOpenSandbox = false
			}
			if !yes {
				if !a.interactive() {
					return errors.New("cocola install requires an interactive terminal")
				}
				a.printer().Banner()
				if err := a.runInstallForm(&options); err != nil {
					return err
				}
				a.home = options.Home
			}
			if err := options.Validate(); err != nil {
				return err
			}
			paths, err = config.ResolvePaths(options.Home)
			if err != nil {
				return err
			}
			var credentials config.Credentials
			err = withOperationLock(paths, "cocola install", func() error {
				var writeErr error
				credentials, writeErr = config.WriteInstallation(paths, options, assets.Compose)
				if errors.Is(writeErr, config.ErrAlreadyInstalled) {
					return fmt.Errorf("%w: %s; rerun cocola install to prepare an upgrade", writeErr, paths.Home)
				}
				return writeErr
			})
			if err != nil {
				return err
			}
			printer := a.printer()
			publicURL, err := options.PublicOrigin()
			if err != nil {
				return err
			}
			result := installResult{
				Status:        "configured",
				Home:          paths.Home,
				ConfigFile:    paths.Environment,
				WebURL:        publicURL,
				GatewayURL:    fmt.Sprintf("http://localhost:%d", options.GatewayPort),
				AdminUsername: credentials.AdminUsername, AdminEmail: credentials.AdminEmail,
				AdminPassword: credentials.AdminPassword,
			}
			if a.json {
				return printer.Encode(result)
			}
			printInstallSummary(printer, result)
			return nil
		},
	}
	flags := command.Flags()
	flags.StringVar(&options.Version, "version", options.Version, "container image version")
	flags.StringVar(&options.Registry, "registry", options.Registry, "container image registry")
	flags.StringVar(&options.PublicURL, "public-url", options.PublicURL, "additional public URL for callbacks or a host-rewriting proxy")
	flags.StringVar(&options.AdminUsername, "admin-username", options.AdminUsername, "bootstrap admin username")
	flags.StringVar(&options.AdminEmail, "admin-email", options.AdminEmail, "bootstrap admin email")
	flags.StringVar(&options.AdminPassword, "admin-password", "", "bootstrap admin password (prefer the interactive prompt)")
	flags.IntVar(&options.WebPort, "web-port", options.WebPort, "Web host port")
	flags.IntVar(&options.GatewayPort, "gateway-port", options.GatewayPort, "Gateway host port")
	flags.IntVar(&options.LLMPort, "llm-port", options.LLMPort, "LLM Gateway host port used by sandboxes")
	flags.IntVar(&options.InternalSCM.HostPort, "internal-scm-port", options.InternalSCM.HostPort, "Internal SCM loopback host port")
	flags.StringVar(&options.InternalSCM.SandboxCloneURL, "sandbox-internal-scm-url", options.InternalSCM.SandboxCloneURL, "Internal SCM URL reachable from external sandboxes")
	flags.BoolVar(&options.ManagedOpenSandbox, "managed-opensandbox", true, "run the bundled OpenSandbox server")
	flags.StringVar(&options.ExternalOpenSandboxURL, "external-opensandbox-url", "", "use an externally managed OpenSandbox URL")
	flags.StringVar(&options.SandboxLLMBaseURL, "sandbox-llm-base-url", "", "LLM Gateway URL reachable from external sandboxes")
	flags.StringVar(&options.SessionVolumeSize, "session-volume-size", options.SessionVolumeSize, "soft capacity request for each new Session Volume")
	flags.BoolVarP(&yes, "yes", "y", false, "accept flags/defaults without prompting")
	if flag := flags.Lookup("yes"); flag != nil {
		flag.Hidden = true
	}
	return command
}

func printUpgradeSummary(printer ui.Printer, result config.UpgradeResult) {
	if !result.Updated {
		printer.Success("Cocola deployment configuration is already up to date.")
		printer.Info("Start or resume Cocola with:")
		printer.Command("cocola start")
		return
	}
	printer.Success("Cocola upgrade is ready.")
	printer.Section("Upgrade summary")
	printer.KeyValues([][2]string{
		{"Current version", result.FromVersion},
		{"Target version", result.ToVersion},
		{"Deployment backup", result.BackupDir},
	})
	printer.Info("Your ports, administrator account, secrets, and custom settings were preserved.")
	printer.Info("Apply the upgrade and run health checks with:")
	printer.Command("cocola start")
}

func (a *application) runInstallForm(options *config.Options) error {
	webPort := strconv.Itoa(options.WebPort)
	gatewayPort := strconv.Itoa(options.GatewayPort)
	llmPort := strconv.Itoa(options.LLMPort)
	internalSCMPort := strconv.Itoa(options.InternalSCM.HostPort)
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewNote().
				Title("Welcome to Cocola").
				Description("Set up your administrator account and service ports. Every field has a safe default, so you can press Enter to continue.").
				Next(true).
				NextLabel("Start setup"),
		).Title("Quick setup"),
		huh.NewGroup(
			huh.NewInput().
				Title("Admin username").
				Description("Used to sign in to the Admin console.").
				Value(&options.AdminUsername),
			huh.NewInput().
				Title("Admin email").
				Description("Also accepted as a sign-in identifier.").
				Value(&options.AdminEmail),
			huh.NewInput().
				Title("Admin password").
				Description("Leave blank to generate a secure password automatically.").
				Placeholder("Generated automatically").
				EchoMode(huh.EchoModePassword).
				Value(&options.AdminPassword),
		).Title("Administrator account").Description("Press Enter to keep the suggested values."),
		huh.NewGroup(
			huh.NewInput().
				Title("Web port").
				Description("Cocola Web and Admin console.").
				Value(&webPort).
				Validate(validatePort),
			huh.NewInput().
				Title("Gateway port").
				Description("Public API gateway.").
				Value(&gatewayPort).
				Validate(validatePort),
			huh.NewInput().
				Title("LLM Gateway port").
				Description("Model gateway used by Agent sandboxes.").
				Value(&llmPort).
				Validate(validatePort),
			huh.NewInput().
				Title("Internal SCM port").
				Description("Loopback-only Git service used by local Projects.").
				Value(&internalSCMPort).
				Validate(validatePort),
		).Title("Service ports").Description("The defaults work for a standard local installation."),
		huh.NewGroup(
			huh.NewNote().
				Title("Security keys").
				Description("Cocola will generate unique authentication, encryption, database, storage, and SCM secrets. They are stored with owner-only permissions in the generated configuration file.").
				Next(true).
				NextLabel("Create configuration"),
		).Title("Security"),
	).WithInput(a.io.In).WithOutput(a.io.Err).WithAccessible(a.accessible)
	if !ui.AutoColor(a.io.Err, a.noColor || a.json) {
		form = form.WithTheme(huh.ThemeBase())
	} else {
		form = form.WithTheme(ui.FormTheme())
	}
	if err := form.Run(); err != nil {
		return fmt.Errorf("installation form: %w", err)
	}
	var err error
	if options.WebPort, err = strconv.Atoi(webPort); err != nil {
		return err
	}
	if options.GatewayPort, err = strconv.Atoi(gatewayPort); err != nil {
		return err
	}
	if options.LLMPort, err = strconv.Atoi(llmPort); err != nil {
		return err
	}
	if options.InternalSCM.HostPort, err = strconv.Atoi(internalSCMPort); err != nil {
		return err
	}
	return nil
}

func printInstallSummary(printer ui.Printer, result installResult) {
	printer.Success("Cocola configuration is ready.")
	printer.Section("Installation summary")
	printer.KeyValues([][2]string{
		{"Web app", result.WebURL},
		{"Gateway", result.GatewayURL},
		{"Admin account", result.AdminUsername + " / " + result.AdminEmail},
		{"Admin password", result.AdminPassword},
		{"Configuration", result.ConfigFile},
	})
	printer.Warn("The admin password is shown only once. Store it securely.")
	printer.Section("Next step")
	printer.Info("Review the generated configuration file before starting Cocola:")
	printer.Path(result.ConfigFile)
	printer.Info("When you are ready, start all services with:")
	printer.Command("cocola start")
}

func validatePort(value string) error {
	port, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || port < 1 || port > 65535 {
		return errors.New("enter a port between 1 and 65535")
	}
	return nil
}
