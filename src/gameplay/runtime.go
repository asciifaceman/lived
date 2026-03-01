package gameplay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/asciifaceman/lived/pkg/dal"
	"gorm.io/gorm"
)

const (
	behaviorQueued    = "queued"
	behaviorActive    = "active"
	behaviorCompleted = "completed"
	behaviorFailed    = "failed"

	worldRuntimeStateKey = "world"
	ascensionKey         = "global"

	marketOpenMinute  = 8 * 60
	marketCloseMinute = 20 * 60
	minutesPerDay     = 24 * 60

	ascensionBaseMinCoins      int64   = 250
	ascensionRequirementGrowth float64 = 1.75
	ascensionStartCoinsPerRun  int64   = 50

	defaultMarketWaitDurationMinutes int64 = 24 * 60
	maxMarketWaitDurationMinutes     int64 = 14 * 24 * 60

	statStrength = "strength"
	statSocial   = "social"

	statStamina              = "stamina"
	statMaxStamina           = "max_stamina"
	statStaminaRecoveryRate  = "stamina_recovery_rate"
	statStaminaRecoveryCarry = "stamina_recovery_carry"
	statStaminaTrainingXP    = "stamina_training_xp"

	defaultMaxStamina          int64 = 100
	defaultStaminaRecoveryRate int64 = 8
	recoveryXPPerRatePoint     int64 = 120
)

var ErrAscensionNotEligible = errors.New("ascension is not yet available")

type BehaviorView struct {
	ID                        uint   `json:"id"`
	Key                       string `json:"key"`
	ActorType                 string `json:"actorType"`
	ActorID                   uint   `json:"actorId"`
	State                     string `json:"state"`
	ScheduledAt               int64  `json:"scheduledAtTick"`
	StartedAt                 int64  `json:"startedAtTick"`
	CompletesAt               int64  `json:"completesAtTick"`
	DurationMinute            int64  `json:"durationMinutes"`
	MarketWaitDurationMinutes int64  `json:"marketWaitDurationMinutes,omitempty"`
	MarketWaitUntilTick       int64  `json:"marketWaitUntilTick,omitempty"`
	ResultMessage             string `json:"resultMessage"`
	FailureReason             string `json:"failureReason"`
}

type RecentEventView struct {
	ID          uint   `json:"id"`
	Tick        int64  `json:"tick"`
	Day         int64  `json:"day"`
	MinuteOfDay int64  `json:"minuteOfDay"`
	Clock       string `json:"clock"`
	Message     string `json:"message"`
	EventType   string `json:"eventType"`
}

type WorldSnapshot struct {
	Inventory      map[string]int64     `json:"inventory"`
	Stats          map[string]int64     `json:"stats"`
	MarketPrices   map[string]int64     `json:"marketPrices"`
	Behaviors      []BehaviorView       `json:"behaviors"`
	RecentEvents   []RecentEventView    `json:"recentEvents"`
	AscensionCount int64                `json:"ascensionCount"`
	WealthBonusPct float64              `json:"wealthBonusPct"`
	Ascension      AscensionEligibility `json:"ascension"`
}

type AscensionEligibility struct {
	Available        bool   `json:"available"`
	RequirementCoins int64  `json:"requirementCoins"`
	CurrentCoins     int64  `json:"currentCoins"`
	Reason           string `json:"reason"`
}

type MarketTickerView struct {
	Symbol       string `json:"symbol"`
	Price        int64  `json:"price"`
	Delta        int64  `json:"delta"`
	LastSource   string `json:"lastSource"`
	UpdatedTick  int64  `json:"updatedTick"`
	SessionState string `json:"sessionState"`
}

type MarketStatus struct {
	Tick           int64              `json:"tick"`
	Day            int64              `json:"day"`
	MinuteOfDay    int64              `json:"minuteOfDay"`
	IsOpen         bool               `json:"isOpen"`
	SessionState   string             `json:"sessionState"`
	MinutesToOpen  int64              `json:"minutesToOpen"`
	MinutesToClose int64              `json:"minutesToClose"`
	Tickers        []MarketTickerView `json:"tickers"`
}

type MarketHistoryEntry struct {
	Symbol       string `json:"symbol"`
	Tick         int64  `json:"tick"`
	Price        int64  `json:"price"`
	Delta        int64  `json:"delta"`
	Source       string `json:"source"`
	SessionState string `json:"sessionState"`
}

type QueueBehaviorOptions struct {
	MarketWaitDurationMinutes int64
	RealmID                   uint
}

type behaviorRuntimePayload struct {
	MarketWaitDurationMinutes int64 `json:"marketWaitDurationMinutes,omitempty"`
	MarketWaitUntilTick       int64 `json:"marketWaitUntilTick,omitempty"`
}

func QueuePlayerBehavior(ctx context.Context, database *gorm.DB, playerID uint, behaviorKey string, currentTick int64, options QueueBehaviorOptions) error {
	if err := ValidatePlayerBehaviorKey(behaviorKey); err != nil {
		return err
	}

	definition, _ := GetBehaviorDefinition(behaviorKey)

	payload := behaviorRuntimePayload{}
	if definition.RequiresMarketOpen {
		waitDuration := options.MarketWaitDurationMinutes
		if waitDuration <= 0 {
			waitDuration = defaultMarketWaitDurationMinutes
		}
		if waitDuration > maxMarketWaitDurationMinutes {
			waitDuration = maxMarketWaitDurationMinutes
		}

		payload.MarketWaitDurationMinutes = waitDuration
		payload.MarketWaitUntilTick = currentTick + waitDuration
	}

	payloadJSON, err := marshalBehaviorRuntimePayload(payload)
	if err != nil {
		return err
	}

	instance := dal.BehaviorInstance{
		RealmID:         options.RealmID,
		Key:             behaviorKey,
		ActorType:       ActorPlayer,
		ActorID:         playerID,
		State:           behaviorQueued,
		ScheduledAtTick: currentTick,
		DurationMinutes: definition.DurationMinutes,
		PayloadJSON:     payloadJSON,
	}
	if instance.RealmID == 0 {
		instance.RealmID = 1
	}

	return database.WithContext(ctx).Create(&instance).Error
}

func DefaultMarketWaitDurationMinutes() int64 {
	return defaultMarketWaitDurationMinutes
}

func MaxMarketWaitDurationMinutes() int64 {
	return maxMarketWaitDurationMinutes
}

func normalizeRealmID(realmID uint) uint {
	if realmID == 0 {
		return 1
	}
	return realmID
}

func EnsureRecurringWorldBehavior(ctx context.Context, database *gorm.DB, currentTick int64, realmID uint) error {
	realmID = normalizeRealmID(realmID)
	for key, definition := range worldBehaviorDefinitions {
		if definition.RepeatIntervalMin <= 0 {
			continue
		}

		var count int64
		err := database.WithContext(ctx).
			Model(&dal.BehaviorInstance{}).
			Where("realm_id = ? AND key = ? AND actor_type = ? AND state IN ?", realmID, key, ActorWorld, []string{behaviorQueued, behaviorActive}).
			Count(&count).Error
		if err != nil {
			return err
		}

		if count > 0 {
			continue
		}

		instance := dal.BehaviorInstance{
			RealmID:           realmID,
			Key:               key,
			ActorType:         ActorWorld,
			ActorID:           0,
			State:             behaviorQueued,
			ScheduledAtTick:   currentTick,
			DurationMinutes:   definition.DurationMinutes,
			RepeatIntervalMin: definition.RepeatIntervalMin,
			PayloadJSON:       "{}",
		}

		if err := database.WithContext(ctx).Create(&instance).Error; err != nil {
			return err
		}
	}

	return nil
}

func ProcessWorldTick(ctx context.Context, database *gorm.DB, currentTick int64) error {
	return ProcessWorldTickForRealm(ctx, database, currentTick, 1)
}

