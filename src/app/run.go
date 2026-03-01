package app

import (
	"context"
	"errors"

	"github.com/asciifaceman/lived/pkg/config"
	"github.com/asciifaceman/lived/pkg/db"
	"github.com/asciifaceman/lived/pkg/migrations"
	"github.com/asciifaceman/lived/src/server"
	"golang.org/x/sync/errgroup"
)

func Run(ctx context.Context, cfg config.Config, serverOptions ...server.Option) error {
	database, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
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
