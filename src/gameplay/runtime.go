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
	behaviorQueued          = "queued"
	behaviorActive          = "active"
	behaviorCompleted       = "completed"
	behaviorCancelled       = "cancelled"
	behaviorFailed          = "failed"
	behaviorModeOnce        = "once"
	behaviorModeRepeat      = "repeat"
	behaviorModeRepeatUntil = "repeat-until"

	worldRuntimeStateKey = "world"
	ascensionKey         = "global"

	marketOpenMinute  = 8 * 60
	marketCloseMinute = 20 * 60
	nightStartMinute  = 20 * 60
	nightEndMinute    = 6 * 60
	minutesPerDay     = 24 * 60

	ascensionBaseMinCoins      int64   = 250
	ascensionRequirementGrowth float64 = 1.75
	ascensionStartCoinsPerRun  int64   = 50

	defaultMarketWaitDurationMinutes int64 = 24 * 60
	maxMarketWaitDurationMinutes     int64 = 14 * 24 * 60

	marketImpactBudgetWindowTicks       int64 = 6 * 60
	marketImpactMinBudgetPerDirection   int64 = 4
	marketImpactMaxBudgetPerDirection   int64 = 32
	marketImpactLiquidityDivisor        int64 = 4000
	marketImpactPopulationBaselineDiv   int64 = 6
	marketImpactStorytellerBonus        int64 = 2
	marketImpactWorldEventBonus         int64 = 1
	marketStorytellerDeltaSource              = "storyteller_curve"

	statStrength  = "strength"
	statSocial    = "social"
	statFinancial = "financial"
	statEndurance = "endurance"

	statStamina              = "stamina"
	statMaxStamina           = "max_stamina"
	statStaminaRecoveryRate  = "stamina_recovery_rate"
	statStaminaRecoveryCarry = "stamina_recovery_carry"
	statTradingAptitude      = "trading_aptitude"

	defaultMaxStamina          int64 = 100
	defaultStaminaRecoveryRate int64 = 8

	maxStaminaPerEndurancePoint     int64 = 3
	recoveryRatePerEnduranceDivisor int64 = 4

	restBehaviorKey                 = "player_rest"
	restRecoveryMultiplier    int64 = 4
	restMinimumRecoveryPoints int64 = 6
)

var ErrAscensionNotEligible = errors.New("ascension is not yet available")
var ErrBehaviorConflict = errors.New("behavior queue conflict")
var ErrQueueSlotsFull = errors.New("queue slots are full")
var ErrBehaviorNotCancelable = errors.New("behavior is not cancelable")

type BehaviorView struct {
	ID                        uint             `json:"id"`
	Key                       string           `json:"key"`
	ActorType                 string           `json:"actorType"`
	ActorID                   uint             `json:"actorId"`
	State                     string           `json:"state"`
	Mode                      string           `json:"mode,omitempty"`
	RepeatIntervalMinutes     int64            `json:"repeatIntervalMinutes,omitempty"`
	RepeatUntilTick           int64            `json:"repeatUntilTick,omitempty"`
	ScheduledAt               int64            `json:"scheduledAtTick"`
	StartedAt                 int64            `json:"startedAtTick"`
	CompletesAt               int64            `json:"completesAtTick"`
	DurationMinute            int64            `json:"durationMinutes"`
	MarketWaitDurationMinutes int64            `json:"marketWaitDurationMinutes,omitempty"`
	MarketWaitUntilTick       int64            `json:"marketWaitUntilTick,omitempty"`
	ResultMessage             string           `json:"resultMessage"`
	FailureReason             string           `json:"failureReason"`
	WaitReason                string           `json:"waitReason,omitempty"`
	Spent                     map[string]int64 `json:"spent,omitempty"`
	Gained                    map[string]int64 `json:"gained,omitempty"`
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
	CoreStats      map[string]int64     `json:"coreStats"`
	DerivedStats   map[string]int64     `json:"derivedStats"`
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
	Symbol       string                  `json:"symbol"`
	Price        int64                   `json:"price"`
	Delta        int64                   `json:"delta"`
	LastSource   string                  `json:"lastSource"`
	UpdatedTick  int64                   `json:"updatedTick"`
	SessionState string                  `json:"sessionState"`
	Liquidity    MarketLiquidityView     `json:"liquidity"`
	Movement     MarketMovementStatsView `json:"movement"`
}

type MarketLiquidityView struct {
	Quantity         int64   `json:"quantity"`
	BaselineQuantity int64   `json:"baselineQuantity"`
	MinQuantity      int64   `json:"minQuantity"`
	MaxQuantity      int64   `json:"maxQuantity"`
	UtilizationPct   float64 `json:"utilizationPct"`
	CapEstimate      int64   `json:"capEstimate"`
	LastPressure     int64   `json:"lastPressure"`
}

