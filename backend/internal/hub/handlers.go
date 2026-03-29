package hub

import (
	"github.com/gin-gonic/gin"

	"github.com/OrcaCD/orca-cd/internal/hub/routes"
	"github.com/OrcaCD/orca-cd/internal/hub/websocket"
)

func RegisterRoutes(router *gin.Engine, cfg Config) {
	api := router.Group("/api/v1")
	{
		api.GET("/health", routes.HealthHandler)
	}

	h := websocket.NewHub(&Log)
	w := websocket.NewWorker(h, &Log)
	w.Start()
	router.GET("/ws/:id", websocket.WsHandler(h, &Log))

	if !cfg.Debug {
		router.Static("/assets", "./frontend/dist/assets")
		router.StaticFile("/", "./frontend/dist/index.html")
		router.NoRoute(func(c *gin.Context) {
			c.File("./frontend/dist/index.html")
		})
	}
}
