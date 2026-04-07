package hub

import (
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/middleware"
	"github.com/OrcaCD/orca-cd/internal/hub/routes"
	"github.com/OrcaCD/orca-cd/internal/hub/websocket"
	"github.com/gin-gonic/gin"
)

func RegisterRoutes(router *gin.Engine, cfg Config) {
	// Configure package-level settings from config
	routes.LocalAuthDisabled = cfg.DisableLocalAuth
	routes.OIDCAppURL = cfg.AppURL

	api := router.Group("/api/v1")
	{
		// Public routes (no authentication required)
		api.GET("/health", routes.HealthHandler)
		api.GET("/auth/setup", routes.SetupHandler)

		// OIDC auth flow (public)
		api.GET("/auth/oidc/:id/authorize", routes.OIDCAuthorizeHandler)
		api.GET("/auth/oidc/:id/callback", routes.OIDCCallbackHandler)

		// Rate-limited auth endpoints: 10 req/min per IP, burst of 5
		authRateLimit := middleware.RateLimit(6*time.Second, 5)
		api.POST("/auth/register", authRateLimit, routes.RegisterHandler)
		api.POST("/auth/login", authRateLimit, routes.LoginHandler)

		// Protected routes (authentication required)
		protected := api.Group("", middleware.RequireAuth())
		{
			protected.GET("/auth/profile", routes.ProfileHandler)
			protected.POST("/auth/logout", routes.LogoutHandler)

			protected.GET("/repositories", routes.ListRepositoriesHandler)
			protected.POST("/repositories", routes.CreateRepositoryHandler)
			protected.POST("/repositories/test-connection", routes.TestConnectionHandler)
		}

		// Admin routes (authentication + admin role required)
		admin := api.Group("/admin", middleware.RequireAuth(), middleware.RequireAdmin())
		{
			admin.GET("/oidc-providers", routes.AdminListOIDCProvidersHandler)
			admin.POST("/oidc-providers", routes.AdminCreateOIDCProviderHandler)
			admin.GET("/oidc-providers/:id", routes.AdminGetOIDCProviderHandler)
			admin.PUT("/oidc-providers/:id", routes.AdminUpdateOIDCProviderHandler)
			admin.DELETE("/oidc-providers/:id", routes.AdminDeleteOIDCProviderHandler)
		}

		h := websocket.NewHub(&Log)
		w := websocket.NewWorker(h, &Log)
		w.Start()

		// Rate-limit reconnects: 20 req/min per IP, burst of 5
		wsRateLimit := middleware.RateLimit(3*time.Second, 5)
		api.GET("/ws", wsRateLimit, websocket.WsHandler(h, &Log))
	}

	if !cfg.Debug {
		router.Static("/assets", "./frontend/dist/assets")
		router.StaticFile("/", "./frontend/dist/index.html")
		router.NoRoute(func(c *gin.Context) {
			c.File("./frontend/dist/index.html")
		})
	}
}
