package app

import (
	"context"
	"errors"
	"time"

	"github.com/asciifaceman/lived/pkg/config"
	"github.com/asciifaceman/lived/pkg/dal"
	"github.com/asciifaceman/lived/src/gameplay"
	"gorm.io/gorm"
)

const (
	worldRuntimeStateKey = "world"
	tickDBTimeout        = 10 * time.Second
)

func runWorldLoop(ctx context.Context, cfg config.Config, database *gorm.DB) error {
	runtimeState, err := loadOrInitRuntimeState(ctx, database)
	if err != nil {
		return err
	}

	if runtimeState.LastProcessedTickAt.IsZero() {
		runtimeState.LastProcessedTickAt = time.Now().UTC()
	}

	startupTime := time.Now().UTC()
	if startupTime.After(runtimeState.LastProcessedTickAt) {
		if err := runTickAt(database, cfg, runtimeState, startupTime); err != nil {
			return err
		}
	}

	ticker := time.NewTicker(cfg.TickInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			shutdownTime := time.Now().UTC()
			if shutdownTime.After(runtimeState.LastProcessedTickAt) {
				if err := runTickAt(database, cfg, runtimeState, shutdownTime); err != nil {
					return err
				}
			}

			flushCtx, cancel := context.WithTimeout(context.Background(), tickDBTimeout)
			defer cancel()
			return persistRuntimeState(flushCtx, database, runtimeState)
		case tickTime := <-ticker.C:
			if err := runTickAt(database, cfg, runtimeState, tickTime.UTC()); err != nil {
				return err
			}
		}
	}
}

func runTickAt(database *gorm.DB, cfg config.Config, runtimeState *dal.WorldRuntimeState, tickTime time.Time) error {
	elapsed := tickTime.Sub(runtimeState.LastProcessedTickAt)
	if elapsed < 0 {
		elapsed = 0
	}

	gameMinutesFloat := cfg.GameMinutesRate*(elapsed.Minutes()) + runtimeState.CarryGameMinutes
	advanceByMinutes := int64(gameMinutesFloat)
	runtimeState.CarryGameMinutes = gameMinutesFloat - float64(advanceByMinutes)
	runtimeState.LastProcessedTickAt = tickTime

	opCtx, cancel := context.WithTimeout(context.Background(), tickDBTimeout)
	defer cancel()

	currentTick, err := advanceWorldAndPersistRuntime(opCtx, database, advanceByMinutes, runtimeState)
	if err != nil {
		return opCtxErrGuard(err)
	}

	if err := gameplay.ProcessWorldTick(opCtx, database, currentTick); err != nil {
		return opCtxErrGuard(err)
	}

	pendingSummary, err := gameplay.BuildPendingBehaviorSummaryJSON(opCtx, database)
	if err != nil {
		return opCtxErrGuard(err)
	}
	runtimeState.PendingBehaviorsJSON = pendingSummary

	return opCtxErrGuard(persistRuntimeState(opCtx, database, runtimeState))
}

func advanceWorldAndPersistRuntime(ctx context.Context, database *gorm.DB, advanceByMinutes int64, runtimeState *dal.WorldRuntimeState) (int64, error) {
	currentTick := int64(0)
	err := database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if advanceByMinutes > 0 {
			update := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Model(&dal.WorldState{}).Update("simulation_tick", gorm.Expr("simulation_tick + ?", advanceByMinutes))
			if update.Error != nil {
				return update.Error
			}

			if update.RowsAffected == 0 {
				world := dal.WorldState{SimulationTick: advanceByMinutes}
				if err := tx.Create(&world).Error; err != nil {
					return err
				}
			}
		}

		if err := tx.Model(&dal.WorldRuntimeState{}).
			Where("key = ?", worldRuntimeStateKey).
			Updates(map[string]any{
				"last_processed_tick_at": runtimeState.LastProcessedTickAt,
				"carry_game_minutes":     runtimeState.CarryGameMinutes,
				"pending_behaviors_json": runtimeState.PendingBehaviorsJSON,
			}).Error; err != nil {
			return err
		}

		worldState := dal.WorldState{}
		result := tx.Order("id ASC").First(&worldState)
		if result.Error != nil {
			if errors.Is(result.Error, gorm.ErrRecordNotFound) {
				currentTick = 0
				return nil
			}
			return result.Error
		}

		currentTick = worldState.SimulationTick
		return nil
	})

	if err != nil {
		return 0, err
	}

	return currentTick, nil
}

func loadOrInitRuntimeState(ctx context.Context, database *gorm.DB) (*dal.WorldRuntimeState, error) {
	runtimeState := &dal.WorldRuntimeState{}
	result := database.WithContext(ctx).Where("key = ?", worldRuntimeStateKey).Limit(1).Find(runtimeState)
	if result.Error != nil {
		return nil, result.Error
	}

	if result.RowsAffected > 0 {
		if runtimeState.PendingBehaviorsJSON == "" {
			runtimeState.PendingBehaviorsJSON = "[]"
		}
		return runtimeState, nil
	}

	initialState := &dal.WorldRuntimeState{
		Key:                  worldRuntimeStateKey,
		LastProcessedTickAt:  time.Now().UTC(),
		CarryGameMinutes:     0,
		PendingBehaviorsJSON: "[]",
	}

	if createErr := database.WithContext(ctx).Create(initialState).Error; createErr != nil {
		return nil, createErr
	}

	return initialState, nil
}

func persistRuntimeState(ctx context.Context, database *gorm.DB, runtimeState *dal.WorldRuntimeState) error {
	return database.WithContext(ctx).Model(&dal.WorldRuntimeState{}).
		Where("key = ?", worldRuntimeStateKey).
		Updates(map[string]any{
			"last_processed_tick_at": runtimeState.LastProcessedTickAt,
			"carry_game_minutes":     runtimeState.CarryGameMinutes,
			"pending_behaviors_json": runtimeState.PendingBehaviorsJSON,
		}).Error
}

func opCtxErrGuard(err error) error {
	if errors.Is(err, context.Canceled) {
		return nil
	}

	return err
}