func ProcessWorldTickForRealm(ctx context.Context, database *gorm.DB, currentTick int64, realmID uint) error {
	realmID = normalizeRealmID(realmID)
	return database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := ensureMarketDefaults(ctx, tx, currentTick, realmID); err != nil {
			return err
		}

		if err := recoverPlayerStaminaTick(ctx, tx, realmID); err != nil {
			return err
		}

		if err := processActivations(ctx, tx, currentTick, realmID); err != nil {
			return err
		}
		if err := processCompletions(ctx, tx, currentTick, realmID); err != nil {
			return err
		}
		return EnsureRecurringWorldBehavior(ctx, tx, currentTick, realmID)
	})
}

func BuildPendingBehaviorSummaryJSON(ctx context.Context, database *gorm.DB) (string, error) {
	return BuildPendingBehaviorSummaryJSONForRealm(ctx, database, 1)
}

func BuildPendingBehaviorSummaryJSONForRealm(ctx context.Context, database *gorm.DB, realmID uint) (string, error) {
	realmID = normalizeRealmID(realmID)
	behaviors := make([]dal.BehaviorInstance, 0)
	err := database.WithContext(ctx).
		Where("realm_id = ? AND state IN ?", realmID, []string{behaviorQueued, behaviorActive}).
		Order("scheduled_at_tick ASC, id ASC").
		Find(&behaviors).Error
	if err != nil {
		return "", err
	}

	type summaryItem struct {
		ID          uint   `json:"id"`
		Key         string `json:"key"`
		State       string `json:"state"`
		ScheduledAt int64  `json:"scheduledAtTick"`
		CompletesAt int64  `json:"completesAtTick"`
		ActorType   string `json:"actorType"`
		ActorID     uint   `json:"actorId"`
	}

	items := make([]summaryItem, 0, len(behaviors))
	for _, behavior := range behaviors {
		items = append(items, summaryItem{
			ID:          behavior.ID,
			Key:         behavior.Key,
			State:       behavior.State,
			ScheduledAt: behavior.ScheduledAtTick,
			CompletesAt: behavior.CompletesAtTick,
			ActorType:   behavior.ActorType,
			ActorID:     behavior.ActorID,
		})
	}

	encoded, err := json.Marshal(items)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func CurrentWorldTick(ctx context.Context, database *gorm.DB) (int64, error) {
	return CurrentWorldTickForRealm(ctx, database, 1)
}

func CurrentWorldTickForRealm(ctx context.Context, database *gorm.DB, realmID uint) (int64, error) {
	realmID = normalizeRealmID(realmID)
	state := dal.WorldState{}
	result := database.WithContext(ctx).Where("realm_id = ?", realmID).Order("id ASC").First(&state)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return 0, nil
		}
		return 0, result.Error
	}
	return state.SimulationTick, nil
}

func LoadWorldSnapshot(ctx context.Context, database *gorm.DB, playerID uint, realmID uint) (WorldSnapshot, error) {
	realmID = normalizeRealmID(realmID)
	inventoryEntries := make([]dal.InventoryEntry, 0)
	if err := database.WithContext(ctx).
		Where("realm_id = ? AND owner_type = ? AND owner_id = ?", realmID, ActorPlayer, playerID).
		Find(&inventoryEntries).Error; err != nil {
		return WorldSnapshot{}, err
	}

	inventory := map[string]int64{}
	for _, entry := range inventoryEntries {
		inventory[entry.ItemKey] = entry.Quantity
	}

	stats, err := loadPlayerStats(ctx, database, playerID, realmID)
	if err != nil {
		return WorldSnapshot{}, err
	}

	marketRows := make([]dal.MarketPrice, 0)
	if err := database.WithContext(ctx).Where("realm_id = ?", realmID).Order("item_key ASC").Find(&marketRows).Error; err != nil {
		return WorldSnapshot{}, err
	}
	marketPrices := map[string]int64{}
	for _, row := range marketRows {
		marketPrices[row.ItemKey] = row.Price
	}

	behaviors := make([]dal.BehaviorInstance, 0)
	if err := database.WithContext(ctx).
		Where("realm_id = ? AND state IN ?", realmID, []string{behaviorQueued, behaviorActive, behaviorCompleted, behaviorFailed}).
		Order("id DESC").
		Limit(20).
		Find(&behaviors).Error; err != nil {
		return WorldSnapshot{}, err
	}

	views := make([]BehaviorView, 0, len(behaviors))
	for _, behavior := range behaviors {
		payload := parseBehaviorRuntimePayload(behavior.PayloadJSON)

		views = append(views, BehaviorView{
			ID:                        behavior.ID,
			Key:                       behavior.Key,
			ActorType:                 behavior.ActorType,
			ActorID:                   behavior.ActorID,
			State:                     behavior.State,
			ScheduledAt:               behavior.ScheduledAtTick,
			StartedAt:                 behavior.StartedAtTick,
			CompletesAt:               behavior.CompletesAtTick,
			DurationMinute:            behavior.DurationMinutes,
			MarketWaitDurationMinutes: payload.MarketWaitDurationMinutes,
			MarketWaitUntilTick:       payload.MarketWaitUntilTick,
			ResultMessage:             behavior.ResultMessage,
			FailureReason:             behavior.FailureReason,
		})
	}

	events := make([]dal.WorldEvent, 0)
	if err := database.WithContext(ctx).
		Where("realm_id = ?", realmID).
		Order("tick DESC, id DESC").
		Limit(10).
		Find(&events).Error; err != nil {
		return WorldSnapshot{}, err
	}
	recentEvents := make([]RecentEventView, 0, len(events))
	for _, event := range events {
		minuteOfDay := positiveMinuteOfDay(event.Tick)
		recentEvents = append(recentEvents, RecentEventView{
			ID:          event.ID,
			Tick:        event.Tick,
			Day:         event.Tick / minutesPerDay,
			MinuteOfDay: minuteOfDay,
			Clock:       fmt.Sprintf("%02d:%02d", minuteOfDay/60, minuteOfDay%60),
			Message:     event.Message,
			EventType:   event.EventType,
		})
	}

	ascension, err := loadOrInitAscensionForRealm(ctx, database, realmID)
	if err != nil {
		return WorldSnapshot{}, err
	}

	ascensionEligibility := ascensionEligibilityForState(inventory["coins"], ascension.Count)

	return WorldSnapshot{
		Inventory:      inventory,
		Stats:          stats,
		MarketPrices:   marketPrices,
		Behaviors:      views,
		RecentEvents:   recentEvents,
		AscensionCount: ascension.Count,
		WealthBonusPct: ascension.WealthBonusPct,
		Ascension:      ascensionEligibility,
	}, nil
}

func positiveMinuteOfDay(tick int64) int64 {
	minute := tick % minutesPerDay
	if minute < 0 {
		minute += minutesPerDay
	}
	return minute
}

func GetAscensionEligibility(ctx context.Context, database *gorm.DB) (AscensionEligibility, error) {
	return GetAscensionEligibilityForRealm(ctx, database, 1)
}

func GetAscensionEligibilityForRealm(ctx context.Context, database *gorm.DB, realmID uint) (AscensionEligibility, error) {
	coins, err := loadPlayerCoinsForRealm(ctx, database, realmID)
	if err != nil {
		return AscensionEligibility{}, err
	}
	ascension, err := loadOrInitAscensionForRealm(ctx, database, realmID)
	if err != nil {
		return AscensionEligibility{}, err
	}

	return ascensionEligibilityForState(coins, ascension.Count), nil
}

func GetAscensionEligibilityForPlayer(ctx context.Context, database *gorm.DB, playerID uint, realmID uint) (AscensionEligibility, error) {
	coins, err := loadPlayerCoinsForPlayer(ctx, database, playerID, realmID)
	if err != nil {
		return AscensionEligibility{}, err
	}
	ascension, err := loadOrInitAscensionForRealm(ctx, database, realmID)
	if err != nil {
		return AscensionEligibility{}, err
	}

	return ascensionEligibilityForState(coins, ascension.Count), nil
}

