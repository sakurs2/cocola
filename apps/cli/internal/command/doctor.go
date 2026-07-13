package command

import (
	"errors"

	"github.com/cocola-project/cocola/apps/cli/internal/doctor"
	"github.com/spf13/cobra"
)

func (a *application) doctorCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose Docker and the Cocola installation",
		RunE: func(command *cobra.Command, _ []string) error {
			paths, err := a.paths()
			if err != nil {
				return err
			}
			report := doctor.Run(command.Context(), paths)
			printer := a.printer()
			if a.json {
				if err := printer.Encode(report); err != nil {
					return err
				}
			} else {
				printer.Section("Cocola doctor")
				for _, check := range report.Checks {
					message := check.Name + ": " + check.Message
					if check.OK {
						printer.Success(message)
					} else {
						printer.Warn(message)
					}
				}
			}
			if !report.OK {
				return errors.New("one or more checks failed")
			}
			return nil
		},
	}
}
