package grpc

import (
	pb "github.com/OrcaCD/orca-cd/api/proto"
	"google.golang.org/grpc"
)

// RegisterHubServiceServer registers the HubService with a gRPC server
func RegisterHubServiceServer(s *grpc.Server, srv *HubService) {
	pb.RegisterHubServiceServer(s, srv)
}

// Global hub service instance for use by controllers
var DefaultHubService *HubService

func init() {
	DefaultHubService = NewHubService()
}
