package hub

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "hub",
	Short: "OrcaCD Hub Server",
	Long:  `The OrcaCD Hub is the central server that manages agents, serves the frontend, and provides the REST API.`,
	Run: func(cmd *cobra.Command, args []string) {
		port, _ := cmd.Flags().GetString("port")
		grpcPort, _ := cmd.Flags().GetString("grpc-port")
		StartHub(port, grpcPort)
	},
}

func init() {
	rootCmd.Flags().StringP("port", "p", "8080", "HTTP port for REST API and frontend")
	rootCmd.Flags().StringP("grpc-port", "g", "9090", "gRPC port for agent communication")
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(healthCmd)
}

func Run() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
