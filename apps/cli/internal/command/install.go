package command

import (
	"errors"
	"fmt"
	"net/url"
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
		Short: "Configure Cocola",
		RunE: func(_ *cobra.Command, _ []string) error {
			options.Home = a.home
			if options.ExternalOpenSandboxURL != "" {
				options.ManagedOpenSandbox = false
			}
			if !yes {
				if !a.interactive() {
					return errors.New("interactive terminal unavailable; pass --yes with explicit flags")
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
			paths, err := config.ResolvePaths(options.Home)
			if err != nil {
				return err
			}
			credentials, err := config.WriteInstallation(paths, options, assets.Compose)
			if err != nil {
				if errors.Is(err, config.ErrAlreadyInstalled) {
					return fmt.Errorf("%w: %s; use cocola up", err, paths.Home)
				}
				return err
			}
			printer := a.printer()
			printer.Success("Configuration written to " + paths.Home)
			result := installResult{
				Status: "configured",
				Home:   paths.Home, WebURL: fmt.Sprintf("http://localhost:%d", options.WebPort),
				GatewayURL:    fmt.Sprintf("http://localhost:%d", options.GatewayPort),
				AdminUsername: credentials.AdminUsername, AdminEmail: credentials.AdminEmail,
				AdminPassword: credentials.AdminPassword,
			}
			if a.json {
				return printer.Encode(result)
			}
			printer.Section("Installation")
			printer.KeyValues([][2]string{
				{"Web", result.WebURL}, {"Gateway", result.GatewayURL},
				{"Admin", result.AdminUsername + " / " + result.AdminEmail},
				{"Password", result.AdminPassword}, {"Config", paths.Environment},
			})
			printer.Warn("The admin password is shown once. Store it securely.")
			printer.Info("Review the configuration, then run: cocola up")
			return nil
		},
	}
	flags := command.Flags()
	flags.StringVar(&options.Version, "version", options.Version, "container image version")
	flags.StringVar(&options.Registry, "registry", options.Registry, "container image registry")
	flags.StringVar(&options.AdminUsername, "admin-username", options.AdminUsername, "bootstrap admin username")
	flags.StringVar(&options.AdminEmail, "admin-email", options.AdminEmail, "bootstrap admin email")
	flags.StringVar(&options.AdminPassword, "admin-password", "", "bootstrap admin password (prefer the interactive prompt)")
	flags.IntVar(&options.WebPort, "web-port", options.WebPort, "Web host port")
	flags.IntVar(&options.GatewayPort, "gateway-port", options.GatewayPort, "Gateway host port")
	flags.IntVar(&options.LLMPort, "llm-port", options.LLMPort, "LLM Gateway host port used by sandboxes")
	flags.BoolVar(&options.ManagedOpenSandbox, "managed-opensandbox", true, "run the bundled OpenSandbox server")
	flags.StringVar(&options.ExternalOpenSandboxURL, "external-opensandbox-url", "", "use an externally managed OpenSandbox URL")
	flags.StringVar(&options.SandboxLLMBaseURL, "sandbox-llm-base-url", "", "LLM Gateway URL reachable from external sandboxes")
	flags.StringVar(&options.SessionVolumeSize, "session-volume-size", options.SessionVolumeSize, "soft capacity request for each new Session Volume")
	flags.BoolVarP(&yes, "yes", "y", false, "accept flags/defaults without prompting")
	return command
}

func (a *application) runInstallForm(options *config.Options) error {
	mode := "managed"
	if !options.ManagedOpenSandbox {
		mode = "external"
	}
	webPort := strconv.Itoa(options.WebPort)
	gatewayPort := strconv.Itoa(options.GatewayPort)
	llmPort := strconv.Itoa(options.LLMPort)
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().Title("Installation directory").Value(&options.Home),
			huh.NewInput().Title("Cocola version").Value(&options.Version),
			huh.NewInput().Title("Image registry").Value(&options.Registry),
		),
		huh.NewGroup(
			huh.NewInput().Title("Admin username").Value(&options.AdminUsername),
			huh.NewInput().Title("Admin email").Value(&options.AdminEmail),
			huh.NewInput().Title("Admin password").Description("Leave blank to generate one").EchoMode(huh.EchoModePassword).Value(&options.AdminPassword),
		),
		huh.NewGroup(
			huh.NewInput().Title("Web port").Value(&webPort).Validate(validatePort),
			huh.NewInput().Title("Gateway port").Value(&gatewayPort).Validate(validatePort),
			huh.NewInput().Title("LLM Gateway port").Value(&llmPort).Validate(validatePort),
			huh.NewSelect[string]().Title("OpenSandbox").Options(
				huh.NewOption("Managed Docker server", "managed"),
				huh.NewOption("External server", "external"),
			).Value(&mode),
		),
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
	options.ManagedOpenSandbox = mode == "managed"
	if !options.ManagedOpenSandbox {
		external := options.ExternalOpenSandboxURL
		sandboxLLMURL := options.SandboxLLMBaseURL
		second := huh.NewForm(huh.NewGroup(
			huh.NewInput().Title("External OpenSandbox URL").Placeholder("https://sandbox.example.com/v1").Value(&external).Validate(validateURL),
			huh.NewInput().Title("Sandbox-accessible LLM Gateway URL").Description("Must be reachable from sandboxes created by the external server").Placeholder("https://llm.cocola.example.com").Value(&sandboxLLMURL).Validate(validateURL),
		)).WithInput(a.io.In).WithOutput(a.io.Err).WithAccessible(a.accessible)
		if !ui.AutoColor(a.io.Err, a.noColor || a.json) {
			second = second.WithTheme(huh.ThemeBase())
		} else {
			second = second.WithTheme(ui.FormTheme())
		}
		if err := second.Run(); err != nil {
			return fmt.Errorf("OpenSandbox form: %w", err)
		}
		options.ExternalOpenSandboxURL = external
		options.SandboxLLMBaseURL = sandboxLLMURL
	}
	return nil
}

func validatePort(value string) error {
	port, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || port < 1 || port > 65535 {
		return errors.New("enter a port between 1 and 65535")
	}
	return nil
}

func validateURL(value string) error {
	parsed, err := url.ParseRequestURI(value)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return errors.New("enter an absolute http(s) URL")
	}
	return nil
}
