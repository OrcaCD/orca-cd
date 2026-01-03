package cmd

import (
	"fmt"
	"github.com/OrcaCD/orca-cd/internal/server"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "github.com/OrcaCD/orca-cd",
	Short: "Go React Demo Application",
	Long:  `A full-stack demo application with Go backend and React frontend.`,
	Run: func(cmd *cobra.Command, args []string) {
		port, _ := cmd.Flags().GetString("port")
		server.Start(port)
	},
}

func init() {
	rootCmd.Flags().StringP("port", "p", "8080", "Port to run the server on")
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(healthCmd)
}

func Run() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
