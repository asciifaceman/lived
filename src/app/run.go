package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/asciifaceman/lived/pkg/config"
	"github.com/asciifaceman/lived/pkg/db"
	"github.com/asciifaceman/lived/pkg/migrations"
	"github.com/asciifaceman/lived/pkg/telemetry"
	"github.com/asciifaceman/lived/src/server"
	serverChat "github.com/asciifaceman/lived/src/server/chat"
	"github.com/jackc/pgx/v5/pgconn"
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
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_ = shutdownTelemetry(shutdownCtx)
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

	if sqlDB, sqlErr := database.DB(); sqlErr == nil {
		defer func() {
			_ = sqlDB.Close()
		}()
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

	if cfg.MMOChatEnabled {
		if err := serverChat.EnsureGlobalChannelBootstrap(ctx, database); err != nil {
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
		if errors.Is(err, context.Canceled) || isExpectedDBResetInterruption(err) {
			return nil
		}
		return err
	}

	return nil
}

func isExpectedDBResetInterruption(err error) bool {
	if err == nil {
		return false
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "57P01", "57P02", "57P03", "42P01", "3D000":
			return true
		}
	}

	message := strings.ToLower(err.Error())
	return strings.Contains(message, "sqlstate 57p01") ||
		strings.Contains(message, "sqlstate 57p02") ||
		strings.Contains(message, "sqlstate 57p03") ||
		strings.Contains(message, "sqlstate 42p01") ||
		strings.Contains(message, "sqlstate 3d000")
}
