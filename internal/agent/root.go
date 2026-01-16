package agent

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "agent",
	Short: "OrcaCD Agent",
	Long:  `The OrcaCD Agent executes tasks assigned by the Hub and reports results back via gRPC streaming.`,
	Run: func(cmd *cobra.Command, args []string) {
		hubAddr, _ := cmd.Flags().GetString("hub")
		agentID, _ := cmd.Flags().GetString("id")
		StartAgent(hubAddr, agentID)
	},
}

func init() {
	rootCmd.Flags().StringP("hub", "H", "localhost:9090", "Hub gRPC address")
	rootCmd.Flags().StringP("id", "i", "", "Agent ID (auto-generated if empty)")
	rootCmd.AddCommand(versionCmd)
}

func Run() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
