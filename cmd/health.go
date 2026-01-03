package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

var healthCmd = &cobra.Command{
	Use:   "health",
	Short: "Check the health of the API server",
	Long:  `Performs a health check by querying the /api/health endpoint.`,
	Run: func(cmd *cobra.Command, args []string) {
		port, _ := cmd.Flags().GetString("port")
		url := fmt.Sprintf("http://localhost:%s/api/health", port)

		log.Info().Str("url", url).Msg("Checking server health")

		client := &http.Client{
			Timeout: 5 * time.Second,
		}

		resp, err := client.Get(url)
		if err != nil {
			log.Error().Err(err).Msg("Health check failed")
			os.Exit(1)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			log.Error().Int("status_code", resp.StatusCode).Msg("Health check failed")
			os.Exit(1)
		}

		var result map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			log.Error().Err(err).Msg("Failed to parse response")
			os.Exit(1)
		}

		log.Info().Msg("Server is healthy!")
		log.Info().Interface("status", result["status"]).Msg("Health status")
		if serverTime, ok := result["time"].(string); ok {
			parsedTime, err := time.Parse(time.RFC3339Nano, serverTime)
			if err == nil {
				log.Info().Str("server_time", parsedTime.Format(time.RFC1123)).Msg("Server time")
			} else {
				log.Info().Interface("server_time", result["time"]).Msg("Server time")
			}
		}
	},
}

func init() {
	healthCmd.Flags().StringP("port", "p", "8080", "Port where the server is running")
}
