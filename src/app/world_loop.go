package app

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/asciifaceman/lived/pkg/config"
	"github.com/asciifaceman/lived/pkg/dal"
	"github.com/asciifaceman/lived/pkg/telemetry"
	"github.com/asciifaceman/lived/src/gameplay"
	"github.com/jackc/pgx/v5/pgconn"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"gorm.io/gorm"
)

const (
	worldRuntimeStateKey = "world"
	tickDBTimeout        = 10 * time.Second
	defaultRealmID       = uint(1)
)

var worldLoopTracer = otel.Tracer("lived/world-loop")

func runWorldLoop(ctx context.Context, cfg config.Config, database *gorm.DB) error {
	startupTime := time.Now().UTC()
	if err := runTickAtForKnownRealms(ctx, database, cfg, startupTime); err != nil {
		return err
	}

	ticker := time.NewTicker(cfg.TickInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			shutdownTime := time.Now().UTC()
			shutdownCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			_ = runTickAtForKnownRealms(shutdownCtx, database, cfg, shutdownTime)
			cancel()
			return nil
		case tickTime := <-ticker.C:
			if err := runTickAtForKnownRealms(ctx, database, cfg, tickTime.UTC()); err != nil {
				return err
			}
		}
	}
}

func runTickAtForKnownRealms(ctx context.Context, database *gorm.DB, cfg config.Config, tickTime time.Time) error {
	spanCtx, span := worldLoopTracer.Start(ctx, "world.tick.discover_realms")
	defer span.End()

	discoveryCtx, cancel := context.WithTimeout(ctx, tickDBTimeout)
	defer cancel()

	realmIDs, err := listKnownRealmIDs(discoveryCtx, database)
	if err != nil {
		return opCtxErrGuard(err)
	}

	for _, realmID := range realmIDs {
		realmCtx, realmSpan := worldLoopTracer.Start(spanCtx, "world.tick.realm")
		realmSpan.SetAttributes(attribute.Int64("realm.id", int64(realmID)))
		err := runTickAtWithContext(realmCtx, database, cfg, tickTime, realmID)
		realmSpan.End()
		if err != nil {
			return err
		}
	}

	return nil
}

func runTickAt(database *gorm.DB, cfg config.Config, tickTime time.Time, realmID uint) error {
	return runTickAtWithContext(context.Background(), database, cfg, tickTime, realmID)
}

func runTickAtWithContext(ctx context.Context, database *gorm.DB, cfg config.Config, tickTime time.Time, realmID uint) error {
	_, span := worldLoopTracer.Start(ctx, "world.tick.realm.process")
	span.SetAttributes(attribute.Int64("realm.id", int64(realmID)))
	defer span.End()

	startedAt := time.Now()
	advanceByMinutes := int64(0)
	failed := false
	defer func() {
		telemetry.RecordWorldTick(ctx, realmID, advanceByMinutes, time.Since(startedAt), failed)
	}()

	opCtx, cancel := context.WithTimeout(ctx, tickDBTimeout)
	defer cancel()

	runtimeState, err := loadOrInitRuntimeState(opCtx, database, realmID)
	if err != nil {
		failed = true
		return opCtxErrGuard(err)
	}

	if runtimeState.LastProcessedTickAt.IsZero() {
		runtimeState.LastProcessedTickAt = tickTime
	}

	elapsed := tickTime.Sub(runtimeState.LastProcessedTickAt)
	if elapsed < 0 {
		elapsed = 0
	}

	gameMinutesFloat := cfg.GameMinutesRate*(elapsed.Minutes()) + runtimeState.CarryGameMinutes
	advanceByMinutes = int64(gameMinutesFloat)
	span.SetAttributes(attribute.Int64("tick.advance_minutes", advanceByMinutes))
	runtimeState.CarryGameMinutes = gameMinutesFloat - float64(advanceByMinutes)
	runtimeState.LastProcessedTickAt = tickTime

	currentTick, err := advanceWorldAndPersistRuntime(opCtx, database, advanceByMinutes, runtimeState, realmID)
	if err != nil {
		failed = true
		return opCtxErrGuard(err)
	}

	if err := gameplay.ProcessWorldTickForRealm(opCtx, database, currentTick, realmID); err != nil {
		failed = true
		return opCtxErrGuard(err)
	}

	pendingSummary, err := gameplay.BuildPendingBehaviorSummaryJSONForRealm(opCtx, database, realmID)
	if err != nil {
		failed = true
		return opCtxErrGuard(err)
	}
	runtimeState.PendingBehaviorsJSON = pendingSummary

	err = persistRuntimeState(opCtx, database, runtimeState, realmID)
	if err != nil {
		failed = true
	}
	return opCtxErrGuard(err)
}