type MarketMovementStatsView struct {
	WindowTicks          int64   `json:"windowTicks"`
	WindowChange         int64   `json:"windowChange"`
	WindowRange          int64   `json:"windowRange"`
	WindowHigh           int64   `json:"windowHigh"`
	WindowLow            int64   `json:"windowLow"`
	Trades               int64   `json:"trades"`
	NPCTradeSharePct     float64 `json:"npcTradeSharePct"`
	NPCParticipantTrades int64   `json:"npcParticipantTrades"`
	NPCCycleMoves        int64   `json:"npcCycleMoves"`
	StorytellerMoves     int64   `json:"storytellerMoves"`
	OrderbookMoves       int64   `json:"orderbookMoves"`
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

type MarketCandleEntry struct {
	Symbol          string `json:"symbol"`
	BucketStartTick int64  `json:"bucketStartTick"`
	Open            int64  `json:"open"`
	High            int64  `json:"high"`
	Low             int64  `json:"low"`
	Close           int64  `json:"close"`
	Points          int64  `json:"points"`
}

type QueueBehaviorOptions struct {
	MarketWaitDurationMinutes int64
	RealmID                   uint
	Mode                      string
	RepeatUntilTick           int64
}

type behaviorRuntimePayload struct {
	MarketWaitDurationMinutes int64            `json:"marketWaitDurationMinutes,omitempty"`
	MarketWaitUntilTick       int64            `json:"marketWaitUntilTick,omitempty"`
	Mode                      string           `json:"mode,omitempty"`
	RepeatIntervalMinutes     int64            `json:"repeatIntervalMinutes,omitempty"`
	RepeatUntilTick           int64            `json:"repeatUntilTick,omitempty"`
	Spent                     map[string]int64 `json:"spent,omitempty"`
	Gained                    map[string]int64 `json:"gained,omitempty"`
}

func QueuePlayerBehavior(ctx context.Context, database *gorm.DB, playerID uint, behaviorKey string, currentTick int64, options QueueBehaviorOptions) error {
	if err := ValidatePlayerBehaviorKey(behaviorKey); err != nil {
		return err
	}

	definition, _ := GetBehaviorDefinition(behaviorKey)
	mode := strings.ToLower(strings.TrimSpace(options.Mode))
	if mode == "" {
		mode = behaviorModeOnce
	}
	if mode != behaviorModeOnce && mode != behaviorModeRepeat && mode != behaviorModeRepeatUntil {
		return fmt.Errorf("mode must be once, repeat, or repeat-until")
	}
	if !behaviorModeSupported(definition, mode) {
		return fmt.Errorf("mode %q is not supported for behavior %s", mode, behaviorKey)
	}

	realmID := normalizeRealmID(options.RealmID)

	if definition.SingleUsePerAscension {
		consumed, err := hasConsumedSingleUseBehavior(ctx, database, playerID, behaviorKey, realmID)
		if err != nil {
			return err
		}
		if consumed {
			return fmt.Errorf("%w: behavior %s can only be queued once per ascension", ErrBehaviorConflict, behaviorKey)
		}
	}

	slots, err := QueueSlotSummaryForPlayer(ctx, database, playerID, realmID)
	if err != nil {
		return err
	}
	if slots.Used >= slots.Total {
		return fmt.Errorf("%w: %d/%d used", ErrQueueSlotsFull, slots.Used, slots.Total)
	}

	if hasConflict, err := hasActiveExclusiveConflict(ctx, database, dal.BehaviorInstance{
		RealmID:   realmID,
		ActorType: ActorPlayer,
		ActorID:   playerID,
	}, definition); err != nil {
		return err
	} else if hasConflict {
		group := behaviorExclusiveGroup(definition)
		if group == "" {
			group = "exclusive"
		}
		return fmt.Errorf("%w: cannot queue %s while another %s behavior is active", ErrBehaviorConflict, behaviorKey, group)
	}

	payload := behaviorRuntimePayload{Mode: mode}
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

	if mode == behaviorModeRepeat || mode == behaviorModeRepeatUntil {
		repeatIntervalMinutes := int64(0)
		payload.RepeatIntervalMinutes = repeatIntervalMinutes
	}

	if mode == behaviorModeRepeatUntil {
		if options.RepeatUntilTick <= currentTick {
			return fmt.Errorf("repeatUntil must resolve to a future tick")
		}
		payload.RepeatUntilTick = options.RepeatUntilTick
	}

	payloadJSON, err := marshalBehaviorRuntimePayload(payload)
	if err != nil {
		return err
	}

	instance := dal.BehaviorInstance{
		RealmID:           realmID,
		Key:               behaviorKey,
		ActorType:         ActorPlayer,
		ActorID:           playerID,
		State:             behaviorQueued,
		ScheduledAtTick:   currentTick,
		DurationMinutes:   definition.DurationMinutes,
		RepeatIntervalMin: payload.RepeatIntervalMinutes,
		PayloadJSON:       payloadJSON,
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
		if err := processMarketOrdersAtTick(ctx, tx, currentTick, realmID); err != nil {
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
	coreStats, derivedStats := splitCoreAndDerivedStats(stats)

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
		Where("realm_id = ? AND state IN ?", realmID, []string{behaviorQueued, behaviorActive, behaviorCompleted, behaviorCancelled, behaviorFailed}).
		Order("id DESC").
		Limit(20).
		Find(&behaviors).Error; err != nil {
		return WorldSnapshot{}, err
	}
	currentTick, err := CurrentWorldTickForRealm(ctx, database, realmID)
	if err != nil {
		return WorldSnapshot{}, err
	}

	views := make([]BehaviorView, 0, len(behaviors))
	for _, behavior := range behaviors {
		payload := parseBehaviorRuntimePayload(behavior.PayloadJSON)
		waitReason := ""
		if behavior.State == behaviorQueued {
			if definition, ok := GetBehaviorDefinition(behavior.Key); ok {
				waitReason = queuedBehaviorWaitReason(ctx, database, behavior, definition, payload, currentTick)
			}
		}

		views = append(views, BehaviorView{
			ID:                        behavior.ID,
			Key:                       behavior.Key,
			ActorType:                 behavior.ActorType,
			ActorID:                   behavior.ActorID,
			State:                     behavior.State,
			Mode:                      payload.Mode,
			RepeatIntervalMinutes:     maxInt64(behavior.RepeatIntervalMin, payload.RepeatIntervalMinutes),
			RepeatUntilTick:           payload.RepeatUntilTick,
			ScheduledAt:               behavior.ScheduledAtTick,
			StartedAt:                 behavior.StartedAtTick,
			CompletesAt:               behavior.CompletesAtTick,
			DurationMinute:            behavior.DurationMinutes,
			MarketWaitDurationMinutes: payload.MarketWaitDurationMinutes,
			MarketWaitUntilTick:       payload.MarketWaitUntilTick,
			ResultMessage:             behavior.ResultMessage,
			FailureReason:             behavior.FailureReason,
			WaitReason:                waitReason,
			Spent:                     payload.Spent,
			Gained:                    payload.Gained,
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
		CoreStats:      coreStats,
		DerivedStats:   derivedStats,
		Stats:          stats,
		MarketPrices:   marketPrices,
		Behaviors:      views,
		RecentEvents:   recentEvents,
		AscensionCount: ascension.Count,
		WealthBonusPct: ascension.WealthBonusPct,
		Ascension:      ascensionEligibility,
	}, nil
}

func splitCoreAndDerivedStats(stats map[string]int64) (map[string]int64, map[string]int64) {
	core := map[string]int64{
		statStrength:  stats[statStrength],
		statSocial:    stats[statSocial],
		statFinancial: stats[statFinancial],
		statEndurance: stats[statEndurance],
	}

	derived := map[string]int64{
		statStamina:             stats[statStamina],
		statMaxStamina:          stats[statMaxStamina],
		statStaminaRecoveryRate: stats[statStaminaRecoveryRate],
		statTradingAptitude:     stats[statTradingAptitude],
	}

	return core, derived
}

func deriveStaminaByEndurance(endurance int64) (int64, int64) {
	if endurance < 0 {
		endurance = 0
	}

	maxStamina := defaultMaxStamina + (endurance * maxStaminaPerEndurancePoint)
	if maxStamina < defaultMaxStamina {
		maxStamina = defaultMaxStamina
	}

	recoveryRate := defaultStaminaRecoveryRate + (endurance / recoveryRatePerEnduranceDivisor)
	if recoveryRate < defaultStaminaRecoveryRate {
		recoveryRate = defaultStaminaRecoveryRate
	}

	return maxStamina, recoveryRate
}

func inferEnduranceFromLegacyStats(rawStats map[string]int64) int64 {
	legacyMax := rawStats[statMaxStamina]
	legacyRecovery := rawStats[statStaminaRecoveryRate]

	fromMax := int64(0)
	if legacyMax > defaultMaxStamina {
		fromMax = (legacyMax - defaultMaxStamina) / maxStaminaPerEndurancePoint
	}

	fromRecovery := int64(0)
	if legacyRecovery > defaultStaminaRecoveryRate {
		fromRecovery = (legacyRecovery - defaultStaminaRecoveryRate) * recoveryRatePerEnduranceDivisor
	}

	if fromRecovery > fromMax {
		return fromRecovery
	}

	return fromMax
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
		if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&dal.PlayerUpgrade{}).Error; err != nil {
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
		if err := tx.WithContext(ctx).
			Where("realm_id = ? AND player_id = ?", realmID, playerID).
			Delete(&dal.PlayerUpgrade{}).Error; err != nil {
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

func behaviorModeSupported(definition BehaviorDefinition, mode string) bool {
	supported := definition.ScheduleModes
	if len(supported) == 0 {
		supported = []string{behaviorModeOnce, behaviorModeRepeat, behaviorModeRepeatUntil}
	}

	normalizedMode := strings.ToLower(strings.TrimSpace(mode))
	for _, candidate := range supported {
		if strings.ToLower(strings.TrimSpace(candidate)) == normalizedMode {
			return true
		}
	}

	return false
}

func hasConsumedSingleUseBehavior(ctx context.Context, database *gorm.DB, playerID uint, behaviorKey string, realmID uint) (bool, error) {
	var count int64
	err := database.WithContext(ctx).
		Model(&dal.BehaviorInstance{}).
		Where("realm_id = ? AND actor_type = ? AND actor_id = ? AND key = ? AND state IN ?", realmID, ActorPlayer, playerID, behaviorKey, []string{behaviorQueued, behaviorActive, behaviorCompleted}).
		Count(&count).Error
	if err != nil {
		return false, err
	}

	return count > 0, nil
}

func loadPlayerStats(ctx context.Context, database *gorm.DB, playerID uint, realmID uint) (map[string]int64, error) {
	realmID = normalizeRealmID(realmID)
	rawStats := map[string]int64{}
	rows := make([]dal.PlayerStat, 0)
	if err := database.WithContext(ctx).
		Where("realm_id = ? AND player_id = ?", realmID, playerID).
		Find(&rows).Error; err != nil {
		return nil, err
	}

	for _, row := range rows {
		rawStats[row.StatKey] = row.Value
	}

	strength := rawStats[statStrength]
	if strength < 0 {
		strength = 0
	}

	social := rawStats[statSocial]
	if social < 0 {
		social = 0
	}

	financial := rawStats[statFinancial]
	if financial < 0 {
		financial = 0
	}

	endurance, foundEndurance := rawStats[statEndurance]
	if !foundEndurance {
		endurance = inferEnduranceFromLegacyStats(rawStats)
	}
	if endurance < 0 {
		endurance = 0
	}

	maxStamina, recoveryRate := deriveStaminaByEndurance(endurance)

	stamina, foundStamina := rawStats[statStamina]
	if !foundStamina {
		stamina = maxStamina
	}
	if stamina < 0 {
		stamina = 0
	}
	if stamina > maxStamina {
		stamina = maxStamina
	}

	recoveryCarry := rawStats[statStaminaRecoveryCarry]
	if recoveryCarry < 0 {
		recoveryCarry = 0
	}

	stats := map[string]int64{
		statStrength:             strength,
		statSocial:               social,
		statFinancial:            financial,
		statEndurance:            endurance,
		statStamina:              stamina,
		statMaxStamina:           maxStamina,
		statStaminaRecoveryRate:  recoveryRate,
		statStaminaRecoveryCarry: recoveryCarry,
		statTradingAptitude:      social + financial,
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

	endurance, err := getPlayerStatValueOrDefault(ctx, tx, behavior.ActorID, statEndurance, 0, behavior.RealmID)
	if err != nil {
		return err
	}
	maxStamina, _ := deriveStaminaByEndurance(endurance)

	current, err := getPlayerStatValueOrDefault(ctx, tx, behavior.ActorID, statStamina, maxStamina, behavior.RealmID)
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
		endurance, err := getPlayerStatValueOrDefault(ctx, tx, characterPlayer.PlayerID, statEndurance, 0, realmID)
		if err != nil {
			return err
		}
		maxStamina, recoveryRate := deriveStaminaByEndurance(endurance)

		currentStamina, err := getPlayerStatValueOrDefault(ctx, tx, characterPlayer.PlayerID, statStamina, maxStamina, realmID)
		if err != nil {
			return err
		}
		if currentStamina < 0 {
			currentStamina = 0
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
			if err := markBehaviorFailedAtTick(ctx, tx, behavior.ID, "unknown behavior definition", currentTick); err != nil {
				return err
			}
			continue
		}

		payload := parseBehaviorRuntimePayload(behavior.PayloadJSON)

		hasConflict, err := hasActiveExclusiveConflict(ctx, tx, behavior, definition)
		if err != nil {
			return err
		}
		if hasConflict {
			continue
		}

		if definition.RequiresMarketOpen && !IsMarketOpen(currentTick) {
			if payload.MarketWaitUntilTick > 0 && currentTick >= payload.MarketWaitUntilTick {
				reason := fmt.Sprintf("market did not open before timeout (waited %d minutes)", payload.MarketWaitDurationMinutes)
				if err := markBehaviorFailedAtTick(ctx, tx, behavior.ID, reason, currentTick); err != nil {
					return err
				}
			}
			continue
		}

		if definition.RequiresNight && !IsNightTick(currentTick) {
			continue
		}

		if err := validateRequirements(ctx, tx, behavior.ActorID, definition.Requirements, behavior.RealmID); err != nil {
			if shouldAutoCompleteQueuedBehavior(payload, err) {
				if err := markBehaviorCompletedAtTick(ctx, tx, behavior.ID, "Loop completed: requirements are no longer met.", currentTick); err != nil {
					return err
				}
				continue
			}
			if err := markBehaviorFailedAtTick(ctx, tx, behavior.ID, err.Error(), currentTick); err != nil {
				return err
			}
			continue
		}

		if err := applyItemDelta(ctx, tx, behavior.ActorID, behavior.RealmID, negateMap(definition.Costs)); err != nil {
			if shouldAutoCompleteQueuedBehavior(payload, err) {
				if err := markBehaviorCompletedAtTick(ctx, tx, behavior.ID, "Loop completed: requirements are no longer met.", currentTick); err != nil {
					return err
				}
				continue
			}
			if err := markBehaviorFailedAtTick(ctx, tx, behavior.ID, err.Error(), currentTick); err != nil {
				return err
			}
			continue
		}

		if err := consumeBehaviorStamina(ctx, tx, behavior, definition); err != nil {
			if isInsufficientStaminaError(err) {
				// Keep the behavior queued so it can auto-resume once stamina recovers.
				continue
			}
			if err := markBehaviorFailedAtTick(ctx, tx, behavior.ID, err.Error(), currentTick); err != nil {
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
	type batchSummary struct {
		CompletedRuns int64
		Spent         map[string]int64
		Gained        map[string]int64
	}

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
	batchByPlayerID := map[uint]*batchSummary{}

	for _, behavior := range active {
		definition, ok := GetBehaviorDefinition(behavior.Key)
		if !ok {
			if err := markBehaviorFailedAtTick(ctx, tx, behavior.ID, "unknown behavior definition", currentTick); err != nil {
				return err
			}
			continue
		}

		payload := parseBehaviorRuntimePayload(behavior.PayloadJSON)

		if definition.RequiresMarketOpen && !IsMarketOpen(currentTick) {
			if payload.MarketWaitUntilTick > 0 && currentTick >= payload.MarketWaitUntilTick {
				reason := fmt.Sprintf("market did not open before timeout (waited %d minutes)", payload.MarketWaitDurationMinutes)
				if err := markBehaviorFailedAtTick(ctx, tx, behavior.ID, reason, currentTick); err != nil {
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
			if err := markBehaviorFailedAtTick(ctx, tx, behavior.ID, err.Error(), currentTick); err != nil {
				return err
			}
			continue
		}

		outputs = applyAscensionBonus(outputs, ascension.WealthBonusPct)
		if err := applyItemDelta(ctx, tx, behavior.ActorID, behavior.RealmID, outputs); err != nil {
			if err := markBehaviorFailedAtTick(ctx, tx, behavior.ID, err.Error(), currentTick); err != nil {
				return err
			}
			continue
		}

		if err := grantUnlocks(ctx, tx, behavior.ActorID, definition.GrantsUnlocks, behavior.RealmID); err != nil {
			if err := markBehaviorFailedAtTick(ctx, tx, behavior.ID, err.Error(), currentTick); err != nil {
				return err
			}
			continue
		}

		if err := applyStatDelta(ctx, tx, behavior.ActorID, behavior.RealmID, definition.StatDeltas); err != nil {
			if err := markBehaviorFailedAtTick(ctx, tx, behavior.ID, err.Error(), currentTick); err != nil {
				return err
			}
			continue
		}

		restMessage, restRecovered, err := applyRestRecoveryOnCompletion(ctx, tx, behavior, definition)
		if err != nil {
			if err := markBehaviorFailedAtTick(ctx, tx, behavior.ID, err.Error(), currentTick); err != nil {
				return err
			}
			continue
		}

		marketMessage, err := applyMarketEffects(ctx, tx, behavior, definition, currentTick)
		if err != nil {
			if err := markBehaviorFailedAtTick(ctx, tx, behavior.ID, err.Error(), currentTick); err != nil {
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
		if restMessage != "" {
			message = strings.TrimSpace(message + " " + restMessage)
		}
		if marketMessage != "" {
			message = strings.TrimSpace(message + " " + marketMessage)
		}

		resolvedSpent := realizedBehaviorSpent(definition)
		resolvedGained := realizedBehaviorGained(outputs, definition.StatDeltas, restRecovered)
		payload.Spent = resolvedSpent
		payload.Gained = resolvedGained
		payloadJSON, err := marshalBehaviorRuntimePayload(payload)
		if err != nil {
			return err
		}

		if err := tx.WithContext(ctx).Model(&dal.BehaviorInstance{}).
			Where("id = ?", behavior.ID).
			Updates(map[string]any{
				"state":          behaviorCompleted,
				"result_message": message,
				"failure_reason": "",
				"payload_json":   payloadJSON,
			}).Error; err != nil {
			return err
		}

		if err := createWorldEvent(ctx, tx, currentTick, "behavior_completed", message, behavior.ActorType, behavior.ID, behavior.RealmID); err != nil {
			return err
		}

		repeatIntervalMinutes := behavior.RepeatIntervalMin
		if repeatIntervalMinutes <= 0 {
			repeatIntervalMinutes = payload.RepeatIntervalMinutes
		}

		if behavior.ActorType == ActorPlayer && behavior.ActorID != 0 {
			summary := batchByPlayerID[behavior.ActorID]
			if summary == nil {
				summary = &batchSummary{Spent: map[string]int64{}, Gained: map[string]int64{}}
				batchByPlayerID[behavior.ActorID] = summary
			}
			summary.CompletedRuns++
			addInt64Map(summary.Spent, resolvedSpent)
			addInt64Map(summary.Gained, resolvedGained)
		}

		shouldQueueNext := shouldQueueNextBehavior(payload, currentTick, repeatIntervalMinutes, behavior.Key, restRecovered)

		if shouldQueueNext {
			nextPayload := payload
			nextPayload.Spent = nil
			nextPayload.Gained = nil
			if definition.RequiresMarketOpen {
				waitDuration := payload.MarketWaitDurationMinutes
				if waitDuration <= 0 {
					waitDuration = defaultMarketWaitDurationMinutes
				}
				if waitDuration > maxMarketWaitDurationMinutes {
					waitDuration = maxMarketWaitDurationMinutes
				}

				nextPayload.MarketWaitDurationMinutes = waitDuration
				nextPayload.MarketWaitUntilTick = currentTick + waitDuration
			}

			nextPayloadJSON, err := marshalBehaviorRuntimePayload(nextPayload)
			if err != nil {
				return err
			}

			next := dal.BehaviorInstance{
				RealmID:           behavior.RealmID,
				Key:               behavior.Key,
				ActorType:         behavior.ActorType,
				ActorID:           behavior.ActorID,
				State:             behaviorQueued,
				ScheduledAtTick:   currentTick + maxInt64(repeatIntervalMinutes, 0),
				DurationMinutes:   definition.DurationMinutes,
				PayloadJSON:       nextPayloadJSON,
				RepeatIntervalMin: repeatIntervalMinutes,
			}
			if err := tx.WithContext(ctx).Create(&next).Error; err != nil {
				return err
			}
		}
	}

	for playerID, summary := range batchByPlayerID {
		if summary.CompletedRuns <= 1 {
			continue
		}
		message := fmt.Sprintf("%d queued runs finished. Spent: %s. Gained: %s.", summary.CompletedRuns, formatSummaryMap(summary.Spent), formatSummaryMap(summary.Gained))
		if err := createWorldEvent(ctx, tx, currentTick, "behavior_batch_completed", message, ActorPlayer, playerID, realmID); err != nil {
			return err
		}
	}

	return nil
}

func applyRestRecoveryOnCompletion(ctx context.Context, tx *gorm.DB, behavior dal.BehaviorInstance, definition BehaviorDefinition) (string, int64, error) {
	if behavior.ActorType != ActorPlayer || behavior.ActorID == 0 || behavior.Key != restBehaviorKey {
		return "", 0, nil
	}

	endurance, err := getPlayerStatValueOrDefault(ctx, tx, behavior.ActorID, statEndurance, 0, behavior.RealmID)
	if err != nil {
		return "", 0, err
	}
	maxStamina, recoveryRate := deriveStaminaByEndurance(endurance)
	currentStamina, err := getPlayerStatValueOrDefault(ctx, tx, behavior.ActorID, statStamina, maxStamina, behavior.RealmID)
	if err != nil {
		return "", 0, err
	}

	recovered := computeRestRecoveryPoints(recoveryRate, definition.DurationMinutes)
	if recovered <= 0 {
		return "", 0, nil
	}

	nextStamina := currentStamina + recovered
	if nextStamina > maxStamina {
		nextStamina = maxStamina
	}

	actualRecovered := nextStamina - currentStamina
	if actualRecovered <= 0 {
		return "Stamina is already full.", 0, nil
	}

	if err := setPlayerStatValue(ctx, tx, behavior.ActorID, statStamina, nextStamina, behavior.RealmID); err != nil {
		return "", 0, err
	}

	return fmt.Sprintf("Rest recovery restored %d stamina.", actualRecovered), actualRecovered, nil
}

func computeRestRecoveryPoints(recoveryRate int64, durationMinutes int64) int64 {
	if durationMinutes <= 0 {
		durationMinutes = 1
	}
	if recoveryRate < 0 {
		recoveryRate = 0
	}

	recovered := (recoveryRate * durationMinutes * restRecoveryMultiplier) / 60
	if recovered < restMinimumRecoveryPoints {
		recovered = restMinimumRecoveryPoints
	}
	return recovered
}

func behaviorExclusiveGroup(definition BehaviorDefinition) string {
	return strings.ToLower(strings.TrimSpace(definition.ExclusiveGroup))
}

func behaviorDefinitionsConflict(left BehaviorDefinition, right BehaviorDefinition) bool {
	leftGroup := behaviorExclusiveGroup(left)
	rightGroup := behaviorExclusiveGroup(right)
	if leftGroup == "" || rightGroup == "" {
		return false
	}
	return leftGroup == rightGroup
}

func hasActiveExclusiveConflict(ctx context.Context, tx *gorm.DB, behavior dal.BehaviorInstance, definition BehaviorDefinition) (bool, error) {
	if behaviorExclusiveGroup(definition) == "" {
		return false, nil
	}

	active := make([]dal.BehaviorInstance, 0)
	if err := tx.WithContext(ctx).
		Where("realm_id = ? AND actor_type = ? AND actor_id = ? AND state = ?", behavior.RealmID, behavior.ActorType, behavior.ActorID, behaviorActive).
		Find(&active).Error; err != nil {
		return false, err
	}

	return hasExclusiveConflictWithActiveBehaviorKeys(definition, behavior.ID, active), nil
}

func hasExclusiveConflictWithActiveBehaviorKeys(definition BehaviorDefinition, candidateBehaviorID uint, active []dal.BehaviorInstance) bool {
	for _, activeBehavior := range active {
		if activeBehavior.ID == candidateBehaviorID {
			continue
		}

		activeDefinition, ok := GetBehaviorDefinition(activeBehavior.Key)
		if !ok {
			continue
		}

		if behaviorDefinitionsConflict(definition, activeDefinition) {
			return true
		}
	}

	return false
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
	return markBehaviorFailedAtTick(ctx, tx, behaviorID, reason, 0)
}

func markBehaviorCompletedAtTick(ctx context.Context, tx *gorm.DB, behaviorID uint, message string, completedAtTick int64) error {
	message = strings.TrimSpace(message)
	if message == "" {
		message = "Behavior completed."
	}
	updates := map[string]any{"state": behaviorCompleted, "result_message": message, "failure_reason": ""}
	if completedAtTick > 0 {
		updates["completes_at_tick"] = completedAtTick
	}
	return tx.WithContext(ctx).Model(&dal.BehaviorInstance{}).
		Where("id = ?", behaviorID).
		Updates(updates).Error
}

func markBehaviorFailedAtTick(ctx context.Context, tx *gorm.DB, behaviorID uint, reason string, failedAtTick int64) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "behavior failed"
	}
	updates := map[string]any{"state": behaviorFailed, "failure_reason": reason}
	if failedAtTick > 0 {
		updates["completes_at_tick"] = failedAtTick
	}
	return tx.WithContext(ctx).Model(&dal.BehaviorInstance{}).
		Where("id = ?", behaviorID).
		Updates(updates).Error
}

func isInsufficientStaminaError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "insufficient stamina")
}

func hasSufficientBehaviorStamina(ctx context.Context, tx *gorm.DB, behavior dal.BehaviorInstance, definition BehaviorDefinition) (bool, error) {
	if behavior.ActorType != ActorPlayer || behavior.ActorID == 0 || definition.StaminaCost <= 0 {
		return true, nil
	}

	endurance, err := getPlayerStatValueOrDefault(ctx, tx, behavior.ActorID, statEndurance, 0, behavior.RealmID)
	if err != nil {
		return false, err
	}
	maxStamina, _ := deriveStaminaByEndurance(endurance)

	current, err := getPlayerStatValueOrDefault(ctx, tx, behavior.ActorID, statStamina, maxStamina, behavior.RealmID)
	if err != nil {
		return false, err
	}

	return current >= definition.StaminaCost, nil
}

func queuedBehaviorWaitReason(ctx context.Context, database *gorm.DB, behavior dal.BehaviorInstance, definition BehaviorDefinition, payload behaviorRuntimePayload, currentTick int64) string {
	if behavior.ScheduledAtTick > currentTick {
		return fmt.Sprintf("Scheduled for tick %d.", behavior.ScheduledAtTick)
	}

	if definition.RequiresMarketOpen && !IsMarketOpen(currentTick) {
		if payload.MarketWaitUntilTick > 0 {
			return fmt.Sprintf("Waiting for market open before tick %d.", payload.MarketWaitUntilTick)
		}
		return "Waiting for market to open."
	}

	if definition.RequiresNight && !IsNightTick(currentTick) {
		return "Waiting for night hours."
	}

	hasConflict, err := hasActiveExclusiveConflict(ctx, database, behavior, definition)
	if err == nil && hasConflict {
		group := behaviorExclusiveGroup(definition)
		if group == "" {
			return "Waiting for an exclusive behavior slot."
		}
		return fmt.Sprintf("Waiting for %s slot.", HumanizeIdentifier(group))
	}

	if err := validateRequirements(ctx, database, behavior.ActorID, definition.Requirements, behavior.RealmID); err != nil {
		return err.Error()
	}

	hasStamina, err := hasSufficientBehaviorStamina(ctx, database, behavior, definition)
	if err == nil && !hasStamina {
		return fmt.Sprintf("Waiting for %s.", HumanizeIdentifier(statStamina))
	}

	return "Queued and waiting for activation."
}

func shouldAutoCompleteQueuedBehavior(payload behaviorRuntimePayload, err error) bool {
	if err == nil {
		return false
	}
	if payload.Mode != behaviorModeRepeat && payload.Mode != behaviorModeRepeatUntil {
		return false
	}

	message := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.HasPrefix(message, "missing ") || strings.HasPrefix(message, "insufficient ")
}

func shouldQueueNextBehavior(payload behaviorRuntimePayload, currentTick int64, repeatIntervalMinutes int64, behaviorKey string, restRecovered int64) bool {
	shouldQueueNext := false
	switch payload.Mode {
	case behaviorModeOnce:
		shouldQueueNext = false
	case behaviorModeRepeat:
		shouldQueueNext = true
	case behaviorModeRepeatUntil:
		shouldQueueNext = payload.RepeatUntilTick == 0 || currentTick < payload.RepeatUntilTick
	default:
		shouldQueueNext = repeatIntervalMinutes > 0
	}

	if shouldQueueNext && behaviorKey == restBehaviorKey && restRecovered <= 0 {
		return false
	}

	return shouldQueueNext
}

func CancelPlayerBehavior(ctx context.Context, database *gorm.DB, playerID uint, behaviorID uint, currentTick int64, realmID uint) (BehaviorView, error) {
	realmID = normalizeRealmID(realmID)
	view := BehaviorView{}
	err := database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		instance := dal.BehaviorInstance{}
		result := tx.WithContext(ctx).
			Where("id = ? AND realm_id = ? AND actor_type = ? AND actor_id = ?", behaviorID, realmID, ActorPlayer, playerID).
			Limit(1).
			Find(&instance)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return fmt.Errorf("%w: behavior instance not found", ErrBehaviorNotCancelable)
		}
		if instance.State != behaviorQueued && instance.State != behaviorActive {
			return fmt.Errorf("%w: behavior state %s", ErrBehaviorNotCancelable, instance.State)
		}

		definition, ok := GetBehaviorDefinition(instance.Key)
		if !ok {
			return fmt.Errorf("unknown behavior definition")
		}

		if instance.State == behaviorActive {
			if err := applyItemDelta(ctx, tx, instance.ActorID, instance.RealmID, definition.Costs); err != nil {
				return err
			}
			if definition.StaminaCost > 0 {
				endurance, err := getPlayerStatValueOrDefault(ctx, tx, instance.ActorID, statEndurance, 0, instance.RealmID)
				if err != nil {
					return err
				}
				maxStamina, _ := deriveStaminaByEndurance(endurance)
				currentStamina, err := getPlayerStatValueOrDefault(ctx, tx, instance.ActorID, statStamina, maxStamina, instance.RealmID)
				if err != nil {
					return err
				}
				refunded := currentStamina + definition.StaminaCost
				if refunded > maxStamina {
					refunded = maxStamina
				}
				if err := setPlayerStatValue(ctx, tx, instance.ActorID, statStamina, refunded, instance.RealmID); err != nil {
					return err
				}
			}
		}

		payload := parseBehaviorRuntimePayload(instance.PayloadJSON)
		if err := tx.WithContext(ctx).Model(&dal.BehaviorInstance{}).
			Where("id = ?", instance.ID).
			Updates(map[string]any{
				"state":             behaviorCancelled,
				"failure_reason":    "",
				"result_message":    "Cancelled by player.",
				"completes_at_tick": currentTick,
				"payload_json":      mustMarshalBehaviorRuntimePayload(payload),
			}).Error; err != nil {
			return err
		}

		if err := createWorldEvent(ctx, tx, currentTick, "behavior_cancelled", fmt.Sprintf("%s cancelled.", BehaviorDisplayName(definition)), ActorPlayer, instance.ID, instance.RealmID); err != nil {
			return err
		}

		view = BehaviorView{
			ID:                    instance.ID,
			Key:                   instance.Key,
			ActorType:             instance.ActorType,
			ActorID:               instance.ActorID,
			State:                 behaviorCancelled,
			Mode:                  payload.Mode,
			RepeatIntervalMinutes: maxInt64(instance.RepeatIntervalMin, payload.RepeatIntervalMinutes),
			RepeatUntilTick:       payload.RepeatUntilTick,
			ScheduledAt:           instance.ScheduledAtTick,
			StartedAt:             instance.StartedAtTick,
			CompletesAt:           currentTick,
			DurationMinute:        instance.DurationMinutes,
			ResultMessage:         "Cancelled by player.",
			Spent:                 payload.Spent,
			Gained:                payload.Gained,
		}

		return nil
	})
	if err != nil {
		return BehaviorView{}, err
	}

	return view, nil
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

	budgetedDelta, budgetErr := applyAdaptiveImpactBudget(ctx, tx, item, delta, source, currentTick, realmID)
	if budgetErr != nil {
		return 0, budgetErr
	}
	if budgetedDelta == 0 {
		if entry.Price < 1 {
			entry.Price = 1
		}
		return entry.Price, nil
	}

	entry.Price = entry.Price + budgetedDelta
	if entry.Price < 1 {
		entry.Price = 1
	}
	if entry.Price > 500 {
		entry.Price = 500
	}
	entry.LastDelta = budgetedDelta
	entry.LastSource = source
	entry.UpdatedTick = currentTick

	if err := tx.WithContext(ctx).Save(&entry).Error; err != nil {
		return 0, err
	}

	if err := appendMarketHistory(ctx, tx, item, currentTick, entry.Price, budgetedDelta, source, realmID); err != nil {
		return 0, err
	}

	return entry.Price, nil
}

func applyAdaptiveImpactBudget(ctx context.Context, tx *gorm.DB, item string, proposedDelta int64, source string, currentTick int64, realmID uint) (int64, error) {
	if proposedDelta == 0 || source == "bootstrap" {
		return proposedDelta, nil
	}

	windowStart := currentTick - marketImpactBudgetWindowTicks
	participants, err := estimateActiveMarketParticipants(ctx, tx, realmID, windowStart)
	if err != nil {
		return 0, err
	}

	liquidity := dal.MarketLiquidity{}
	if err := tx.WithContext(ctx).
		Where("realm_id = ? AND item_key = ?", realmID, item).
		Limit(1).
		Find(&liquidity).Error; err != nil {
		return 0, err
	}

	directionalBudget := marketImpactMinBudgetPerDirection + participants
	if liquidity.Quantity > 0 {
		directionalBudget += maxInt64(liquidity.Quantity/marketImpactLiquidityDivisor, 0)
	}
	if source == marketStorytellerDeltaSource {
		directionalBudget += marketImpactStorytellerBonus
	} else if strings.HasPrefix(source, "world_") {
		directionalBudget += marketImpactWorldEventBonus
	}
	if directionalBudget < marketImpactMinBudgetPerDirection {
		directionalBudget = marketImpactMinBudgetPerDirection
	}
	if directionalBudget > marketImpactMaxBudgetPerDirection {
		directionalBudget = marketImpactMaxBudgetPerDirection
	}

	used := struct {
		Positive int64
		Negative int64
	}{}
	if err := tx.WithContext(ctx).
		Model(&dal.MarketHistory{}).
		Select(
			"COALESCE(SUM(CASE WHEN delta > 0 THEN delta ELSE 0 END), 0) AS positive, COALESCE(SUM(CASE WHEN delta < 0 THEN -delta ELSE 0 END), 0) AS negative",
		).
		Where("realm_id = ? AND item_key = ? AND tick >= ?", realmID, item, windowStart).
		Scan(&used).Error; err != nil {
		return 0, err
	}

	remainingPositive := directionalBudget - used.Positive
	remainingNegative := directionalBudget - used.Negative
	if proposedDelta > 0 {
		if remainingPositive <= 0 {
			return 0, nil
		}
		if proposedDelta > remainingPositive {
			return remainingPositive, nil
		}
		return proposedDelta, nil
	}

	absDelta := -proposedDelta
	if remainingNegative <= 0 {
		return 0, nil
	}
	if absDelta > remainingNegative {
		return -remainingNegative, nil
	}
	return proposedDelta, nil
}

func estimateActiveMarketParticipants(ctx context.Context, tx *gorm.DB, realmID uint, windowStart int64) (int64, error) {
	realmID = normalizeRealmID(realmID)

	var realmPopulation int64
	if err := tx.WithContext(ctx).
		Model(&dal.Character{}).
		Where("realm_id = ? AND status = ?", realmID, "active").
		Count(&realmPopulation).Error; err != nil {
		return 0, err
	}

	var openOrderParticipants int64
	if err := tx.WithContext(ctx).
		Model(&dal.MarketOrder{}).
		Distinct("player_id").
		Where("realm_id = ? AND state = ? AND player_id > 0", realmID, marketOrderStateOpen).
		Count(&openOrderParticipants).Error; err != nil {
		return 0, err
	}

	var recentTradeParticipants int64
	if err := tx.WithContext(ctx).
		Raw(
			"SELECT COUNT(*) FROM ("+
				"SELECT DISTINCT buyer_id AS pid FROM market_trades WHERE realm_id = ? AND tick >= ? AND buyer_type = 'player' AND buyer_id > 0 " +
				"UNION " +
				"SELECT DISTINCT seller_id AS pid FROM market_trades WHERE realm_id = ? AND tick >= ? AND seller_type = 'player' AND seller_id > 0"+
				") AS market_participants",
			realmID,
			windowStart,
			realmID,
			windowStart,
		).
		Scan(&recentTradeParticipants).Error; err != nil {
		return 0, err
	}

	participants := maxInt64(openOrderParticipants, recentTradeParticipants)
	populationBaseline := maxInt64(realmPopulation/marketImpactPopulationBaselineDiv, 1)
	if participants < populationBaseline {
		participants = populationBaseline
	}
	if participants < 1 {
		participants = 1
	}

	return participants, nil
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

func IsNightTick(currentTick int64) bool {
	minuteOfDay := positiveModulo(currentTick, minutesPerDay)
	return minuteOfDay >= nightStartMinute || minuteOfDay < nightEndMinute
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
	liquidityBySymbol, err := loadMarketLiquiditySnapshot(ctx, database, realmID)
	if err != nil {
		return MarketStatus{}, err
	}

	tickers := make([]MarketTickerView, 0, len(rows))
	for _, row := range rows {
		liquidity := liquidityBySymbol[row.ItemKey]
		if liquidity.Quantity > 0 {
			liquidity.CapEstimate = liquidity.Quantity * row.Price
		}
		movement, movementErr := loadMarketMovementStats(ctx, database, row.ItemKey, currentTick, realmID)
		if movementErr != nil {
			return MarketStatus{}, movementErr
		}
		tickers = append(tickers, MarketTickerView{
			Symbol:       row.ItemKey,
			Price:        row.Price,
			Delta:        row.LastDelta,
			LastSource:   row.LastSource,
			UpdatedTick:  row.UpdatedTick,
			SessionState: MarketSessionState(currentTick),
			Liquidity:    liquidity,
			Movement:     movement,
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

func GetMarketCandles(ctx context.Context, database *gorm.DB, symbol string, bucketTicks int64, limit int, realmID uint) ([]MarketCandleEntry, error) {
	realmID = normalizeRealmID(realmID)
	if err := ensureMarketHistorySeed(ctx, database, realmID); err != nil {
		return nil, err
	}

	if bucketTicks <= 0 {
		bucketTicks = 30
	}
	if bucketTicks > 24*60 {
		bucketTicks = 24 * 60
	}
	if limit <= 0 {
		limit = 120
	}
	if limit > 500 {
		limit = 500
	}

	symbol = strings.TrimSpace(symbol)
	if symbol == "" {
		return []MarketCandleEntry{}, nil
	}

	rows := make([]dal.MarketHistory, 0)
	if err := database.WithContext(ctx).
		Model(&dal.MarketHistory{}).
		Where("realm_id = ? AND item_key = ?", realmID, symbol).
		Order("tick ASC, id ASC").
		Limit(limit * 8).
		Find(&rows).Error; err != nil {
		return nil, err
	}

	if len(rows) == 0 {
		return []MarketCandleEntry{}, nil
	}

	byBucket := map[int64]*MarketCandleEntry{}
	for _, row := range rows {
		bucketStart := (row.Tick / bucketTicks) * bucketTicks
		entry, exists := byBucket[bucketStart]
		if !exists {
			byBucket[bucketStart] = &MarketCandleEntry{
				Symbol:          symbol,
				BucketStartTick: bucketStart,
				Open:            row.Price,
				High:            row.Price,
				Low:             row.Price,
				Close:           row.Price,
				Points:          1,
			}
			continue
		}

		if row.Price > entry.High {
			entry.High = row.Price
		}
		if row.Price < entry.Low {
			entry.Low = row.Price
		}
		entry.Close = row.Price
		entry.Points++
	}

	buckets := make([]int64, 0, len(byBucket))
	for bucket := range byBucket {
		buckets = append(buckets, bucket)
	}
	sort.Slice(buckets, func(i, j int) bool { return buckets[i] < buckets[j] })
	if len(buckets) > limit {
		buckets = buckets[len(buckets)-limit:]
	}

	candles := make([]MarketCandleEntry, 0, len(buckets))
	for _, bucket := range buckets {
		candles = append(candles, *byBucket[bucket])
	}

	return candles, nil
}

func loadMarketLiquiditySnapshot(ctx context.Context, database *gorm.DB, realmID uint) (map[string]MarketLiquidityView, error) {
	rows := make([]dal.MarketLiquidity, 0)
	if err := database.WithContext(ctx).Where("realm_id = ?", realmID).Find(&rows).Error; err != nil {
		return nil, err
	}

	result := make(map[string]MarketLiquidityView, len(rows))
	for _, row := range rows {
		rangeSpan := row.MaxQty - row.MinQty
		utilization := 0.0
		if rangeSpan > 0 {
			utilization = float64(row.Quantity-row.MinQty) * 100 / float64(rangeSpan)
		}
		result[row.ItemKey] = MarketLiquidityView{
			Quantity:         row.Quantity,
			BaselineQuantity: row.BaselineQty,
			MinQuantity:      row.MinQty,
			MaxQuantity:      row.MaxQty,
			UtilizationPct:   utilization,
			CapEstimate:      0,
			LastPressure:     row.LastPressure,
		}
	}

	return result, nil
}

func loadMarketMovementStats(ctx context.Context, database *gorm.DB, symbol string, currentTick int64, realmID uint) (MarketMovementStatsView, error) {
	const windowTicks int64 = 24 * 60
	windowStart := currentTick - windowTicks

	historyRows := make([]dal.MarketHistory, 0)
	if err := database.WithContext(ctx).
		Where("realm_id = ? AND item_key = ? AND tick >= ?", realmID, symbol, windowStart).
		Order("tick ASC, id ASC").
		Find(&historyRows).Error; err != nil {
		return MarketMovementStatsView{}, err
	}

	movement := MarketMovementStatsView{WindowTicks: windowTicks}
	if len(historyRows) > 0 {
		low := historyRows[0].Price
		high := historyRows[0].Price
		for _, row := range historyRows {
			if row.Price < low {
				low = row.Price
			}
			if row.Price > high {
				high = row.Price
			}
		}
		movement.WindowLow = low
		movement.WindowHigh = high
		movement.WindowRange = high - low
		movement.WindowChange = historyRows[len(historyRows)-1].Price - historyRows[0].Price
		for _, row := range historyRows {
			switch row.Source {
			case "npc_cycle":
				movement.NPCCycleMoves++
			case marketStorytellerDeltaSource:
				movement.StorytellerMoves++
			case "orderbook_trade":
				movement.OrderbookMoves++
			}
		}
	}

	var trades int64
	if err := database.WithContext(ctx).
		Model(&dal.MarketTrade{}).
		Where("realm_id = ? AND item_key = ? AND tick >= ?", realmID, symbol, windowStart).
		Count(&trades).Error; err != nil {
		return MarketMovementStatsView{}, err
	}
	movement.Trades = trades

	var npcTrades int64
	if err := database.WithContext(ctx).
		Model(&dal.MarketTrade{}).
		Where("realm_id = ? AND item_key = ? AND tick >= ? AND (buyer_type = ? OR seller_type = ?)", realmID, symbol, windowStart, "npc", "npc").
		Count(&npcTrades).Error; err != nil {
		return MarketMovementStatsView{}, err
	}
	movement.NPCParticipantTrades = npcTrades
	if trades > 0 {
		movement.NPCTradeSharePct = float64(npcTrades) * 100 / float64(trades)
	}

	return movement, nil
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
	payload.Mode = strings.ToLower(strings.TrimSpace(payload.Mode))
	if payload.Mode != "" && payload.Mode != behaviorModeOnce && payload.Mode != behaviorModeRepeat && payload.Mode != behaviorModeRepeatUntil {
		payload.Mode = behaviorModeOnce
	}
	if payload.RepeatIntervalMinutes < 0 {
		payload.RepeatIntervalMinutes = 0
	}
	if payload.RepeatUntilTick < 0 {
		payload.RepeatUntilTick = 0
	}
	payload.Spent = normalizeInt64Map(payload.Spent)
	payload.Gained = normalizeInt64Map(payload.Gained)
	return payload
}

func maxInt64(left int64, right int64) int64 {
	if left >= right {
		return left
	}
	return right
}

func marshalBehaviorRuntimePayload(payload behaviorRuntimePayload) (string, error) {
	payload.Spent = normalizeInt64Map(payload.Spent)
	payload.Gained = normalizeInt64Map(payload.Gained)
	if isEmptyBehaviorRuntimePayload(payload) {
		return "{}", nil
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	return string(encoded), nil
}

func mustMarshalBehaviorRuntimePayload(payload behaviorRuntimePayload) string {
	encoded, err := marshalBehaviorRuntimePayload(payload)
	if err != nil {
		return "{}"
	}
	return encoded
}

func isEmptyBehaviorRuntimePayload(payload behaviorRuntimePayload) bool {
	return payload.MarketWaitDurationMinutes == 0 &&
		payload.MarketWaitUntilTick == 0 &&
		payload.Mode == "" &&
		payload.RepeatIntervalMinutes == 0 &&
		payload.RepeatUntilTick == 0 &&
		len(payload.Spent) == 0 &&
		len(payload.Gained) == 0
}

func normalizeInt64Map(values map[string]int64) map[string]int64 {
	if len(values) == 0 {
		return nil
	}
	normalized := make(map[string]int64, len(values))
	for key, value := range values {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" || value == 0 {
			continue
		}
		normalized[trimmed] = value
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}

func realizedBehaviorSpent(definition BehaviorDefinition) map[string]int64 {
	spent := map[string]int64{}
	if definition.StaminaCost > 0 {
		spent[statStamina] = definition.StaminaCost
	}
	for itemKey, amount := range definition.Costs {
		if amount <= 0 {
			continue
		}
		spent[itemKey] = amount
	}
	return normalizeInt64Map(spent)
}

func realizedBehaviorGained(outputs map[string]int64, statDeltas map[string]int64, recoveredStamina int64) map[string]int64 {
	gained := map[string]int64{}
	addInt64Map(gained, outputs)
	addInt64Map(gained, statDeltas)
	if recoveredStamina > 0 {
		gained[statStamina] += recoveredStamina
	}
	return normalizeInt64Map(gained)
}

func addInt64Map(target map[string]int64, delta map[string]int64) {
	for key, amount := range delta {
		if amount == 0 {
			continue
		}
		target[key] += amount
		if target[key] == 0 {
			delete(target, key)
		}
	}
}

func formatSummaryMap(values map[string]int64) string {
	if len(values) == 0 {
		return "none"
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%d %s", values[key], HumanizeIdentifier(key)))
	}
	return strings.Join(parts, ", ")
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
		leftName := BehaviorDisplayName(sorted[i])
		rightName := BehaviorDisplayName(sorted[j])
		if leftName == rightName {
			return sorted[i].Key < sorted[j].Key
		}
		return leftName < rightName
	})
	return sorted
}

func SortUpgradeDefinitions(definitions []UpgradeDefinition) []UpgradeDefinition {
	sorted := make([]UpgradeDefinition, len(definitions))
	copy(sorted, definitions)
	sort.Slice(sorted, func(i, j int) bool {
		leftName := strings.TrimSpace(sorted[i].Name)
		rightName := strings.TrimSpace(sorted[j].Name)
		if leftName == "" {
			leftName = HumanizeIdentifier(sorted[i].Key)
		}
		if rightName == "" {
			rightName = HumanizeIdentifier(sorted[j].Key)
		}
		if leftName == rightName {
			return sorted[i].Key < sorted[j].Key
		}
		return leftName < rightName
	})
	return sorted
}
