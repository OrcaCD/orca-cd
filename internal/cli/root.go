package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "orca-cli",
	Short: "OrcaCD CLI",
	Long:  `The OrcaCD CLI provides administrative commands for account recovery and management.`,
}

func init() {
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(oneTimeAccessTokenCmd)
}

func Run() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
