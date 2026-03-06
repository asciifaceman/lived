package server

import (
	"github.com/asciifaceman/lived/pkg/config"
	"github.com/asciifaceman/lived/pkg/version"
	serverAdmin "github.com/asciifaceman/lived/src/server/admin"
	serverAuth "github.com/asciifaceman/lived/src/server/auth"
	serverChat "github.com/asciifaceman/lived/src/server/chat"
	serverFeed "github.com/asciifaceman/lived/src/server/feed"
	serverMMO "github.com/asciifaceman/lived/src/server/mmo"
	serverOnboarding "github.com/asciifaceman/lived/src/server/onboarding"
	serverPlayer "github.com/asciifaceman/lived/src/server/player"
	serverStream "github.com/asciifaceman/lived/src/server/stream"
	serverSystem "github.com/asciifaceman/lived/src/server/system"
	"github.com/labstack/echo/v4"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"gorm.io/gorm"
)

func registerRoutes(e *echo.Echo, database *gorm.DB, cfg config.Config) {
	e.GET("/health", func(c echo.Context) error {
		payload := map[string]any{"service": "lived", "database": "unknown"}

		sqlDB, err := database.DB()
		if err == nil {
			if pingErr := sqlDB.PingContext(c.Request().Context()); pingErr == nil {
				payload["database"] = "ok"
			} else {
				payload["database"] = "degraded"
			}
		}

		return respondSuccess(c, 200, "Lived service heartbeat is steady.", payload)
	})

	e.GET("/metrics", echo.WrapHandler(promhttp.Handler()))

	v1 := e.Group("/v1")
	if cfg.MMOAdminEnabled {
		serverAdmin.RegisterRoutes(v1.Group("/admin"), database, cfg)
	}
	if cfg.MMOAuthEnabled {
		serverAuth.RegisterRoutes(v1.Group("/auth"), database, cfg)
		serverOnboarding.RegisterRoutes(v1.Group("/onboarding"), database, cfg)
	}
	if cfg.MMOChatEnabled {
		serverFeed.RegisterRoutes(v1.Group("/feed"), database, cfg)
		serverChat.RegisterRoutes(v1.Group("/chat"), database, cfg)
	}
	if cfg.MMORealmScopingEnabled {
		serverMMO.RegisterRoutes(v1.Group("/mmo"), database, cfg)
	}
	serverSystem.RegisterRoutes(v1.Group("/system"), database, cfg)
	serverPlayer.RegisterRoutes(v1.Group("/player"), database, cfg)
	serverStream.RegisterRoutes(v1.Group("/stream"), database, cfg)
	v1.GET("", func(c echo.Context) error {
		return respondSuccess(c, 200, "API gateway is listening.", map[string]any{"version": version.APIVersion, "backend": version.BackendVersion})
	})

	e.GET("/swagger", swaggerUIRedirectHandler)
	e.GET("/swagger/", swaggerUIHandler)
	e.GET("/swagger/openapi.json", swaggerSpecHandler)

	registerFrontendRoutes(e, cfg)
}