func Ascend(ctx context.Context, database *gorm.DB, playerName string) (int64, float64, error) {
	var count int64
	var bonus float64
	err := database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		eligibility, eligibilityErr := GetAscensionEligibility(ctx, tx)
		if eligibilityErr != nil {
			return eligibilityErr
		}
		if !eligibility.Available {
			return fmt.Errorf("%w: %s", ErrAscensionNotEligible, eligibility.Reason)
		}

		ascension, err := loadOrInitAscensionForRealm(ctx, tx, 1)
		if err != nil {
			return err
		}

		ascension.Count++
		ascension.WealthBonusPct += 10
		if err := tx.Model(&dal.AscensionState{}).
			Where("id = ?", ascension.ID).
			Updates(map[string]any{"count": ascension.Count, "wealth_bonus_pct": ascension.WealthBonusPct}).Error; err != nil {
			return err
		}

		if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&dal.InventoryEntry{}).Error; err != nil {
			return err
		}
		if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&dal.BehaviorInstance{}).Error; err != nil {
			return err
		}
		if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&dal.WorldEvent{}).Error; err != nil {
			return err
		}
		if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&dal.PlayerUnlock{}).Error; err != nil {
			return err
		}
		if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&dal.PlayerStat{}).Error; err != nil {
			return err
		}
		if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&dal.MarketPrice{}).Error; err != nil {
			return err
		}
		if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&dal.WorldState{}).Error; err != nil {
			return err
		}

		if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&dal.Player{}).Error; err != nil {
			return err
		}

		newPlayer := dal.Player{Name: playerName}
		if err := tx.Create(&newPlayer).Error; err != nil {
			return err
		}
		if err := tx.Create(&dal.WorldState{SimulationTick: 0}).Error; err != nil {
			return err
		}

		startingCoins := ascensionStartingCoins(ascension.Count)
		if startingCoins > 0 {
			if err := tx.Create(&dal.InventoryEntry{OwnerType: ActorPlayer, OwnerID: newPlayer.ID, ItemKey: "coins", Quantity: startingCoins}).Error; err != nil {
				return err
			}
		}

		if err := ensureMarketDefaults(ctx, tx, 0, 1); err != nil {
			return err
		}

		runtime, err := loadOrInitRuntimeState(ctx, tx)
		if err != nil {
			return err
		}
		if err := tx.Model(&dal.WorldRuntimeState{}).
			Where("id = ?", runtime.ID).
			Updates(map[string]any{
				"last_processed_tick_at": time.Now().UTC(),
				"carry_game_minutes":     0,
				"pending_behaviors_json": "[]",
			}).Error; err != nil {
			return err
		}

		count = ascension.Count
		bonus = ascension.WealthBonusPct
		return nil
	})

	if err != nil {
		return 0, 0, err
	}
	return count, bonus, nil
}

func AscendForPlayerRealm(ctx context.Context, database *gorm.DB, playerID uint, realmID uint, playerName string) (int64, float64, error) {
	realmID = normalizeRealmID(realmID)

	var count int64
	var bonus float64
	err := database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		eligibility, eligibilityErr := GetAscensionEligibilityForPlayer(ctx, tx, playerID, realmID)
		if eligibilityErr != nil {
			return eligibilityErr
		}
		if !eligibility.Available {
			return fmt.Errorf("%w: %s", ErrAscensionNotEligible, eligibility.Reason)
		}

		ascension, err := loadOrInitAscensionForRealm(ctx, tx, realmID)
		if err != nil {
			return err
		}

		ascension.Count++
		ascension.WealthBonusPct += 10
		if err := tx.Model(&dal.AscensionState{}).
			Where("id = ?", ascension.ID).
			Updates(map[string]any{"count": ascension.Count, "wealth_bonus_pct": ascension.WealthBonusPct}).Error; err != nil {
			return err
		}

		if strings.TrimSpace(playerName) != "" {
			if err := tx.Model(&dal.Player{}).Where("id = ?", playerID).Update("name", strings.TrimSpace(playerName)).Error; err != nil {
				return err
			}
		}

		if err := tx.WithContext(ctx).
			Where("realm_id = ? AND owner_type = ? AND owner_id = ?", realmID, ActorPlayer, playerID).
			Delete(&dal.InventoryEntry{}).Error; err != nil {
			return err
		}
		if err := tx.WithContext(ctx).
			Where("realm_id = ? AND actor_type = ? AND actor_id = ?", realmID, ActorPlayer, playerID).
			Delete(&dal.BehaviorInstance{}).Error; err != nil {
			return err
		}
		if err := tx.WithContext(ctx).
			Where("realm_id = ? AND player_id = ?", realmID, playerID).
			Delete(&dal.PlayerUnlock{}).Error; err != nil {
			return err
		}
		if err := tx.WithContext(ctx).
			Where("realm_id = ? AND player_id = ?", realmID, playerID).
			Delete(&dal.PlayerStat{}).Error; err != nil {
			return err
		}

		startingCoins := ascensionStartingCoins(ascension.Count)
		if startingCoins > 0 {
			if err := tx.Create(&dal.InventoryEntry{RealmID: realmID, OwnerType: ActorPlayer, OwnerID: playerID, ItemKey: "coins", Quantity: startingCoins}).Error; err != nil {
				return err
			}
		}

		count = ascension.Count
		bonus = ascension.WealthBonusPct
		return nil
	})

	if err != nil {
		return 0, 0, err
	}
	return count, bonus, nil
}

func loadGlobalPlayerCoins(ctx context.Context, database *gorm.DB) (int64, error) {
	return loadPlayerCoinsForRealm(ctx, database, 1)
}

func loadPlayerCoinsForRealm(ctx context.Context, database *gorm.DB, realmID uint) (int64, error) {
	realmID = normalizeRealmID(realmID)
	var total int64
	if err := database.WithContext(ctx).
		Model(&dal.InventoryEntry{}).
		Where("realm_id = ? AND owner_type = ? AND item_key = ?", realmID, ActorPlayer, "coins").
		Select("COALESCE(SUM(quantity), 0)").
		Scan(&total).Error; err != nil {
		return 0, err
	}

	return total, nil
}

func loadPlayerCoinsForPlayer(ctx context.Context, database *gorm.DB, playerID uint, realmID uint) (int64, error) {
	realmID = normalizeRealmID(realmID)
	var total int64
	if err := database.WithContext(ctx).
		Model(&dal.InventoryEntry{}).
		Where("realm_id = ? AND owner_type = ? AND owner_id = ? AND item_key = ?", realmID, ActorPlayer, playerID, "coins").
		Select("COALESCE(SUM(quantity), 0)").
		Scan(&total).Error; err != nil {
		return 0, err
	}

	return total, nil
}

func loadPlayerStats(ctx context.Context, database *gorm.DB, playerID uint, realmID uint) (map[string]int64, error) {
	realmID = normalizeRealmID(realmID)
	stats := map[string]int64{
		statStrength:             0,
		statSocial:               0,
		statMaxStamina:           defaultMaxStamina,
		statStaminaRecoveryRate:  defaultStaminaRecoveryRate,
		statStaminaRecoveryCarry: 0,
		statStaminaTrainingXP:    0,
		statStamina:              defaultMaxStamina,
	}
	rows := make([]dal.PlayerStat, 0)
	if err := database.WithContext(ctx).
		Where("realm_id = ? AND player_id = ?", realmID, playerID).
		Find(&rows).Error; err != nil {
		return nil, err
	}

	for _, row := range rows {
		stats[row.StatKey] = row.Value
	}

	if stats[statMaxStamina] <= 0 {
		stats[statMaxStamina] = defaultMaxStamina
	}
	if stats[statStaminaRecoveryRate] <= 0 {
		stats[statStaminaRecoveryRate] = defaultStaminaRecoveryRate
	}
	if stats[statStamina] <= 0 {
		stats[statStamina] = stats[statMaxStamina]
	}
	if stats[statStamina] > stats[statMaxStamina] {
		stats[statStamina] = stats[statMaxStamina]
	}

	return stats, nil
}

