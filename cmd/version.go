package cmd

import (
	"fmt"
	"github.com/OrcaCD/orca-cd/internal/config"

	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version information",
	Long:  `Display version, build time, and git commit information.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("Version:    %s\n", config.Version)
		fmt.Printf("Build Time: %s\n", config.BuildTime)
		fmt.Printf("Git Commit: %s\n", config.GitCommit)
	},
}
