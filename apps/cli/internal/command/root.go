package command

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/cocola-project/cocola/apps/cli/internal/config"
	"github.com/cocola-project/cocola/apps/cli/internal/ui"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

type IO struct {
	In  io.Reader
	Out io.Writer
	Err io.Writer
}

type application struct {
	io         IO
	home       string
	noColor    bool
	json       bool
	accessible bool
}

func Execute(ctx context.Context, args []string, streams IO) error {
	app := &application{io: streams, home: config.DefaultHome()}
	root := app.rootCommand()
	root.SetArgs(args)
	root.SetIn(streams.In)
	root.SetOut(streams.Out)
	root.SetErr(streams.Err)
	if err := root.ExecuteContext(ctx); err != nil {
		if app.json {
			_ = json.NewEncoder(streams.Err).Encode(map[string]string{"error": err.Error()})
		} else {
			app.printer().Error(err.Error())
		}
		return err
	}
	return nil
}

func (a *application) rootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:           "cocola",
		Short:         "Install and operate Cocola",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(command *cobra.Command, _ []string) error {
			printer := a.printer()
			if a.json {
				return printer.Encode(map[string]any{
					"name":     "cocola",
					"commands": []string{"install", "up", "down", "restart", "status", "logs", "doctor", "version"},
				})
			}
			printer.Banner()
			printer.Section("Common commands")
			printer.KeyValues([][2]string{
				{"cocola install", "Create the deployment configuration"},
				{"cocola up", "Pull images and start Cocola"},
				{"cocola status", "Show service status"},
				{"cocola logs", "Follow service logs"},
				{"cocola doctor", "Diagnose the host and installation"},
			})
			fmt.Fprintln(command.OutOrStdout(), "\nRun cocola --help for every command.")
			return nil
		},
	}
	flags := root.PersistentFlags()
	flags.StringVar(&a.home, "home", a.home, "Cocola installation directory")
	flags.BoolVar(&a.noColor, "no-color", false, "disable ANSI colors")
	flags.BoolVar(&a.json, "json", false, "emit machine-readable JSON where supported")
	flags.BoolVar(&a.accessible, "accessible", os.Getenv("ACCESSIBLE") != "", "use screen-reader friendly prompts")

	root.AddCommand(
		a.installCommand(), a.lifecycleCommand("up"), a.lifecycleCommand("down"),
		a.lifecycleCommand("restart"), a.statusCommand(), a.logsCommand(),
		a.doctorCommand(), a.versionCommand(),
	)
	return root
}

func (a *application) printer() ui.Printer {
	return ui.Printer{
		Out: a.io.Out, Err: a.io.Err, JSON: a.json,
		Color: ui.AutoColor(a.io.Out, a.noColor || a.json),
	}
}

func (a *application) paths() (config.Paths, error) {
	return config.ResolvePaths(a.home)
}

func (a *application) interactive() bool {
	input, inputOK := a.io.In.(*os.File)
	output, outputOK := a.io.Err.(*os.File)
	return inputOK && outputOK && term.IsTerminal(int(input.Fd())) && term.IsTerminal(int(output.Fd()))
}
