package hub

import (
	"fmt"
	"net/http"
	"os"

	"github.com/spf13/cobra"
)

var healthCmd = &cobra.Command{
	Use:   "health",
	Short: "Check the health of the hub server",
	Long:  `Check the health status of the running hub server.`,
	Run: func(cmd *cobra.Command, args []string) {
		port := os.Getenv("PORT")
		if port == "" {
			port = "8080"
		}
		
		resp, err := http.Get(fmt.Sprintf("http://localhost:%s/api/health", port))
		if err != nil {
			fmt.Printf("Health check failed: %v\n", err)
			os.Exit(1)
		}
		defer resp.Body.Close()
		
		if resp.StatusCode == http.StatusOK {
			fmt.Println("Hub is healthy")
			os.Exit(0)
		}
		fmt.Printf("Hub is unhealthy: status %d\n", resp.StatusCode)
		os.Exit(1)
	},
}