func applyStatDelta(ctx context.Context, tx *gorm.DB, playerID uint, realmID uint, delta map[string]int64) error {
	realmID = normalizeRealmID(realmID)
	if playerID == 0 || len(delta) == 0 {
		return nil
	}

	for statKey, change := range delta {
		if change == 0 {
			continue
		}

		entry := dal.PlayerStat{}
		result := tx.WithContext(ctx).
			Where("realm_id = ? AND player_id = ? AND stat_key = ?", realmID, playerID, statKey).
			Limit(1).
			Find(&entry)
		if result.Error != nil {
			return result.Error
		}

		if result.RowsAffected == 0 {
			entry = dal.PlayerStat{RealmID: realmID, PlayerID: playerID, StatKey: statKey, Value: 0}
		}

		next := entry.Value + change
		if next < 0 {
			next = 0
		}
		entry.Value = next

		if result.RowsAffected == 0 {
			if err := tx.WithContext(ctx).Create(&entry).Error; err != nil {
				return err
			}
			continue
		}

		if err := tx.WithContext(ctx).Save(&entry).Error; err != nil {
			return err
		}
	}

	return nil
}

func getPlayerStatValue(ctx context.Context, tx *gorm.DB, playerID uint, statKey string, realmID uint) (int64, error) {
	if playerID == 0 {
		return 0, nil
	}

	value, _, err := getPlayerStatValueWithPresence(ctx, tx, playerID, statKey, realmID)
	if err != nil {
		return 0, err
	}

	return value, nil
}

func getPlayerStatValueWithPresence(ctx context.Context, tx *gorm.DB, playerID uint, statKey string, realmID uint) (int64, bool, error) {
	realmID = normalizeRealmID(realmID)
	if playerID == 0 {
		return 0, false, nil
	}

	row := dal.PlayerStat{}
	result := tx.WithContext(ctx).
		Where("realm_id = ? AND player_id = ? AND stat_key = ?", realmID, playerID, statKey).
		Limit(1).
		Find(&row)
	if result.Error != nil {
		return 0, false, result.Error
	}
	if result.RowsAffected == 0 {
		return 0, false, nil
	}

	return row.Value, true, nil
}

func getPlayerStatValueOrDefault(ctx context.Context, tx *gorm.DB, playerID uint, statKey string, defaultValue int64, realmID uint) (int64, error) {
	value, found, err := getPlayerStatValueWithPresence(ctx, tx, playerID, statKey, realmID)
	if err != nil {
		return 0, err
	}
	if !found {
		return defaultValue, nil
	}
	return value, nil
}

func setPlayerStatValue(ctx context.Context, tx *gorm.DB, playerID uint, statKey string, value int64, realmID uint) error {
	realmID = normalizeRealmID(realmID)
	if playerID == 0 {
		return nil
	}

	if value < 0 {
		value = 0
	}

	entry := dal.PlayerStat{}
	result := tx.WithContext(ctx).
		Where("realm_id = ? AND player_id = ? AND stat_key = ?", realmID, playerID, statKey).
		Limit(1).
		Find(&entry)
	if result.Error != nil {
		return result.Error
	}

	if result.RowsAffected == 0 {
		entry = dal.PlayerStat{RealmID: realmID, PlayerID: playerID, StatKey: statKey, Value: value}
		return tx.WithContext(ctx).Create(&entry).Error
	}

	entry.Value = value
	return tx.WithContext(ctx).Save(&entry).Error
}

func consumeBehaviorStamina(ctx context.Context, tx *gorm.DB, behavior dal.BehaviorInstance, definition BehaviorDefinition) error {
	if behavior.ActorType != ActorPlayer || behavior.ActorID == 0 || definition.StaminaCost <= 0 {
		return nil
	}

	current, err := getPlayerStatValueOrDefault(ctx, tx, behavior.ActorID, statStamina, defaultMaxStamina, behavior.RealmID)
	if err != nil {
		return err
	}
	if current < definition.StaminaCost {
		return fmt.Errorf("insufficient stamina (need %d)", definition.StaminaCost)
	}

	if err := setPlayerStatValue(ctx, tx, behavior.ActorID, statStamina, current-definition.StaminaCost, behavior.RealmID); err != nil {
		return err
	}

	return nil
}

func awardStaminaRecoveryProgress(ctx context.Context, tx *gorm.DB, behavior dal.BehaviorInstance, definition BehaviorDefinition) error {
	if behavior.ActorType != ActorPlayer || behavior.ActorID == 0 || definition.StaminaCost <= 0 {
		return nil
	}

	if err := applyStatDelta(ctx, tx, behavior.ActorID, behavior.RealmID, map[string]int64{statStaminaTrainingXP: definition.StaminaCost}); err != nil {
		return err
	}

	xp, err := getPlayerStatValueOrDefault(ctx, tx, behavior.ActorID, statStaminaTrainingXP, 0, behavior.RealmID)
	if err != nil {
		return err
	}
	if xp < recoveryXPPerRatePoint {
		return nil
	}

	gain := xp / recoveryXPPerRatePoint
	remainingXP := xp % recoveryXPPerRatePoint

	rate, err := getPlayerStatValueOrDefault(ctx, tx, behavior.ActorID, statStaminaRecoveryRate, defaultStaminaRecoveryRate, behavior.RealmID)
	if err != nil {
		return err
	}

	if err := setPlayerStatValue(ctx, tx, behavior.ActorID, statStaminaRecoveryRate, rate+gain, behavior.RealmID); err != nil {
		return err
	}

	return setPlayerStatValue(ctx, tx, behavior.ActorID, statStaminaTrainingXP, remainingXP, behavior.RealmID)
}

func recoverPlayerStaminaTick(ctx context.Context, tx *gorm.DB, realmID uint) error {
	realmID = normalizeRealmID(realmID)
	characterPlayers := make([]struct {
		PlayerID uint
	}, 0)
	if err := tx.WithContext(ctx).
		Model(&dal.Character{}).
		Distinct("player_id").
		Where("realm_id = ? AND status = ?", realmID, "active").
		Find(&characterPlayers).Error; err != nil {
		return err
	}

	for _, characterPlayer := range characterPlayers {
		maxStamina, err := getPlayerStatValueOrDefault(ctx, tx, characterPlayer.PlayerID, statMaxStamina, defaultMaxStamina, realmID)
		if err != nil {
			return err
		}
		if maxStamina <= 0 {
			maxStamina = defaultMaxStamina
		}
		recoveryRate, err := getPlayerStatValueOrDefault(ctx, tx, characterPlayer.PlayerID, statStaminaRecoveryRate, defaultStaminaRecoveryRate, realmID)
		if err != nil {
			return err
		}
		if recoveryRate <= 0 {
			recoveryRate = defaultStaminaRecoveryRate
		}
		currentStamina, err := getPlayerStatValueOrDefault(ctx, tx, characterPlayer.PlayerID, statStamina, maxStamina, realmID)
		if err != nil {
			return err
		}
		recoveryCarry, err := getPlayerStatValueOrDefault(ctx, tx, characterPlayer.PlayerID, statStaminaRecoveryCarry, 0, realmID)
		if err != nil {
			return err
		}

		totalRecovery := recoveryCarry + recoveryRate
		recoveryPerTick := totalRecovery / 60
		nextCarry := totalRecovery % 60

		next := currentStamina + recoveryPerTick
		if next > maxStamina {
			next = maxStamina
		}

		if err := setPlayerStatValue(ctx, tx, characterPlayer.PlayerID, statMaxStamina, maxStamina, realmID); err != nil {
			return err
		}
		if err := setPlayerStatValue(ctx, tx, characterPlayer.PlayerID, statStaminaRecoveryRate, recoveryRate, realmID); err != nil {
			return err
		}
		if err := setPlayerStatValue(ctx, tx, characterPlayer.PlayerID, statStaminaRecoveryCarry, nextCarry, realmID); err != nil {
			return err
		}
		if err := setPlayerStatValue(ctx, tx, characterPlayer.PlayerID, statStamina, next, realmID); err != nil {
			return err
		}
	}

	return nil
}

func adjustedDurationMinutes(ctx context.Context, tx *gorm.DB, behavior dal.BehaviorInstance, definition BehaviorDefinition) int64 {
	duration := definition.DurationMinutes
	if duration <= 1 {
		return 1
	}

	if behavior.ActorType == ActorPlayer && behavior.Key == "player_chop_wood" {
		strength, err := getPlayerStatValue(ctx, tx, behavior.ActorID, statStrength, behavior.RealmID)
		if err == nil && strength > 0 {
			reduction := strength / 8
			if reduction > 0 {
				duration -= reduction
			}
		}
	}

	if duration < 8 {
		return 8
	}

	return duration
}

