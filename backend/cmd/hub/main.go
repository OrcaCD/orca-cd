package main

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"github.com/OrcaCD/orca-cd/internal/hub"
	"github.com/OrcaCD/orca-cd/internal/shared/httpclient"
	"github.com/OrcaCD/orca-cd/internal/version"
	"github.com/spf13/cobra"
)

func main() {
	rootCmd := &cobra.Command{
		Use:                "hub [flags]",
		Short:              "Orca Hub",
		DisableFlagParsing: true,
		SilenceUsage:       true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := hub.DefaultConfig()
			if err != nil {
				return err
			}
			return hub.Run(cfg)
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
		Short: "Check the health of the hub",
		Run: func(cmd *cobra.Command, args []string) {
			cfg, err := hub.DefaultConfig()
			if err != nil {
				fmt.Fprintf(os.Stderr, "invalid configuration: %v\n", err)
				os.Exit(1)
			}

			resp, err := httpclient.Get(context.Background(), "http://localhost:"+cfg.Port+"/api/v1/health")
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
