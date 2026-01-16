package hub

import (
	"fmt"

	"github.com/OrcaCD/orca-cd/internal/config"
	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version number",
	Long:  `Print the version number, build time, and git commit of the hub.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("OrcaCD Hub\n")
		fmt.Printf("Version:    %s\n", config.Version)
		fmt.Printf("Build Time: %s\n", config.BuildTime)
		fmt.Printf("Git Commit: %s\n", config.GitCommit)
	},
}