func ascensionEligibilityForState(coins, ascensionCount int64) AscensionEligibility {
	requirement := ascensionRequiredCoins(ascensionCount)
	if coins >= requirement {
		return AscensionEligibility{
			Available:        true,
			RequirementCoins: requirement,
			CurrentCoins:     coins,
			Reason:           "Ascension is available.",
		}
	}

	missing := requirement - coins
	return AscensionEligibility{
		Available:        false,
		RequirementCoins: requirement,
		CurrentCoins:     coins,
		Reason:           fmt.Sprintf("Ascension %d unlocks at %d coins. Earn %d more.", ascensionCount+1, requirement, missing),
	}
}

func ascensionRequiredCoins(ascensionCount int64) int64 {
	if ascensionCount <= 0 {
		return ascensionBaseMinCoins
	}

	scaled := float64(ascensionBaseMinCoins) * math.Pow(ascensionRequirementGrowth, float64(ascensionCount))
	required := int64(math.Round(scaled))
	if required < ascensionBaseMinCoins {
		return ascensionBaseMinCoins
	}
	return required
}

func ascensionStartingCoins(ascensionCount int64) int64 {
	if ascensionCount <= 0 {
		return 0
	}
	return ascensionStartCoinsPerRun * ascensionCount
}

func processActivations(ctx context.Context, tx *gorm.DB, currentTick int64, realmID uint) error {
	realmID = normalizeRealmID(realmID)
	queued := make([]dal.BehaviorInstance, 0)
	if err := tx.WithContext(ctx).
		Where("realm_id = ? AND state = ? AND scheduled_at_tick <= ?", realmID, behaviorQueued, currentTick).
		Order("scheduled_at_tick ASC, id ASC").
		Find(&queued).Error; err != nil {
		return err
	}

	for _, behavior := range queued {
		definition, ok := GetBehaviorDefinition(behavior.Key)
		if !ok {
			if err := markBehaviorFailed(ctx, tx, behavior.ID, "unknown behavior definition"); err != nil {
				return err
			}
			continue
		}

		payload := parseBehaviorRuntimePayload(behavior.PayloadJSON)

		if definition.RequiresMarketOpen && !IsMarketOpen(currentTick) {
			if payload.MarketWaitUntilTick > 0 && currentTick >= payload.MarketWaitUntilTick {
				reason := fmt.Sprintf("market did not open before timeout (waited %d minutes)", payload.MarketWaitDurationMinutes)
				if err := markBehaviorFailed(ctx, tx, behavior.ID, reason); err != nil {
					return err
				}
			}
			continue
		}

		if err := validateRequirements(ctx, tx, behavior.ActorID, definition.Requirements, behavior.RealmID); err != nil {
			if err := markBehaviorFailed(ctx, tx, behavior.ID, err.Error()); err != nil {
				return err
			}
			continue
		}

		if err := applyItemDelta(ctx, tx, behavior.ActorID, behavior.RealmID, negateMap(definition.Costs)); err != nil {
			if err := markBehaviorFailed(ctx, tx, behavior.ID, err.Error()); err != nil {
				return err
			}
			continue
		}

		if err := consumeBehaviorStamina(ctx, tx, behavior, definition); err != nil {
			if err := markBehaviorFailed(ctx, tx, behavior.ID, err.Error()); err != nil {
				return err
			}
			continue
		}

		adjustedDuration := adjustedDurationMinutes(ctx, tx, behavior, definition)

		if err := tx.WithContext(ctx).Model(&dal.BehaviorInstance{}).
			Where("id = ?", behavior.ID).
			Updates(map[string]any{
				"state":             behaviorActive,
				"started_at_tick":   currentTick,
				"completes_at_tick": currentTick + adjustedDuration,
				"duration_minutes":  adjustedDuration,
				"failure_reason":    "",
			}).Error; err != nil {
			return err
		}

		if definition.StartMessage != "" {
			if err := createWorldEvent(ctx, tx, currentTick, "behavior_started", definition.StartMessage, behavior.ActorType, behavior.ID, behavior.RealmID); err != nil {
				return err
			}
		}
	}

	return nil
}

func processCompletions(ctx context.Context, tx *gorm.DB, currentTick int64, realmID uint) error {
	realmID = normalizeRealmID(realmID)
	active := make([]dal.BehaviorInstance, 0)
	if err := tx.WithContext(ctx).
		Where("realm_id = ? AND state = ? AND completes_at_tick <= ?", realmID, behaviorActive, currentTick).
		Order("completes_at_tick ASC, id ASC").
		Find(&active).Error; err != nil {
		return err
	}

	ascension, err := loadOrInitAscensionForRealm(ctx, tx, realmID)
	if err != nil {
		return err
	}

	for _, behavior := range active {
		definition, ok := GetBehaviorDefinition(behavior.Key)
		if !ok {
			if err := markBehaviorFailed(ctx, tx, behavior.ID, "unknown behavior definition"); err != nil {
				return err
			}
			continue
		}

		payload := parseBehaviorRuntimePayload(behavior.PayloadJSON)

		if definition.RequiresMarketOpen && !IsMarketOpen(currentTick) {
			if payload.MarketWaitUntilTick > 0 && currentTick >= payload.MarketWaitUntilTick {
				reason := fmt.Sprintf("market did not open before timeout (waited %d minutes)", payload.MarketWaitDurationMinutes)
				if err := markBehaviorFailed(ctx, tx, behavior.ID, reason); err != nil {
					return err
				}
				continue
			}

			nextOpen := nextMarketOpenTick(currentTick)
			if err := tx.WithContext(ctx).Model(&dal.BehaviorInstance{}).
				Where("id = ?", behavior.ID).
				Updates(map[string]any{"completes_at_tick": nextOpen}).Error; err != nil {
				return err
			}
			continue
		}

		outputs, dynamicMessage, err := resolveBehaviorOutputs(ctx, tx, behavior, definition, currentTick)
		if err != nil {
			if err := markBehaviorFailed(ctx, tx, behavior.ID, err.Error()); err != nil {
				return err
			}
			continue
		}

		outputs = applyAscensionBonus(outputs, ascension.WealthBonusPct)
		if err := applyItemDelta(ctx, tx, behavior.ActorID, behavior.RealmID, outputs); err != nil {
			if err := markBehaviorFailed(ctx, tx, behavior.ID, err.Error()); err != nil {
				return err
			}
			continue
		}

		if err := grantUnlocks(ctx, tx, behavior.ActorID, definition.GrantsUnlocks, behavior.RealmID); err != nil {
			if err := markBehaviorFailed(ctx, tx, behavior.ID, err.Error()); err != nil {
				return err
			}
			continue
		}

		if err := applyStatDelta(ctx, tx, behavior.ActorID, behavior.RealmID, definition.StatDeltas); err != nil {
			if err := markBehaviorFailed(ctx, tx, behavior.ID, err.Error()); err != nil {
				return err
			}
			continue
		}

		if err := awardStaminaRecoveryProgress(ctx, tx, behavior, definition); err != nil {
			if err := markBehaviorFailed(ctx, tx, behavior.ID, err.Error()); err != nil {
				return err
			}
			continue
		}

		marketMessage, err := applyMarketEffects(ctx, tx, behavior, definition, currentTick)
		if err != nil {
			if err := markBehaviorFailed(ctx, tx, behavior.ID, err.Error()); err != nil {
				return err
			}
			continue
		}

		message := definition.CompleteMessage
		if message == "" {
			message = fmt.Sprintf("Behavior %s completed.", behavior.Key)
		}
		if dynamicMessage != "" {
			message = strings.TrimSpace(message + " " + dynamicMessage)
		}
		if marketMessage != "" {
			message = strings.TrimSpace(message + " " + marketMessage)
		}

		if err := tx.WithContext(ctx).Model(&dal.BehaviorInstance{}).
			Where("id = ?", behavior.ID).
			Updates(map[string]any{
				"state":          behaviorCompleted,
				"result_message": message,
				"failure_reason": "",
			}).Error; err != nil {
			return err
		}

		if err := createWorldEvent(ctx, tx, currentTick, "behavior_completed", message, behavior.ActorType, behavior.ID, behavior.RealmID); err != nil {
			return err
		}

		if definition.RepeatIntervalMin > 0 {
			next := dal.BehaviorInstance{
				RealmID:           behavior.RealmID,
				Key:               behavior.Key,
				ActorType:         behavior.ActorType,
				ActorID:           behavior.ActorID,
				State:             behaviorQueued,
				ScheduledAtTick:   currentTick + definition.RepeatIntervalMin,
				DurationMinutes:   definition.DurationMinutes,
				PayloadJSON:       "{}",
				RepeatIntervalMin: definition.RepeatIntervalMin,
			}
			if err := tx.WithContext(ctx).Create(&next).Error; err != nil {
				return err
			}
		}
	}

	return nil
}

