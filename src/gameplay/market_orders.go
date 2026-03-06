package gameplay

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/asciifaceman/lived/pkg/dal"
	"gorm.io/gorm"
)

const (
	marketOrderSideBuy  = "buy"
	marketOrderSideSell = "sell"

	marketOrderStateOpen      = "open"
	marketOrderStateFilled    = "filled"
	marketOrderStateCancelled = "cancelled"
	marketOrderStateExpired   = "expired"

	marketCounterpartyPlayer = "player"
	marketCounterpartyNPC    = "npc"

	marketLotSize int64 = 100

	storytellerCyclePeriodTicks int64 = 3 * 24 * 60
	storytellerTickInterval     int64 = 30
	storytellerTrendWindowTicks int64 = 12 * 60
)

type PlaceMarketOrderRequest struct {
	ItemKey            string
	Side               string
	Quantity           int64
	LimitPrice         int64
	CancelAfterMinutes int64
	ManualCancelFeeBps int64
}

type MarketOrderView struct {
	ID               uint   `json:"id"`
	ItemKey          string `json:"itemKey"`
	Side             string `json:"side"`
	State            string `json:"state"`
	LimitPrice       int64  `json:"limitPrice"`
	QuantityTotal    int64  `json:"quantityTotal"`
	QuantityOpen     int64  `json:"quantityOpen"`
	EscrowCoins      int64  `json:"escrowCoins"`
	CancelAfterTick  int64  `json:"cancelAfterTick"`
	LastMatchedTick  int64  `json:"lastMatchedTick"`
	CancellationNote string `json:"cancellationNote"`
}

type MarketTradeView struct {
	ID         uint   `json:"id"`
	ItemKey    string `json:"itemKey"`
	Price      int64  `json:"price"`
	Quantity   int64  `json:"quantity"`
	Tick       int64  `json:"tick"`
	BuyerType  string `json:"buyerType"`
	BuyerID    uint   `json:"buyerId"`
	SellerType string `json:"sellerType"`
	SellerID   uint   `json:"sellerId"`
}

type MarketOrderBook struct {
	Symbol string            `json:"symbol"`
	Buys   []MarketOrderView `json:"buys"`
	Sells  []MarketOrderView `json:"sells"`
}

func PlaceMarketOrder(ctx context.Context, database *gorm.DB, playerID uint, realmID uint, currentTick int64, req PlaceMarketOrderRequest) (MarketOrderView, error) {
	realmID = normalizeRealmID(realmID)
	item := strings.TrimSpace(req.ItemKey)
	side := strings.ToLower(strings.TrimSpace(req.Side))
	if item == "" {
		return MarketOrderView{}, fmt.Errorf("itemKey is required")
	}
	if side != marketOrderSideBuy && side != marketOrderSideSell {
		return MarketOrderView{}, fmt.Errorf("side must be buy or sell")
	}
	if req.Quantity <= 0 || req.Quantity%marketLotSize != 0 {
		return MarketOrderView{}, fmt.Errorf("quantity must be a positive multiple of %d", marketLotSize)
	}
	if req.LimitPrice <= 0 {
		return MarketOrderView{}, fmt.Errorf("limitPrice must be positive")
	}
	if req.CancelAfterMinutes <= 0 {
		req.CancelAfterMinutes = 24 * 60
	}
	if req.ManualCancelFeeBps <= 0 {
		req.ManualCancelFeeBps = 50
	}

	cancelAfterTick := currentTick + req.CancelAfterMinutes
	created := dal.MarketOrder{}
	err := database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := ensureMarketDefaults(ctx, tx, currentTick, realmID); err != nil {
			return err
		}
		if err := ensureMarketLiquidityDefaults(ctx, tx, currentTick, realmID); err != nil {
			return err
		}

		order := dal.MarketOrder{
			RealmID:         realmID,
			PlayerID:        playerID,
			ItemKey:         item,
			Side:            side,
			State:           marketOrderStateOpen,
			LimitPrice:      req.LimitPrice,
			QuantityTotal:   req.Quantity,
			QuantityOpen:    req.Quantity,
			CancelAfterTick: cancelAfterTick,
		}

		if side == marketOrderSideBuy {
			escrow := req.Quantity * req.LimitPrice
			if escrow <= 0 {
				return fmt.Errorf("invalid buy escrow")
			}
			if err := applyItemDelta(ctx, tx, playerID, realmID, map[string]int64{"coins": -escrow}); err != nil {
				return err
			}
			order.EscrowCoins = escrow
		} else {
			if err := applyItemDelta(ctx, tx, playerID, realmID, map[string]int64{item: -req.Quantity}); err != nil {
				return err
			}
		}

		if err := tx.WithContext(ctx).Create(&order).Error; err != nil {
			return err
		}
		created = order
		return processMarketOrdersAtTick(ctx, tx, currentTick, realmID)
	})
	if err != nil {
		return MarketOrderView{}, err
	}

	return toMarketOrderView(created), nil
}

