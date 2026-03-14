package main

import (
	"fmt"
	"net/http"
	"os"

	"github.com/OrcaCD/orca-cd/internal/hub"
	"github.com/OrcaCD/orca-cd/internal/version"
	"github.com/spf13/cobra"
)

func main() {
	rootCmd := &cobra.Command{
		Use:                "hub [flags]",
		Short:              "Orca Agent",
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return hub.Run(hub.DefaultConfig())
		},
	}

	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "Print the version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println(version.Full())
		},
	}

	healthCheckCmd := &cobra.Command{
		Use:   "healthcheck",
		Short: "Run a health check",
		Run: func(cmd *cobra.Command, args []string) {
			resp, err := http.Get("http://localhost:8080/api/v1/health")
			if err != nil {
				fmt.Fprintf(os.Stderr, "health check failed: %v\n", err)
				os.Exit(1)
			}
			defer func() {
				if closeErr := resp.Body.Close(); closeErr != nil {
					fmt.Fprintf(os.Stderr, "failed to close response body: %v\n", closeErr)
				}
			}()
			if resp.StatusCode == http.StatusOK {
				fmt.Println("ok")
			} else {
				fmt.Fprintf(os.Stderr, "health check failed: status %d\n", resp.StatusCode)
				os.Exit(1)
			}
		},
	}

	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(healthCheckCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