func validateRequirements(ctx context.Context, tx *gorm.DB, playerID uint, requirements Requirement, realmID uint) error {
	realmID = normalizeRealmID(realmID)
	for _, unlock := range requirements.Unlocks {
		var count int64
		if err := tx.WithContext(ctx).
			Model(&dal.PlayerUnlock{}).
			Where("realm_id = ? AND player_id = ? AND unlock_key = ?", realmID, playerID, unlock).
			Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			return fmt.Errorf("missing unlock: %s", unlock)
		}
	}

	for item, required := range requirements.Items {
		if required <= 0 {
			continue
		}
		qty, err := getInventoryQuantity(ctx, tx, playerID, item, realmID)
		if err != nil {
			return err
		}
		if qty < required {
			return fmt.Errorf("missing %s (need %d)", item, required)
		}
	}

	return nil
}

func applyItemDelta(ctx context.Context, tx *gorm.DB, playerID uint, realmID uint, delta map[string]int64) error {
	realmID = normalizeRealmID(realmID)
	for item, change := range delta {
		if change == 0 {
			continue
		}

		entry := dal.InventoryEntry{}
		result := tx.WithContext(ctx).
			Where("realm_id = ? AND owner_type = ? AND owner_id = ? AND item_key = ?", realmID, ActorPlayer, playerID, item).
			Limit(1).
			Find(&entry)
		if result.Error != nil {
			return result.Error
		}

		if result.RowsAffected == 0 {
			entry = dal.InventoryEntry{RealmID: realmID, OwnerType: ActorPlayer, OwnerID: playerID, ItemKey: item, Quantity: 0}
		}

		nextQty := entry.Quantity + change
		if nextQty < 0 {
			return fmt.Errorf("insufficient %s", item)
		}
		entry.Quantity = nextQty

		if result.RowsAffected == 0 {
			if err := tx.WithContext(ctx).Create(&entry).Error; err != nil {
				return err
			}
			continue
		}
		if err := tx.WithContext(ctx).Save(&entry).Error; err != nil {
			return err
		}
	}
	return nil
}

func getInventoryQuantity(ctx context.Context, tx *gorm.DB, playerID uint, item string, realmID uint) (int64, error) {
	realmID = normalizeRealmID(realmID)
	entry := dal.InventoryEntry{}
	result := tx.WithContext(ctx).
		Where("realm_id = ? AND owner_type = ? AND owner_id = ? AND item_key = ?", realmID, ActorPlayer, playerID, item).
		Limit(1).
		Find(&entry)
	if result.Error != nil {
		return 0, result.Error
	}
	if result.RowsAffected == 0 {
		return 0, nil
	}
	return entry.Quantity, nil
}

func markBehaviorFailed(ctx context.Context, tx *gorm.DB, behaviorID uint, reason string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "behavior failed"
	}
	return tx.WithContext(ctx).Model(&dal.BehaviorInstance{}).
		Where("id = ?", behaviorID).
		Updates(map[string]any{"state": behaviorFailed, "failure_reason": reason}).Error
}

func createWorldEvent(ctx context.Context, tx *gorm.DB, tick int64, eventType, message, source string, refID uint, realmID uint) error {
	event := dal.WorldEvent{
		RealmID:     normalizeRealmID(realmID),
		Tick:        tick,
		EventType:   eventType,
		Message:     message,
		Visibility:  "public",
		Source:      source,
		ReferenceID: refID,
	}
	return tx.WithContext(ctx).Create(&event).Error
}

func resolveBehaviorOutputs(ctx context.Context, tx *gorm.DB, behavior dal.BehaviorInstance, definition BehaviorDefinition, currentTick int64) (map[string]int64, string, error) {
	rng := rand.New(rand.NewSource(time.Now().UnixNano() + int64(behavior.ID) + currentTick))
	outputs := map[string]int64{}

	for item, qty := range definition.Outputs {
		if !shouldAwardOutput(definition.OutputChances[item], rng) {
			continue
		}
		outputs[item] = outputs[item] + qty
	}

	for item, expression := range definition.OutputExpressions {
		if !shouldAwardOutput(definition.OutputChances[item], rng) {
			continue
		}

		resolved, err := resolveOutputExpression(strings.TrimSpace(expression), rng)
		if err != nil {
			return nil, "", fmt.Errorf("invalid output expression for %s: %w", item, err)
		}
		outputs[item] = outputs[item] + resolved
	}

	if behavior.Key == "player_sell_scrap" {
		if !IsMarketOpen(currentTick) {
			return nil, "", fmt.Errorf("market is closed")
		}

		price, err := getMarketPrice(ctx, tx, "scrap", behavior.RealmID)
		if err != nil {
			return nil, "", err
		}

		social, err := getPlayerStatValue(ctx, tx, behavior.ActorID, statSocial, behavior.RealmID)
		if err != nil {
			return nil, "", err
		}
		socialBonus := social / 5
		if socialBonus > 0 {
			price += socialBonus
		}
		if price < 1 {
			price = 1
		}

		outputs["coins"] = outputs["coins"] + price
		return outputs, fmt.Sprintf("Current scrap market price yields %d coins.", price), nil
	}

	if behavior.Key == "player_sell_wood" {
		if !IsMarketOpen(currentTick) {
			return nil, "", fmt.Errorf("market is closed")
		}

		price, err := getMarketPrice(ctx, tx, "wood", behavior.RealmID)
		if err != nil {
			return nil, "", err
		}

		social, err := getPlayerStatValue(ctx, tx, behavior.ActorID, statSocial, behavior.RealmID)
		if err != nil {
			return nil, "", err
		}
		socialBonus := social / 6
		if socialBonus > 0 {
			price += socialBonus
		}
		if price < 1 {
			price = 1
		}

		outputs["coins"] = outputs["coins"] + price
		return outputs, fmt.Sprintf("Current wood market price yields %d coins.", price), nil
	}

	if behavior.Key == "player_chop_wood" {
		strength, err := getPlayerStatValue(ctx, tx, behavior.ActorID, statStrength, behavior.RealmID)
		if err != nil {
			return nil, "", err
		}

		woodBonus := strength / 4
		if woodBonus > 0 {
			outputs["wood"] = outputs["wood"] + woodBonus
			return outputs, fmt.Sprintf("Your strength grants +%d bonus wood.", woodBonus), nil
		}
	}

	if behavior.Key == "world_market_ai_cycle" {
		if !IsMarketOpen(currentTick) {
			return outputs, "The market is closed overnight; AI desks hold positions.", nil
		}

		delta, explanation, err := decideAIMarketDelta(ctx, tx, behavior.RealmID)
		if err != nil {
			return nil, "", err
		}
		if delta != 0 {
			newPrice, err := applySingleMarketDelta(ctx, tx, "scrap", delta, behavior.Key, currentTick, behavior.RealmID)
			if err != nil {
				return nil, "", err
			}
			return outputs, fmt.Sprintf("%s Scrap now trades at %d coins.", explanation, newPrice), nil
		}
	}

	return outputs, "", nil
}

