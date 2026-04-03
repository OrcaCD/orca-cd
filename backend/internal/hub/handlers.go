package hub

import (
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/middleware"
	"github.com/OrcaCD/orca-cd/internal/hub/routes"
	"github.com/gin-gonic/gin"
)

func RegisterRoutes(router *gin.Engine, cfg Config) {
	api := router.Group("/api/v1")
	{
		// Public routes (no authentication required)
		api.GET("/health", routes.HealthHandler)
		api.GET("/auth/setup", routes.SetupHandler)

		// Rate-limited auth endpoints: 10 req/min per IP, burst of 5
		authRateLimit := middleware.RateLimit(6*time.Second, 5)
		api.POST("/auth/register", authRateLimit, routes.RegisterHandler)
		api.POST("/auth/login", authRateLimit, routes.LoginHandler)

		// Protected routes (authentication required)
		protected := api.Group("", middleware.RequireAuth())
		{
			protected.GET("/auth/profile", routes.ProfileHandler)
		}
	}

	if !cfg.Debug {
		router.Static("/assets", "./frontend/dist/assets")
		router.StaticFile("/", "./frontend/dist/index.html")
		router.NoRoute(func(c *gin.Context) {
			c.File("./frontend/dist/index.html")
		})
	}
}
