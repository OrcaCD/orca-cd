package main

import (
	"fmt"
	"os"

	"github.com/OrcaCD/orca-cd/internal/agent"
	"github.com/OrcaCD/orca-cd/internal/version"
	"github.com/spf13/cobra"
)

func main() {
	rootCmd := &cobra.Command{
		Use:                "anget [flags]",
		Short:              "Orca Agent",
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return agent.Run(agent.DefaultConfig())
		},
	}

	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "Print the version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println(version.Full())
		},
	}

	rootCmd.AddCommand(versionCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