func resolveOutputExpression(expression string, rng *rand.Rand) (int64, error) {
	if expression == "" {
		return 0, nil
	}

	if strings.Contains(expression, "+") {
		parts := strings.Split(expression, "+")
		if len(parts) != 2 {
			return 0, fmt.Errorf("unsupported expression: %s", expression)
		}

		base, err := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
		if err != nil {
			return 0, err
		}

		dieExpr := strings.TrimSpace(parts[1])
		if !strings.HasPrefix(strings.ToLower(dieExpr), "d") {
			return 0, fmt.Errorf("unsupported dice expression: %s", expression)
		}

		sides, err := strconv.ParseInt(strings.TrimPrefix(strings.ToLower(dieExpr), "d"), 10, 64)
		if err != nil || sides <= 0 {
			return 0, fmt.Errorf("invalid dice sides in expression: %s", expression)
		}

		roll := int64(rng.Intn(int(sides)) + 1)
		return base + roll, nil
	}

	if strings.Count(expression, "-") == 1 {
		parts := strings.Split(expression, "-")
		minValue, errMin := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
		maxValue, errMax := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
		if errMin == nil && errMax == nil && maxValue >= minValue {
			rangeSize := maxValue - minValue + 1
			roll := int64(rng.Intn(int(rangeSize)))
			return minValue + roll, nil
		}
	}

	staticValue, err := strconv.ParseInt(expression, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("unsupported output expression: %s", expression)
	}

	return staticValue, nil
}

func grantUnlocks(ctx context.Context, tx *gorm.DB, playerID uint, unlocks []string, realmID uint) error {
	realmID = normalizeRealmID(realmID)
	if playerID == 0 || len(unlocks) == 0 {
		return nil
	}

	for _, unlock := range unlocks {
		unlock = strings.TrimSpace(unlock)
		if unlock == "" {
			continue
		}

		var count int64
		if err := tx.WithContext(ctx).
			Model(&dal.PlayerUnlock{}).
			Where("realm_id = ? AND player_id = ? AND unlock_key = ?", realmID, playerID, unlock).
			Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			continue
		}

		if err := tx.WithContext(ctx).Create(&dal.PlayerUnlock{RealmID: realmID, PlayerID: playerID, UnlockKey: unlock}).Error; err != nil {
			return err
		}
	}

	return nil
}

func applyMarketEffects(ctx context.Context, tx *gorm.DB, behavior dal.BehaviorInstance, definition BehaviorDefinition, currentTick int64) (string, error) {
	if !IsMarketOpen(currentTick) {
		return "Market doors are shut overnight; convoy effects pause until open.", nil
	}

	if len(definition.MarketEffects) == 0 {
		return "", nil
	}

	messages := make([]string, 0, len(definition.MarketEffects))
	for item, delta := range definition.MarketEffects {
		if delta == 0 {
			continue
		}

		price, err := applySingleMarketDelta(ctx, tx, item, delta, behavior.Key, currentTick, behavior.RealmID)
		if err != nil {
			return "", err
		}
		messages = append(messages, fmt.Sprintf("%s price shifts by %+d to %d.", item, delta, price))
	}

	return strings.Join(messages, " "), nil
}

func ensureMarketDefaults(ctx context.Context, database *gorm.DB, currentTick int64, realmID uint) error {
	realmID = normalizeRealmID(realmID)
	defaults := map[string]int64{"scrap": 8, "wood": 5}
	for item, price := range defaults {
		entry := dal.MarketPrice{}
		result := database.WithContext(ctx).Where("realm_id = ? AND item_key = ?", realmID, item).Limit(1).Find(&entry)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected > 0 {
			continue
		}

		entry = dal.MarketPrice{RealmID: realmID, ItemKey: item, Price: price, LastDelta: 0, LastSource: "bootstrap", UpdatedTick: currentTick}
		if err := database.WithContext(ctx).Create(&entry).Error; err != nil {
			return err
		}

		if err := appendMarketHistory(ctx, database, item, currentTick, price, 0, "bootstrap", realmID); err != nil {
			return err
		}
	}
	return nil
}

func getMarketPrice(ctx context.Context, tx *gorm.DB, item string, realmID uint) (int64, error) {
	realmID = normalizeRealmID(realmID)
	entry := dal.MarketPrice{}
	result := tx.WithContext(ctx).Where("realm_id = ? AND item_key = ?", realmID, item).Limit(1).Find(&entry)
	if result.Error != nil {
		return 0, result.Error
	}
	if result.RowsAffected == 0 {
		if err := ensureMarketDefaults(ctx, tx, 0, realmID); err != nil {
			return 0, err
		}
		result = tx.WithContext(ctx).Where("realm_id = ? AND item_key = ?", realmID, item).Limit(1).Find(&entry)
		if result.Error != nil {
			return 0, result.Error
		}
	}
	if entry.Price < 1 {
		entry.Price = 1
	}
	return entry.Price, nil
}

func applySingleMarketDelta(ctx context.Context, tx *gorm.DB, item string, delta int64, source string, currentTick int64, realmID uint) (int64, error) {
	realmID = normalizeRealmID(realmID)
	entry := dal.MarketPrice{}
	result := tx.WithContext(ctx).Where("realm_id = ? AND item_key = ?", realmID, item).Limit(1).Find(&entry)
	if result.Error != nil {
		return 0, result.Error
	}
	if result.RowsAffected == 0 {
		entry = dal.MarketPrice{RealmID: realmID, ItemKey: item, Price: 1}
		if err := tx.WithContext(ctx).Create(&entry).Error; err != nil {
			return 0, err
		}
	}

	entry.Price = entry.Price + delta
	if entry.Price < 1 {
		entry.Price = 1
	}
	if entry.Price > 500 {
		entry.Price = 500
	}
	entry.LastDelta = delta
	entry.LastSource = source
	entry.UpdatedTick = currentTick

	if err := tx.WithContext(ctx).Save(&entry).Error; err != nil {
		return 0, err
	}

	if err := appendMarketHistory(ctx, tx, item, currentTick, entry.Price, delta, source, realmID); err != nil {
		return 0, err
	}

	return entry.Price, nil
}

func appendMarketHistory(ctx context.Context, tx *gorm.DB, item string, tick, price, delta int64, source string, realmID uint) error {
	entry := dal.MarketHistory{
		RealmID:      normalizeRealmID(realmID),
		ItemKey:      item,
		Tick:         tick,
		Price:        price,
		Delta:        delta,
		Source:       source,
		SessionState: MarketSessionState(tick),
	}
	return tx.WithContext(ctx).Create(&entry).Error
}

func decideAIMarketDelta(ctx context.Context, tx *gorm.DB, realmID uint) (int64, string, error) {
	realmID = normalizeRealmID(realmID)
	var scrapStock int64
	if err := tx.WithContext(ctx).
		Model(&dal.InventoryEntry{}).
		Where("realm_id = ? AND owner_type = ? AND item_key = ?", realmID, ActorPlayer, "scrap").
		Select("COALESCE(SUM(quantity), 0)").
		Scan(&scrapStock).Error; err != nil {
		return 0, "", err
	}

	if scrapStock >= 6 {
		return -1, "AI traders detect oversupply and press prices down.", nil
	}
	if scrapStock <= 1 {
		return +1, "AI traders detect scarcity and bid prices upward.", nil
	}

	var recentSales int64
	if err := tx.WithContext(ctx).
		Model(&dal.BehaviorInstance{}).
		Where("realm_id = ? AND key = ? AND state = ?", realmID, "player_sell_scrap", behaviorCompleted).
		Count(&recentSales).Error; err != nil {
		return 0, "", err
	}

	if recentSales%2 == 0 {
		return +1, "AI traders anticipate stronger demand and tighten supply.", nil
	}

	return -1, "AI traders rotate inventory and undercut local offers.", nil
}

func shouldAwardOutput(chance float64, rng *rand.Rand) bool {
	if chance <= 0 {
		return true
	}
	if chance >= 1 {
		return true
	}
	return rng.Float64() <= chance
}

