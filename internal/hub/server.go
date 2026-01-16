package hub

import (
	"fmt"
	"net"
	"sync"

	"github.com/OrcaCD/orca-cd/internal/controllers"
	"github.com/OrcaCD/orca-cd/internal/database"
	hubgrpc "github.com/OrcaCD/orca-cd/internal/hub/grpc"
	"github.com/OrcaCD/orca-cd/internal/middleware"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc"
)

func StartHub(httpPort, grpcPort string) {
	// Initialize database
	database.Connect()

	var wg sync.WaitGroup
	wg.Add(2)

	// Start gRPC server
	go func() {
		defer wg.Done()
		startGRPCServer(grpcPort)
	}()

	// Start HTTP server
	go func() {
		defer wg.Done()
		startHTTPServer(httpPort)
	}()

	wg.Wait()
}

func startGRPCServer(port string) {
	addr := fmt.Sprintf(":%s", port)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to listen for gRPC")
	}

	grpcServer := grpc.NewServer()
	hubgrpc.RegisterHubServiceServer(grpcServer, hubgrpc.DefaultHubService)

	log.Info().Str("addr", addr).Msg("Starting gRPC server")
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatal().Err(err).Msg("Failed to serve gRPC")
	}
}

func startHTTPServer(port string) {
	// Setup Gin router
	r := gin.Default()

	// Apply middleware
	r.Use(middleware.CORS())

	// API routes
	api := r.Group("/api")
	{
		api.GET("/health", controllers.HealthCheck)
		api.GET("/messages", controllers.GetMessages)
		api.POST("/messages", controllers.CreateMessage)
		
		// Agent management endpoints
		agents := api.Group("/agents")
		{
			agents.GET("", controllers.GetAgents)
			agents.GET("/:id", controllers.GetAgent)
		}
	}

	// Serve static files from the frontend build
	r.Static("/assets", "./frontend/dist/assets")
	r.StaticFile("/", "./frontend/dist/index.html")
	r.NoRoute(func(c *gin.Context) {
		c.File("./frontend/dist/index.html")
	})

	// Start server
	addr := fmt.Sprintf(":%s", port)
	log.Info().Str("addr", addr).Msg("Starting HTTP server")
	r.Run(addr)
}