func CancelMarketOrder(ctx context.Context, database *gorm.DB, playerID uint, realmID uint, orderID uint, currentTick int64) (MarketOrderView, error) {
	realmID = normalizeRealmID(realmID)
	updated := dal.MarketOrder{}
	err := database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		order := dal.MarketOrder{}
		result := tx.WithContext(ctx).
			Where("id = ? AND realm_id = ? AND player_id = ?", orderID, realmID, playerID).
			Limit(1).
			Find(&order)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return fmt.Errorf("market order not found")
		}
		if order.State != marketOrderStateOpen {
			return fmt.Errorf("market order is not open")
		}

		isEarlyCancel := order.CancelAfterTick > 0 && currentTick < order.CancelAfterTick
		if err := settleOrderOnClose(ctx, tx, &order, marketOrderStateCancelled, isEarlyCancel, currentTick); err != nil {
			return err
		}
		updated = order
		return nil
	})
	if err != nil {
		return MarketOrderView{}, err
	}

	return toMarketOrderView(updated), nil
}

func ListMarketOrdersForPlayer(ctx context.Context, database *gorm.DB, playerID uint, realmID uint, state string, limit int) ([]MarketOrderView, error) {
	realmID = normalizeRealmID(realmID)
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	query := database.WithContext(ctx).
		Where("realm_id = ? AND player_id = ?", realmID, playerID)
	if strings.TrimSpace(state) != "" {
		query = query.Where("state = ?", strings.TrimSpace(state))
	}
	rows := make([]dal.MarketOrder, 0)
	if err := query.Order("id DESC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	views := make([]MarketOrderView, 0, len(rows))
	for _, row := range rows {
		views = append(views, toMarketOrderView(row))
	}
	return views, nil
}

func GetMarketOrderBook(ctx context.Context, database *gorm.DB, realmID uint, symbol string, depth int) (MarketOrderBook, error) {
	realmID = normalizeRealmID(realmID)
	symbol = strings.TrimSpace(symbol)
	if depth <= 0 {
		depth = 20
	}
	if depth > 200 {
		depth = 200
	}
	rows := make([]dal.MarketOrder, 0)
	query := database.WithContext(ctx).
		Where("realm_id = ? AND state = ?", realmID, marketOrderStateOpen)
	if symbol != "" {
		query = query.Where("item_key = ?", symbol)
	}
	if err := query.Order("id DESC").Limit(depth * 4).Find(&rows).Error; err != nil {
		return MarketOrderBook{}, err
	}

	buys := make([]MarketOrderView, 0, depth)
	sells := make([]MarketOrderView, 0, depth)
	for _, row := range rows {
		if symbol == "" {
			symbol = row.ItemKey
		}
		if row.QuantityOpen <= 0 {
			continue
		}
		if row.Side == marketOrderSideBuy && len(buys) < depth {
			buys = append(buys, toMarketOrderView(row))
		}
		if row.Side == marketOrderSideSell && len(sells) < depth {
			sells = append(sells, toMarketOrderView(row))
		}
	}

	sort.Slice(buys, func(i, j int) bool {
		if buys[i].LimitPrice == buys[j].LimitPrice {
			return buys[i].ID < buys[j].ID
		}
		return buys[i].LimitPrice > buys[j].LimitPrice
	})
	sort.Slice(sells, func(i, j int) bool {
		if sells[i].LimitPrice == sells[j].LimitPrice {
			return sells[i].ID < sells[j].ID
		}
		return sells[i].LimitPrice < sells[j].LimitPrice
	})

	return MarketOrderBook{Symbol: symbol, Buys: buys, Sells: sells}, nil
}

func ListRecentMarketTrades(ctx context.Context, database *gorm.DB, realmID uint, symbol string, limit int) ([]MarketTradeView, error) {
	realmID = normalizeRealmID(realmID)
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	query := database.WithContext(ctx).Where("realm_id = ?", realmID)
	if strings.TrimSpace(symbol) != "" {
		query = query.Where("item_key = ?", strings.TrimSpace(symbol))
	}
	rows := make([]dal.MarketTrade, 0)
	if err := query.Order("tick DESC, id DESC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	trades := make([]MarketTradeView, 0, len(rows))
	for _, row := range rows {
		trades = append(trades, MarketTradeView{
			ID:         row.ID,
			ItemKey:    row.ItemKey,
			Price:      row.Price,
			Quantity:   row.Quantity,
			Tick:       row.Tick,
			BuyerType:  row.BuyerType,
			BuyerID:    row.BuyerID,
			SellerType: row.SellerType,
			SellerID:   row.SellerID,
		})
	}
	return trades, nil
}

func processMarketOrdersAtTick(ctx context.Context, tx *gorm.DB, currentTick int64, realmID uint) error {
	realmID = normalizeRealmID(realmID)
	if err := ensureMarketDefaults(ctx, tx, currentTick, realmID); err != nil {
		return err
	}
	if err := ensureMarketLiquidityDefaults(ctx, tx, currentTick, realmID); err != nil {
		return err
	}
	if err := expireOpenMarketOrders(ctx, tx, currentTick, realmID); err != nil {
		return err
	}
	if err := matchPlayerOrders(ctx, tx, currentTick, realmID); err != nil {
		return err
	}
	if err := matchOrdersWithNPC(ctx, tx, currentTick, realmID); err != nil {
		return err
	}
	if err := runNPCAutonomousMarketCycle(ctx, tx, currentTick, realmID); err != nil {
		return err
	}
	if err := runStorytellerMarketManagement(ctx, tx, currentTick, realmID); err != nil {
		return err
	}
	return rebalanceMarketLiquidity(ctx, tx, currentTick, realmID)
}

func runStorytellerMarketManagement(ctx context.Context, tx *gorm.DB, currentTick int64, realmID uint) error {
	if currentTick%storytellerTickInterval != 0 {
		return nil
	}

	prices := make([]dal.MarketPrice, 0)
	if err := tx.WithContext(ctx).
		Where("realm_id = ?", realmID).
		Order("item_key ASC").
		Find(&prices).Error; err != nil {
		return err
	}
	if len(prices) == 0 {
		return nil
	}

	liquidityRows := make([]dal.MarketLiquidity, 0)
	if err := tx.WithContext(ctx).
		Where("realm_id = ?", realmID).
		Find(&liquidityRows).Error; err != nil {
		return err
	}
	liquidityBySymbol := make(map[string]dal.MarketLiquidity, len(liquidityRows))
	for _, row := range liquidityRows {
		liquidityBySymbol[row.ItemKey] = row
	}

	for _, row := range prices {
		liquidity, hasLiquidity := liquidityBySymbol[row.ItemKey]
		baselinePrice := baselineNarrativePrice(row.ItemKey, row.Price)
		wave := storytellerWaveOffset(currentTick, row.ItemKey, baselinePrice)
		pressure := int64(0)
		if hasLiquidity {
			pressure = storytellerPressureOffset(liquidity)
		}
		trendCorrection, err := storytellerTrendCorrection(ctx, tx, row.ItemKey, currentTick, realmID)
		if err != nil {
			return err
		}

		targetPrice := baselinePrice + wave + pressure + trendCorrection
		if targetPrice < 1 {
			targetPrice = 1
		}
		if targetPrice > 500 {
			targetPrice = 500
		}

		delta := targetPrice - row.Price
		if delta > 2 {
			delta = 2
		}
		if delta < -2 {
			delta = -2
		}
		if delta == 0 {
			continue
		}

		if _, err := applySingleMarketDelta(ctx, tx, row.ItemKey, delta, marketStorytellerDeltaSource, currentTick, realmID); err != nil {
			return err
		}
	}

	return nil
}

func baselineNarrativePrice(symbol string, fallback int64) int64 {
	switch strings.ToLower(strings.TrimSpace(symbol)) {
	case "scrap":
		return 8
	case "wood":
		return 5
	default:
		if fallback > 0 {
			return fallback
		}
		return 1
	}
}

func storytellerWaveOffset(currentTick int64, symbol string, baselinePrice int64) int64 {
	seed := symbolSeed(symbol)
	phase := float64(positiveModulo(currentTick+seed*11, storytellerCyclePeriodTicks))
	angle := (phase / float64(storytellerCyclePeriodTicks)) * 2 * math.Pi
	amplitude := float64(maxInt64(baselinePrice/4, 1))
	return int64(math.Round(math.Sin(angle) * amplitude))
}

func storytellerPressureOffset(liquidity dal.MarketLiquidity) int64 {
	baseline := maxInt64(liquidity.BaselineQty, 1)
	pressurePct := ((baseline - liquidity.Quantity) * 100) / baseline
	if pressurePct > 18 {
		return 2
	}
	if pressurePct > 8 {
		return 1
	}
	if pressurePct < -18 {
		return -2
	}
	if pressurePct < -8 {
		return -1
	}
	return 0
}

func storytellerTrendCorrection(ctx context.Context, tx *gorm.DB, symbol string, currentTick int64, realmID uint) (int64, error) {
	windowStart := currentTick - storytellerTrendWindowTicks
	oldest := dal.MarketHistory{}
	oldestResult := tx.WithContext(ctx).
		Where("realm_id = ? AND item_key = ? AND tick >= ?", realmID, symbol, windowStart).
		Order("tick ASC, id ASC").
		Limit(1).
		Find(&oldest)
	if oldestResult.Error != nil {
		return 0, oldestResult.Error
	}
	if oldestResult.RowsAffected == 0 {
		return 0, nil
	}

	latest := dal.MarketHistory{}
	latestResult := tx.WithContext(ctx).
		Where("realm_id = ? AND item_key = ?", realmID, symbol).
		Order("tick DESC, id DESC").
		Limit(1).
		Find(&latest)
	if latestResult.Error != nil {
		return 0, latestResult.Error
	}
	if latestResult.RowsAffected == 0 {
		return 0, nil
	}

	change := latest.Price - oldest.Price
	if change >= 8 {
		return -2, nil
	}
	if change >= 4 {
		return -1, nil
	}
	if change <= -8 {
		return 2, nil
	}
	if change <= -4 {
		return 1, nil
	}

	return 0, nil
}

func runNPCAutonomousMarketCycle(ctx context.Context, tx *gorm.DB, currentTick int64, realmID uint) error {
	// Run a lightweight deterministic NPC cycle every 15 in-game minutes.
	if currentTick%15 != 0 {
		return nil
	}

	rows := make([]dal.MarketLiquidity, 0)
	if err := tx.WithContext(ctx).Where("realm_id = ?", realmID).Find(&rows).Error; err != nil {
		return err
	}

	for i := range rows {
		entry := &rows[i]
		baseline := maxInt64(entry.BaselineQty, 1)
		flowUnit := maxInt64(baseline/120, 1)

		seed := symbolSeed(entry.ItemKey)
		cyclePhase := positiveModulo(currentTick+seed, 180)

		// First half-cycle models external demand, second half external supply.
		npcNetFlow := int64(0)
		if cyclePhase < 90 {
			npcNetFlow = -flowUnit
		} else {
			npcNetFlow = flowUnit
		}

		nextQty := entry.Quantity + npcNetFlow
		if nextQty < entry.MinQty {
			nextQty = entry.MinQty
		}
		if nextQty > entry.MaxQty {
			nextQty = entry.MaxQty
		}

		pressure := baseline - nextQty
		pressurePct := absInt64(pressure) * 100 / baseline
		priceDelta := int64(0)
		switch {
		case pressurePct >= 40:
			priceDelta = 2
		case pressurePct >= 12:
			priceDelta = 1
		default:
			priceDelta = 0
		}
		if pressure < 0 {
			priceDelta = -priceDelta
		}

		// Add a tiny deterministic pulse so symbols continue to move when near neutral.
		if priceDelta == 0 {
			if cyclePhase == 0 {
				priceDelta = 1
			} else if cyclePhase == 90 {
				priceDelta = -1
			}
		}

		if nextQty != entry.Quantity || pressure != entry.LastPressure {
			entry.Quantity = nextQty
			entry.LastPressure = pressure
			entry.UpdatedTick = currentTick
			if err := tx.WithContext(ctx).Save(entry).Error; err != nil {
				return err
			}
		}

		if priceDelta != 0 {
			if _, err := applySingleMarketDelta(ctx, tx, entry.ItemKey, priceDelta, "npc_cycle", currentTick, realmID); err != nil {
				return err
			}
		}
	}

	return nil
}

func symbolSeed(symbol string) int64 {
	seed := int64(0)
	for _, ch := range symbol {
		seed += int64(ch)
	}
	return seed
}

func ensureMarketLiquidityDefaults(ctx context.Context, tx *gorm.DB, currentTick int64, realmID uint) error {
	defaults := []dal.MarketLiquidity{
		{RealmID: realmID, ItemKey: "scrap", Quantity: 12000, BaselineQty: 12000, MinQty: 3000, MaxQty: 24000, UpdatedTick: currentTick},
		{RealmID: realmID, ItemKey: "wood", Quantity: 10000, BaselineQty: 10000, MinQty: 2500, MaxQty: 22000, UpdatedTick: currentTick},
	}
	for _, item := range defaults {
		entry := dal.MarketLiquidity{}
		result := tx.WithContext(ctx).Where("realm_id = ? AND item_key = ?", realmID, item.ItemKey).Limit(1).Find(&entry)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected > 0 {
			continue
		}
		if err := tx.WithContext(ctx).Create(&item).Error; err != nil {
			return err
		}
	}
	return nil
}

func expireOpenMarketOrders(ctx context.Context, tx *gorm.DB, currentTick int64, realmID uint) error {
	rows := make([]dal.MarketOrder, 0)
	if err := tx.WithContext(ctx).
		Where("realm_id = ? AND state = ? AND cancel_after_tick > 0 AND cancel_after_tick <= ?", realmID, marketOrderStateOpen, currentTick).
		Order("id ASC").
		Find(&rows).Error; err != nil {
		return err
	}
	for i := range rows {
		order := rows[i]
		if err := settleOrderOnClose(ctx, tx, &order, marketOrderStateExpired, false, currentTick); err != nil {
			return err
		}
	}
	return nil
}

func settleOrderOnClose(ctx context.Context, tx *gorm.DB, order *dal.MarketOrder, finalState string, chargeManualCancelFee bool, currentTick int64) error {
	if order.State != marketOrderStateOpen {
		return nil
	}

	if order.Side == marketOrderSideBuy {
		refund := order.EscrowCoins
		fee := int64(0)
		if chargeManualCancelFee {
			fee = maxInt64((refund*50)/10000, 1)
		}
		if refund > 0 {
			refundAfterFee := refund - fee
			if refundAfterFee < 0 {
				refundAfterFee = 0
			}
			if refundAfterFee > 0 {
				if err := applyItemDelta(ctx, tx, order.PlayerID, order.RealmID, map[string]int64{"coins": refundAfterFee}); err != nil {
					return err
				}
			}
			order.CancellationNote = ""
			if fee > 0 {
				order.CancellationNote = fmt.Sprintf("manual cancel fee charged: %d coins", fee)
			}
		}
		order.EscrowCoins = 0
	} else if order.Side == marketOrderSideSell && order.QuantityOpen > 0 {
		if err := applyItemDelta(ctx, tx, order.PlayerID, order.RealmID, map[string]int64{order.ItemKey: order.QuantityOpen}); err != nil {
			return err
		}
		if chargeManualCancelFee {
			feeCoins := maxInt64((order.QuantityOpen*order.LimitPrice*50)/10000, 1)
			if err := applyItemDelta(ctx, tx, order.PlayerID, order.RealmID, map[string]int64{"coins": -feeCoins}); err != nil {
				return err
			}
			order.CancellationNote = fmt.Sprintf("manual cancel fee charged: %d coins", feeCoins)
		}
	}

	order.State = finalState
	order.QuantityOpen = 0
	order.LastMatchedTick = currentTick
	return tx.WithContext(ctx).Save(order).Error
}

func matchPlayerOrders(ctx context.Context, tx *gorm.DB, currentTick int64, realmID uint) error {
	buys := make([]dal.MarketOrder, 0)
	sells := make([]dal.MarketOrder, 0)
	if err := tx.WithContext(ctx).
		Where("realm_id = ? AND state = ? AND side = ? AND quantity_open > 0", realmID, marketOrderStateOpen, marketOrderSideBuy).
		Order("limit_price DESC, id ASC").
		Find(&buys).Error; err != nil {
		return err
	}
	if err := tx.WithContext(ctx).
		Where("realm_id = ? AND state = ? AND side = ? AND quantity_open > 0", realmID, marketOrderStateOpen, marketOrderSideSell).
		Order("limit_price ASC, id ASC").
		Find(&sells).Error; err != nil {
		return err
	}

	for bi := range buys {
		buy := &buys[bi]
		if buy.QuantityOpen <= 0 {
			continue
		}
		for si := range sells {
			sell := &sells[si]
			if sell.QuantityOpen <= 0 {
				continue
			}
			if buy.PlayerID == sell.PlayerID {
				continue
			}
			if buy.ItemKey != sell.ItemKey {
				continue
			}
			if buy.LimitPrice < sell.LimitPrice {
				continue
			}

			price := sell.LimitPrice
			qty := minInt64(buy.QuantityOpen, sell.QuantityOpen)
			if qty <= 0 {
				continue
			}

			if err := executeTrade(ctx, tx, currentTick, buy, sell, qty, price, marketCounterpartyPlayer, marketCounterpartyPlayer); err != nil {
				return err
			}
		}
	}

	return nil
}

func matchOrdersWithNPC(ctx context.Context, tx *gorm.DB, currentTick int64, realmID uint) error {
	orders := make([]dal.MarketOrder, 0)
	if err := tx.WithContext(ctx).
		Where("realm_id = ? AND state = ? AND quantity_open > 0", realmID, marketOrderStateOpen).
		Order("id ASC").
		Find(&orders).Error; err != nil {
		return err
	}

	for oi := range orders {
		order := &orders[oi]
		if order.QuantityOpen <= 0 {
			continue
		}

		liquidity := dal.MarketLiquidity{}
		result := tx.WithContext(ctx).
			Where("realm_id = ? AND item_key = ?", realmID, order.ItemKey).
			Limit(1).
			Find(&liquidity)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			continue
		}

		marketPrice, err := getMarketPrice(ctx, tx, order.ItemKey, realmID)
		if err != nil {
			return err
		}

		switch order.Side {
		case marketOrderSideBuy:
			if order.LimitPrice < marketPrice || liquidity.Quantity <= liquidity.MinQty {
				continue
			}
			availableToSell := liquidity.Quantity - liquidity.MinQty
			qty := minInt64(order.QuantityOpen, availableToSell)
			if qty <= 0 {
				continue
			}
			npcSell := &dal.MarketOrder{RealmID: realmID, PlayerID: 0, ItemKey: order.ItemKey, Side: marketOrderSideSell, LimitPrice: marketPrice, QuantityOpen: qty}
			if err := executeTrade(ctx, tx, currentTick, order, npcSell, qty, marketPrice, marketCounterpartyPlayer, marketCounterpartyNPC); err != nil {
				return err
			}
			liquidity.Quantity -= qty
			liquidity.UpdatedTick = currentTick
			if err := tx.WithContext(ctx).Save(&liquidity).Error; err != nil {
				return err
			}
		case marketOrderSideSell:
			if order.LimitPrice > marketPrice || liquidity.Quantity >= liquidity.MaxQty {
				continue
			}
			space := liquidity.MaxQty - liquidity.Quantity
			qty := minInt64(order.QuantityOpen, space)
			if qty <= 0 {
				continue
			}
			npcBuy := &dal.MarketOrder{RealmID: realmID, PlayerID: 0, ItemKey: order.ItemKey, Side: marketOrderSideBuy, LimitPrice: marketPrice, QuantityOpen: qty, EscrowCoins: qty * marketPrice}
			if err := executeTrade(ctx, tx, currentTick, npcBuy, order, qty, marketPrice, marketCounterpartyNPC, marketCounterpartyPlayer); err != nil {
				return err
			}
			liquidity.Quantity += qty
			liquidity.UpdatedTick = currentTick
			if err := tx.WithContext(ctx).Save(&liquidity).Error; err != nil {
				return err
			}
		}
	}

	return nil
}

func executeTrade(ctx context.Context, tx *gorm.DB, currentTick int64, buy *dal.MarketOrder, sell *dal.MarketOrder, quantity int64, price int64, buyerType string, sellerType string) error {
	if quantity <= 0 || price <= 0 {
		return nil
	}
	cost := quantity * price
	if buy.EscrowCoins < cost {
		return nil
	}

	if buyerType == marketCounterpartyPlayer {
		if err := applyItemDelta(ctx, tx, buy.PlayerID, buy.RealmID, map[string]int64{buy.ItemKey: quantity}); err != nil {
			return err
		}
	}
	if sellerType == marketCounterpartyPlayer {
		if err := applyItemDelta(ctx, tx, sell.PlayerID, sell.RealmID, map[string]int64{"coins": cost}); err != nil {
			return err
		}
	}

	buy.QuantityOpen -= quantity
	buy.EscrowCoins -= cost
	buy.LastMatchedTick = currentTick
	if buy.QuantityOpen <= 0 {
		if buy.EscrowCoins > 0 && buy.PlayerID != 0 {
			if err := applyItemDelta(ctx, tx, buy.PlayerID, buy.RealmID, map[string]int64{"coins": buy.EscrowCoins}); err != nil {
				return err
			}
			buy.EscrowCoins = 0
		}
		buy.State = marketOrderStateFilled
	}

	sell.QuantityOpen -= quantity
	sell.LastMatchedTick = currentTick
	if sell.QuantityOpen <= 0 {
		sell.State = marketOrderStateFilled
	}

	if buy.ID != 0 {
		if err := tx.WithContext(ctx).Save(buy).Error; err != nil {
			return err
		}
	}
	if sell.ID != 0 {
		if err := tx.WithContext(ctx).Save(sell).Error; err != nil {
			return err
		}
	}

	trade := dal.MarketTrade{
		RealmID:      buy.RealmID,
		ItemKey:      buy.ItemKey,
		Price:        price,
		Quantity:     quantity,
		Tick:         currentTick,
		BuyerType:    buyerType,
		BuyerID:      buy.PlayerID,
		SellerType:   sellerType,
		SellerID:     sell.PlayerID,
		BuyOrderID:   buy.ID,
		SellOrderID:  sell.ID,
		ExecutionTag: "limit",
	}
	if err := tx.WithContext(ctx).Create(&trade).Error; err != nil {
		return err
	}

	marketPrice, err := getMarketPrice(ctx, tx, buy.ItemKey, buy.RealmID)
	if err != nil {
		return err
	}
	baseImpact := tradePriceImpactFromQuantity(quantity)
	priceDelta := int64(0)
	if baseImpact > 0 {
		switch {
		case sellerType == marketCounterpartyNPC:
			priceDelta = baseImpact
		case buyerType == marketCounterpartyNPC:
			priceDelta = -baseImpact
		default:
			// For player-vs-player prints, infer direction from execution relative to current price.
			if price > marketPrice {
				priceDelta = baseImpact
			} else if price < marketPrice {
				priceDelta = -baseImpact
			}
		}
	}
	if _, err := applySingleMarketDelta(ctx, tx, buy.ItemKey, priceDelta, "orderbook_trade", currentTick, buy.RealmID); err != nil {
		return err
	}

	return nil
}

func rebalanceMarketLiquidity(ctx context.Context, tx *gorm.DB, currentTick int64, realmID uint) error {
	rows := make([]dal.MarketLiquidity, 0)
	if err := tx.WithContext(ctx).Where("realm_id = ?", realmID).Find(&rows).Error; err != nil {
		return err
	}
	for i := range rows {
		entry := &rows[i]
		delta := entry.BaselineQty - entry.Quantity
		if delta == 0 {
			continue
		}
		step := maxInt64(absInt64(delta)/20, 1)
		if delta < 0 {
			step = -step
		}
		entry.Quantity += step
		if entry.Quantity < entry.MinQty {
			entry.Quantity = entry.MinQty
		}
		if entry.Quantity > entry.MaxQty {
			entry.Quantity = entry.MaxQty
		}
		entry.UpdatedTick = currentTick
		if err := tx.WithContext(ctx).Save(entry).Error; err != nil {
			return err
		}
	}
	return nil
}

func toMarketOrderView(row dal.MarketOrder) MarketOrderView {
	return MarketOrderView{
		ID:               row.ID,
		ItemKey:          row.ItemKey,
		Side:             row.Side,
		State:            row.State,
		LimitPrice:       row.LimitPrice,
		QuantityTotal:    row.QuantityTotal,
		QuantityOpen:     row.QuantityOpen,
		EscrowCoins:      row.EscrowCoins,
		CancelAfterTick:  row.CancelAfterTick,
		LastMatchedTick:  row.LastMatchedTick,
		CancellationNote: row.CancellationNote,
	}
}

func minInt64(left int64, right int64) int64 {
	if left < right {
		return left
	}
	return right
}

func absInt64(value int64) int64 {
	if value < 0 {
		return -value
	}
	return value
}

func tradePriceImpactFromQuantity(quantity int64) int64 {
	if quantity <= 0 {
		return 0
	}
	units := quantity / marketLotSize
	switch {
	case units >= 50:
		return 3
	case units >= 20:
		return 2
	case units >= 5:
		return 1
	default:
		return 0
	}
}
