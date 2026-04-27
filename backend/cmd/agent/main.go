package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/OrcaCD/orca-cd/internal/agent"
	"github.com/OrcaCD/orca-cd/internal/shared/httpclient"
	"github.com/OrcaCD/orca-cd/internal/version"
	"github.com/spf13/cobra"
)

func main() {
	rootCmd := &cobra.Command{
		Use:                "agent [flags]",
		Short:              "Orca Agent",
		DisableFlagParsing: true,
		SilenceUsage:       true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := agent.DefaultConfig()
			if err != nil {
				var fatalErr *agent.FatalConfigError
				if errors.As(err, &fatalErr) {
					agent.Log.Error().Msg(err.Error())
					quit := make(chan os.Signal, 1)
					signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
					<-quit
					return nil
				}
				return err
			}
			return agent.Run(cfg)
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
		Short: "Check the health of the agent",
		Run: func(cmd *cobra.Command, args []string) {
			cfg, err := agent.DefaultConfig()
			if err != nil {
				fmt.Fprintf(os.Stderr, "invalid configuration: %v\n", err)
				os.Exit(1)
			}

			req, err := httpclient.NewRequest(context.Background(), http.MethodGet, fmt.Sprintf("http://127.0.0.1:%s/api/v1/health", cfg.HealthPort), nil)
			if err != nil {
				fmt.Fprintf(os.Stderr, "health check failed: %v\n", err)
				os.Exit(1)
			}
			resp, err := httpclient.UnsafeClient.Do(req)
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