func listKnownRealmIDs(ctx context.Context, database *gorm.DB) ([]uint, error) {
	realmSet := map[uint]struct{}{defaultRealmID: {}}

	characterRows := make([]struct {
		RealmID uint
	}, 0)
	if err := database.WithContext(ctx).
		Model(&dal.Character{}).
		Distinct("realm_id").
		Where("realm_id > 0").
		Find(&characterRows).Error; err != nil {
		return nil, err
	}
	for _, row := range characterRows {
		realmSet[row.RealmID] = struct{}{}
	}

	worldRows := make([]struct {
		RealmID uint
	}, 0)
	if err := database.WithContext(ctx).
		Model(&dal.WorldState{}).
		Distinct("realm_id").
		Where("realm_id > 0").
		Find(&worldRows).Error; err != nil {
		return nil, err
	}
	for _, row := range worldRows {
		realmSet[row.RealmID] = struct{}{}
	}

	runtimeRows := make([]struct {
		RealmID uint
	}, 0)
	if err := database.WithContext(ctx).
		Model(&dal.WorldRuntimeState{}).
		Distinct("realm_id").
		Where("realm_id > 0").
		Find(&runtimeRows).Error; err != nil {
		return nil, err
	}
	for _, row := range runtimeRows {
		realmSet[row.RealmID] = struct{}{}
	}

	realmIDs := make([]uint, 0, len(realmSet))
	for realmID := range realmSet {
		realmIDs = append(realmIDs, realmID)
	}
	sort.Slice(realmIDs, func(i, j int) bool { return realmIDs[i] < realmIDs[j] })

	return realmIDs, nil
}

func advanceWorldAndPersistRuntime(ctx context.Context, database *gorm.DB, advanceByMinutes int64, runtimeState *dal.WorldRuntimeState, realmID uint) (int64, error) {
	currentTick := int64(0)
	err := database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if advanceByMinutes > 0 {
			update := tx.Model(&dal.WorldState{}).
				Where("realm_id = ?", realmID).
				Update("simulation_tick", gorm.Expr("simulation_tick + ?", advanceByMinutes))
			if update.Error != nil {
				return update.Error
			}

			if update.RowsAffected == 0 {
				world := dal.WorldState{RealmID: realmID, SimulationTick: advanceByMinutes}
				if err := tx.Create(&world).Error; err != nil {
					return err
				}
			}
		}

		if err := tx.Model(&dal.WorldRuntimeState{}).
			Where("realm_id = ? AND key = ?", realmID, worldRuntimeStateKey).
			Updates(map[string]any{
				"last_processed_tick_at": runtimeState.LastProcessedTickAt,
				"carry_game_minutes":     runtimeState.CarryGameMinutes,
				"pending_behaviors_json": runtimeState.PendingBehaviorsJSON,
			}).Error; err != nil {
			return err
		}

		worldState := dal.WorldState{}
		result := tx.Where("realm_id = ?", realmID).Order("id ASC").First(&worldState)
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

func loadOrInitRuntimeState(ctx context.Context, database *gorm.DB, realmID uint) (*dal.WorldRuntimeState, error) {
	runtimeState := &dal.WorldRuntimeState{}
	result := database.WithContext(ctx).Where("realm_id = ? AND key = ?", realmID, worldRuntimeStateKey).Limit(1).Find(runtimeState)
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
		RealmID:              realmID,
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

func persistRuntimeState(ctx context.Context, database *gorm.DB, runtimeState *dal.WorldRuntimeState, realmID uint) error {
	return database.WithContext(ctx).Model(&dal.WorldRuntimeState{}).
		Where("realm_id = ? AND key = ?", realmID, worldRuntimeStateKey).
		Updates(map[string]any{
			"last_processed_tick_at": runtimeState.LastProcessedTickAt,
			"carry_game_minutes":     runtimeState.CarryGameMinutes,
			"pending_behaviors_json": runtimeState.PendingBehaviorsJSON,
		}).Error
}

func opCtxErrGuard(err error) error {
	if err == nil {
		return nil
	}

	if errors.Is(err, context.Canceled) {
		return nil
	}

	if isTransientPostgresAdminShutdownError(err) {
		return nil
	}

	return err
}

func isTransientPostgresAdminShutdownError(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "57P01", "57P02", "57P03":
			return true
		}
	}

	message := strings.ToLower(err.Error())
	return strings.Contains(message, "sqlstate 57p01") ||
		strings.Contains(message, "sqlstate 57p02") ||
		strings.Contains(message, "sqlstate 57p03")
}
