package gameplay

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/asciifaceman/lived/pkg/dal"
	"gorm.io/gorm"
)

const defaultQueueSlotCap int64 = 1

var ErrUpgradeNotFound = errors.New("upgrade not found")
var ErrUpgradeMaxed = errors.New("upgrade already at maximum purchases")

// QueueSlotSummary describes current queue slot capacity and usage for a player.
type QueueSlotSummary struct {
	Total     int64 `json:"total"`
	Used      int64 `json:"used"`
	Available int64 `json:"available"`
}

func QueueSlotSummaryForPlayer(ctx context.Context, database *gorm.DB, playerID uint, realmID uint) (QueueSlotSummary, error) {
	realmID = normalizeRealmID(realmID)
	total, err := QueueSlotCapForPlayer(ctx, database, playerID, realmID)
	if err != nil {
		return QueueSlotSummary{}, err
	}

	used, err := queuedOrActiveBehaviorCountForPlayer(ctx, database, playerID, realmID)
	if err != nil {
		return QueueSlotSummary{}, err
	}

	available := total - used
	if available < 0 {
		available = 0
	}

	return QueueSlotSummary{Total: total, Used: used, Available: available}, nil
}

func QueueSlotCapForPlayer(ctx context.Context, database *gorm.DB, playerID uint, realmID uint) (int64, error) {
	realmID = normalizeRealmID(realmID)
	if playerID == 0 {
		return defaultQueueSlotCap, nil
	}

	counts, err := loadPlayerUpgradePurchaseCounts(ctx, database, playerID, realmID)
	if err != nil {
		return 0, err
	}

	total := defaultQueueSlotCap
	for key, purchased := range counts {
		definition, ok := GetUpgradeDefinition(key)
		if !ok {
			continue
		}
		total += scaledQueueSlotDeltaTotal(definition, purchased)
	}

	if total < 1 {
		total = 1
	}
	return total, nil
}

func PurchaseUpgradeForPlayer(ctx context.Context, database *gorm.DB, playerID uint, realmID uint, upgradeKey string) (int64, error) {
	realmID = normalizeRealmID(realmID)
	key := strings.TrimSpace(upgradeKey)
	if key == "" {
		return 0, fmt.Errorf("upgrade key is required")
	}

	definition, ok := GetUpgradeDefinition(key)
	if !ok {
		return 0, ErrUpgradeNotFound
	}

	var nextCount int64
	err := database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		entry := dal.PlayerUpgrade{}
		result := tx.WithContext(ctx).
			Where("realm_id = ? AND player_id = ? AND upgrade_key = ?", realmID, playerID, key).
			Limit(1).
			Find(&entry)
		if result.Error != nil {
			return result.Error
		}

		currentCount := int64(0)
		if result.RowsAffected > 0 {
			currentCount = entry.PurchaseCnt
		}

		if definition.MaxPurchases > 0 && currentCount >= definition.MaxPurchases {
			return ErrUpgradeMaxed
		}

		if err := validateRequirements(ctx, tx, playerID, definition.Requirements, realmID); err != nil {
			return err
		}

		scaledCosts := scaledInt64Map(definition.Costs, definition.CostScaling, currentCount)
		if err := applyItemDelta(ctx, tx, playerID, realmID, negateMap(scaledCosts)); err != nil {
			return err
		}

		scaledItems := scaledInt64Map(definition.Outputs.Items, definition.OutputScaling, currentCount)
		if err := applyItemDelta(ctx, tx, playerID, realmID, scaledItems); err != nil {
			return err
		}

		scaledStats := scaledInt64Map(definition.Outputs.StatDeltas, definition.OutputScaling, currentCount)
		if err := applyStatDelta(ctx, tx, playerID, realmID, scaledStats); err != nil {
			return err
		}

		if err := grantUnlocks(ctx, tx, playerID, definition.Outputs.Unlocks, realmID); err != nil {
			return err
		}

		nextCount = currentCount + 1
		if result.RowsAffected == 0 {
			entry = dal.PlayerUpgrade{RealmID: realmID, PlayerID: playerID, UpgradeKey: key, PurchaseCnt: nextCount}
			return tx.WithContext(ctx).Create(&entry).Error
		}

		entry.PurchaseCnt = nextCount
		return tx.WithContext(ctx).Save(&entry).Error
	})
	if err != nil {
		return 0, err
	}

	return nextCount, nil
}

