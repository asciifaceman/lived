package app

import (
	"context"
	"errors"

	"github.com/asciifaceman/lived/pkg/config"
	"github.com/asciifaceman/lived/pkg/db"
	"github.com/asciifaceman/lived/pkg/migrations"
	"github.com/asciifaceman/lived/pkg/telemetry"
	"github.com/asciifaceman/lived/src/server"
	"go.opentelemetry.io/contrib/instrumentation/github.com/labstack/echo/otelecho"
	"golang.org/x/sync/errgroup"
	"gorm.io/plugin/opentelemetry/tracing"
)

func Run(ctx context.Context, cfg config.Config, serverOptions ...server.Option) error {
	if cfg.MMOOTelEnabled {
		shutdownTelemetry, err := telemetry.Setup(ctx, cfg)
		if err != nil {
			return err
		}
		defer func() {
			_ = shutdownTelemetry(context.Background())
		}()

		serviceName := cfg.OTELServiceName
		if serviceName == "" {
			serviceName = "lived"
		}
		serverOptions = append(serverOptions, server.WithMiddleware(otelecho.Middleware(serviceName)))
	}

	database, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}

	if cfg.MMOOTelEnabled {
		if err := database.Use(tracing.NewPlugin()); err != nil {
			return err
		}
	}

	if cfg.AutoMigrate {
		if err := migrations.Run(ctx, database); err != nil {
			return err
		}
	}

	g, runCtx := errgroup.WithContext(ctx)

	g.Go(func() error {
		return runWorldLoop(runCtx, cfg, database)
	})

	g.Go(func() error {
		return server.Run(runCtx, cfg, database, serverOptions...)
	})

	if err := g.Wait(); err != nil {
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	}

	return nil
}
