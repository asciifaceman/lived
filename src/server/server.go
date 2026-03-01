package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/asciifaceman/lived/pkg/config"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

type Option func(*options)

type options struct {
	logger           *slog.Logger
	customMiddleware []echo.MiddlewareFunc
}

func WithLogger(logger *slog.Logger) Option {
	return func(opts *options) {
		if logger != nil {
			opts.logger = logger
		}
	}
}

func WithMiddleware(custom ...echo.MiddlewareFunc) Option {
	return func(opts *options) {
		opts.customMiddleware = append(opts.customMiddleware, custom...)
	}
}

func defaultOptions() options {
	return options{
		logger: slog.New(slog.NewJSONHandler(os.Stdout, nil)),
	}
}

func Run(ctx context.Context, cfg config.Config, database *gorm.DB, runOptions ...Option) error {
	opts := defaultOptions()
	for _, applyOption := range runOptions {
		applyOption(&opts)
	}

	e := echo.New()
	e.HideBanner = true
	e.HidePort = true
	e.Logger.SetOutput(os.Stdout)
	e.HTTPErrorHandler = makeHTTPErrorHandler(opts.logger)

	registerMiddleware(e, opts.logger, opts.customMiddleware)

	registerRoutes(e, database, cfg)
	opts.logger.Info("http_server_starting", "addr", cfg.HTTPAddr)

	errCh := make(chan error, 1)
	go func() {
		if err := e.Start(cfg.HTTPAddr); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		opts.logger.Info("http_server_shutting_down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := e.Shutdown(shutdownCtx); err != nil {
			opts.logger.Error("http_server_shutdown_failed", "error", err)
			return err
		}
		opts.logger.Info("http_server_stopped")
		return nil
	case err := <-errCh:
		opts.logger.Error("http_server_failed", "error", err)
		return err
	}
}
