package hub

import (
	"github.com/gin-gonic/gin"

	"github.com/OrcaCD/orca-cd/internal/hub/routes"
)

func RegisterRoutes(router *gin.Engine, cfg Config) {
	api := router.Group("/api/v1")
	{
		api.GET("/health", routes.HealthHandler)
	}

	if !cfg.Debug {
		router.Static("/assets", "./frontend/dist/assets")
		router.StaticFile("/", "./frontend/dist/index.html")
		router.NoRoute(func(c *gin.Context) {
			c.File("./frontend/dist/index.html")
		})
	}
}