func IsMarketOpen(currentTick int64) bool {
	minuteOfDay := positiveModulo(currentTick, minutesPerDay)
	return minuteOfDay >= marketOpenMinute && minuteOfDay < marketCloseMinute
}

func nextMarketOpenTick(currentTick int64) int64 {
	minuteOfDay := positiveModulo(currentTick, minutesPerDay)
	if minuteOfDay < marketOpenMinute {
		return currentTick + (marketOpenMinute - minuteOfDay)
	}
	return currentTick + (minutesPerDay - minuteOfDay + marketOpenMinute)
}

func MarketSessionState(currentTick int64) string {
	if IsMarketOpen(currentTick) {
		return "open"
	}
	return "closed"
}

func GetMarketStatus(ctx context.Context, database *gorm.DB, currentTick int64, realmID uint) (MarketStatus, error) {
	realmID = normalizeRealmID(realmID)
	rows := make([]dal.MarketPrice, 0)
	if err := database.WithContext(ctx).Where("realm_id = ?", realmID).Order("item_key ASC").Find(&rows).Error; err != nil {
		return MarketStatus{}, err
	}

	tickers := make([]MarketTickerView, 0, len(rows))
	for _, row := range rows {
		tickers = append(tickers, MarketTickerView{
			Symbol:       row.ItemKey,
			Price:        row.Price,
			Delta:        row.LastDelta,
			LastSource:   row.LastSource,
			UpdatedTick:  row.UpdatedTick,
			SessionState: MarketSessionState(currentTick),
		})
	}

	minuteOfDay := positiveModulo(currentTick, minutesPerDay)
	minutesToOpen := int64(0)
	minutesToClose := int64(0)
	if IsMarketOpen(currentTick) {
		minutesToClose = marketCloseMinute - minuteOfDay
	} else {
		if minuteOfDay < marketOpenMinute {
			minutesToOpen = marketOpenMinute - minuteOfDay
		} else {
			minutesToOpen = minutesPerDay - minuteOfDay + marketOpenMinute
		}
	}

	return MarketStatus{
		Tick:           currentTick,
		Day:            currentTick / minutesPerDay,
		MinuteOfDay:    minuteOfDay,
		IsOpen:         IsMarketOpen(currentTick),
		SessionState:   MarketSessionState(currentTick),
		MinutesToOpen:  minutesToOpen,
		MinutesToClose: minutesToClose,
		Tickers:        tickers,
	}, nil
}

func GetMarketHistory(ctx context.Context, database *gorm.DB, symbol string, limit int, realmID uint) ([]MarketHistoryEntry, error) {
	realmID = normalizeRealmID(realmID)
	if err := ensureMarketHistorySeed(ctx, database, realmID); err != nil {
		return nil, err
	}

	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}

	query := database.WithContext(ctx).Model(&dal.MarketHistory{}).Where("realm_id = ?", realmID)
	if strings.TrimSpace(symbol) != "" {
		query = query.Where("item_key = ?", strings.TrimSpace(symbol))
	}

	rows := make([]dal.MarketHistory, 0)
	if err := query.Order("tick DESC, id DESC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}

	history := make([]MarketHistoryEntry, 0, len(rows))
	for _, row := range rows {
		history = append(history, MarketHistoryEntry{
			Symbol:       row.ItemKey,
			Tick:         row.Tick,
			Price:        row.Price,
			Delta:        row.Delta,
			Source:       row.Source,
			SessionState: row.SessionState,
		})
	}

	return history, nil
}

func ensureMarketHistorySeed(ctx context.Context, database *gorm.DB, realmID uint) error {
	realmID = normalizeRealmID(realmID)
	var count int64
	if err := database.WithContext(ctx).Model(&dal.MarketHistory{}).Where("realm_id = ?", realmID).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	prices := make([]dal.MarketPrice, 0)
	if err := database.WithContext(ctx).Where("realm_id = ?", realmID).Find(&prices).Error; err != nil {
		return err
	}

	for _, row := range prices {
		history := dal.MarketHistory{
			RealmID:      realmID,
			ItemKey:      row.ItemKey,
			Tick:         row.UpdatedTick,
			Price:        row.Price,
			Delta:        row.LastDelta,
			Source:       row.LastSource,
			SessionState: MarketSessionState(row.UpdatedTick),
		}
		if err := database.WithContext(ctx).Create(&history).Error; err != nil {
			return err
		}
	}

	return nil
}

func positiveModulo(value, mod int64) int64 {
	if mod == 0 {
		return 0
	}
	result := value % mod
	if result < 0 {
		result += mod
	}
	return result
}

func parseBehaviorRuntimePayload(raw string) behaviorRuntimePayload {
	payload := behaviorRuntimePayload{}
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "{}" {
		return payload
	}

	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return behaviorRuntimePayload{}
	}

	if payload.MarketWaitDurationMinutes < 0 {
		payload.MarketWaitDurationMinutes = 0
	}
	if payload.MarketWaitUntilTick < 0 {
		payload.MarketWaitUntilTick = 0
	}
	return payload
}

func marshalBehaviorRuntimePayload(payload behaviorRuntimePayload) (string, error) {
	if payload == (behaviorRuntimePayload{}) {
		return "{}", nil
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	return string(encoded), nil
}

func loadOrInitAscension(ctx context.Context, database *gorm.DB) (*dal.AscensionState, error) {
	return loadOrInitAscensionForRealm(ctx, database, 1)
}

func loadOrInitAscensionForRealm(ctx context.Context, database *gorm.DB, realmID uint) (*dal.AscensionState, error) {
	realmID = normalizeRealmID(realmID)
	asc := &dal.AscensionState{}
	result := database.WithContext(ctx).Where("realm_id = ? AND key = ?", realmID, ascensionKey).Limit(1).Find(asc)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected > 0 {
		return asc, nil
	}

	initial := &dal.AscensionState{RealmID: realmID, Key: ascensionKey, Count: 0, WealthBonusPct: 0}
	if err := database.WithContext(ctx).Create(initial).Error; err != nil {
		return nil, err
	}
	return initial, nil
}

func loadOrInitRuntimeState(ctx context.Context, database *gorm.DB) (*dal.WorldRuntimeState, error) {
	return loadOrInitRuntimeStateForRealm(ctx, database, 1)
}

func loadOrInitRuntimeStateForRealm(ctx context.Context, database *gorm.DB, realmID uint) (*dal.WorldRuntimeState, error) {
	realmID = normalizeRealmID(realmID)
	runtimeState := &dal.WorldRuntimeState{}
	result := database.WithContext(ctx).Where("realm_id = ? AND key = ?", realmID, worldRuntimeStateKey).Limit(1).Find(runtimeState)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected > 0 {
		return runtimeState, nil
	}

	initial := &dal.WorldRuntimeState{
		RealmID:              realmID,
		Key:                  worldRuntimeStateKey,
		LastProcessedTickAt:  time.Now().UTC(),
		CarryGameMinutes:     0,
		PendingBehaviorsJSON: "[]",
	}
	if err := database.WithContext(ctx).Create(initial).Error; err != nil {
		return nil, err
	}
	return initial, nil
}

func applyAscensionBonus(outputs map[string]int64, wealthBonusPct float64) map[string]int64 {
	if len(outputs) == 0 {
		return map[string]int64{}
	}

	result := map[string]int64{}
	for item, qty := range outputs {
		if item == "coins" && qty > 0 && wealthBonusPct > 0 {
			multiplier := 1 + (wealthBonusPct / 100)
			qty = int64(math.Round(float64(qty) * multiplier))
			if qty < 1 {
				qty = 1
			}
		}
		result[item] = qty
	}
	return result
}

func negateMap(values map[string]int64) map[string]int64 {
	if len(values) == 0 {
		return map[string]int64{}
	}
	negated := map[string]int64{}
	for key, value := range values {
		negated[key] = -value
	}
	return negated
}

func SortBehaviorDefinitions(definitions []BehaviorDefinition) []BehaviorDefinition {
	sorted := make([]BehaviorDefinition, len(definitions))
	copy(sorted, definitions)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Key < sorted[j].Key
	})
	return sorted
}
