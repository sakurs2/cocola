package command

import (
	"errors"

	"github.com/cocola-project/cocola/apps/cli/internal/compose"
	"github.com/spf13/cobra"
)

func (a *application) lifecycleCommand(action string) *cobra.Command {
	return &cobra.Command{
		Use:   action,
		Short: map[string]string{"up": "Start Cocola", "down": "Stop Cocola", "restart": "Restart Cocola"}[action],
		RunE: func(command *cobra.Command, _ []string) error {
			runner, err := a.runner(false)
			if err != nil {
				return err
			}
			printer := a.printer()
			switch action {
			case "up":
				printer.Info("Pulling Cocola images")
				if err := runner.Pull(command.Context()); err != nil {
					return err
				}
				printer.Info("Starting Cocola")
				err = runner.Up(command.Context())
			case "down":
				printer.Info("Stopping Cocola")
				err = runner.Down(command.Context())
			case "restart":
				printer.Info("Restarting Cocola")
				err = runner.Restart(command.Context())
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
		},
	}
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
	output := a.io.Out
	if a.json && !rawOutput {
		output = a.io.Err
	}
	return compose.New(paths, a.io.In, output, a.io.Err)
}