func loadPlayerUpgradePurchaseCounts(ctx context.Context, database *gorm.DB, playerID uint, realmID uint) (map[string]int64, error) {
	rows := make([]dal.PlayerUpgrade, 0)
	if err := database.WithContext(ctx).
		Where("realm_id = ? AND player_id = ?", realmID, playerID).
		Find(&rows).Error; err != nil {
		return nil, err
	}

	counts := map[string]int64{}
	for _, row := range rows {
		if row.PurchaseCnt <= 0 {
			continue
		}
		counts[row.UpgradeKey] = row.PurchaseCnt
	}
	return counts, nil
}

func LoadPlayerUpgradeCounts(ctx context.Context, database *gorm.DB, playerID uint, realmID uint) (map[string]int64, error) {
	realmID = normalizeRealmID(realmID)
	if playerID == 0 {
		return map[string]int64{}, nil
	}
	return loadPlayerUpgradePurchaseCounts(ctx, database, playerID, realmID)
}

func queuedOrActiveBehaviorCountForPlayer(ctx context.Context, database *gorm.DB, playerID uint, realmID uint) (int64, error) {
	var count int64
	err := database.WithContext(ctx).
		Model(&dal.BehaviorInstance{}).
		Where("realm_id = ? AND actor_type = ? AND actor_id = ? AND state IN ?", realmID, ActorPlayer, playerID, []string{behaviorQueued, behaviorActive}).
		Count(&count).Error
	if err != nil {
		return 0, err
	}
	return count, nil
}

func scaledQueueSlotDeltaTotal(definition UpgradeDefinition, purchaseCount int64) int64 {
	if purchaseCount <= 0 || definition.Outputs.QueueSlotsDelta == 0 {
		return 0
	}

	total := int64(0)
	for level := int64(0); level < purchaseCount; level++ {
		delta := scaleInt64(definition.Outputs.QueueSlotsDelta, definition.OutputScaling, level)
		if delta > 0 {
			total += delta
		}
	}
	return total
}

func ProjectedUpgradeCosts(definition UpgradeDefinition, purchaseCount int64) map[string]int64 {
	return scaledInt64Map(definition.Costs, definition.CostScaling, purchaseCount)
}

func ProjectedUpgradeOutputs(definition UpgradeDefinition, purchaseCount int64) UpgradeOutputDefinition {
	return UpgradeOutputDefinition{
		QueueSlotsDelta: scaleInt64(definition.Outputs.QueueSlotsDelta, definition.OutputScaling, purchaseCount),
		Unlocks:         append([]string(nil), definition.Outputs.Unlocks...),
		Items:           scaledInt64Map(definition.Outputs.Items, definition.OutputScaling, purchaseCount),
		StatDeltas:      scaledInt64Map(definition.Outputs.StatDeltas, definition.OutputScaling, purchaseCount),
	}
}

func scaledInt64Map(base map[string]int64, factor float64, purchaseIndex int64) map[string]int64 {
	if len(base) == 0 {
		return map[string]int64{}
	}

	scaled := make(map[string]int64, len(base))
	for key, value := range base {
		next := scaleInt64(value, factor, purchaseIndex)
		if next == 0 {
			continue
		}
		scaled[key] = next
	}
	return scaled
}

func scaleInt64(base int64, factor float64, purchaseIndex int64) int64 {
	if base == 0 {
		return 0
	}
	if purchaseIndex <= 0 {
		return base
	}
	if factor <= 0 {
		factor = 1
	}
	scaled := float64(base) * math.Pow(factor, float64(purchaseIndex))
	if scaled < 0 {
		return int64(math.Ceil(scaled))
	}
	return int64(math.Round(scaled))
}
