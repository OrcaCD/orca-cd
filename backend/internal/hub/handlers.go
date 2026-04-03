package hub

import (
	"github.com/gin-gonic/gin"

	"github.com/OrcaCD/orca-cd/internal/hub/middleware"
	"github.com/OrcaCD/orca-cd/internal/hub/routes"
)

func RegisterRoutes(router *gin.Engine, cfg Config) {
	api := router.Group("/api/v1")
	{
		// Public routes — no authentication required
		api.GET("/health", routes.HealthHandler)
		api.GET("/auth/setup", routes.SetupHandler)
		api.POST("/auth/register", routes.RegisterHandler)
		api.POST("/auth/login", routes.LoginHandler)

		// Protected routes — authentication required
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
