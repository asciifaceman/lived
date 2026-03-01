package server

import (
	"github.com/asciifaceman/lived/pkg/config"
	"github.com/asciifaceman/lived/pkg/version"
	serverPlayer "github.com/asciifaceman/lived/src/server/player"
	serverStream "github.com/asciifaceman/lived/src/server/stream"
	serverSystem "github.com/asciifaceman/lived/src/server/system"
	"github.com/labstack/echo/v4"
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

	v1 := e.Group("/v1")
	serverSystem.RegisterRoutes(v1.Group("/system"), database, cfg)
	serverPlayer.RegisterRoutes(v1.Group("/player"), database)
	serverStream.RegisterRoutes(v1.Group("/stream"), database, cfg)
	v1.GET("", func(c echo.Context) error {
		return respondSuccess(c, 200, "API gateway is listening.", map[string]any{"version": version.APIVersion, "backend": version.BackendVersion})
	})

	e.GET("/swagger", swaggerUIRedirectHandler)
	e.GET("/swagger/", swaggerUIHandler)
	e.GET("/swagger/openapi.json", swaggerSpecHandler)

	registerFrontendRoutes(e, cfg)
}
