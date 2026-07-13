package command

import (
	"fmt"

	"github.com/cocola-project/cocola/apps/cli/internal/version"
	"github.com/spf13/cobra"
)

func (a *application) versionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Show Cocola CLI version",
		RunE: func(_ *cobra.Command, _ []string) error {
			info := version.Current()
			if a.json {
				return a.printer().Encode(info)
			}
			fmt.Fprintf(a.io.Out, "cocola %s (%s, %s, %s/%s)\n", info.Version, info.Commit, info.Go, info.OS, info.Arch)
			return nil
		},
	}
}
