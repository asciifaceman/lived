package mmo

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/asciifaceman/lived/pkg/config"
	"github.com/asciifaceman/lived/pkg/dal"
	"github.com/asciifaceman/lived/src/gameplay"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

const statusSuccess = "success"

type apiResponse struct {
	Status    string `json:"status"`
	Message   string `json:"message"`
	RequestID string `json:"requestId,omitempty"`
	Data      any    `json:"data,omitempty"`
}

type itemQuantity struct {
	ItemKey  string `json:"itemKey"`
	Quantity int64  `json:"quantity"`
}

func RegisterRoutes(group *echo.Group, database *gorm.DB, _ config.Config) {
	group.GET("/stats/system", makeSystemStatsHandler(database))
	group.GET("/stats/players", makePlayerStatsHandler(database))
	group.GET("/stats/economy", makeEconomyStatsHandler(database))
}

func makeSystemStatsHandler(database *gorm.DB) echo.HandlerFunc {
	return func(c echo.Context) error {
		realmID, err := parseRealmID(c.QueryParam("realmId"))
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}

		tick, err := gameplay.CurrentWorldTickForRealm(c.Request().Context(), database, realmID)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load world tick")
		}

		counts := map[string]int64{}
		states := []string{"queued", "active", "completed", "failed"}
		for _, state := range states {
			var count int64
			if err := database.WithContext(c.Request().Context()).
				Model(&dal.BehaviorInstance{}).
				Where("realm_id = ? AND state = ?", realmID, state).
				Count(&count).Error; err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "failed to load behavior stats")
			}
			counts[state] = count
		}

		var worldEvents int64
		if err := database.WithContext(c.Request().Context()).
			Model(&dal.WorldEvent{}).
			Where("realm_id = ?", realmID).
			Count(&worldEvents).Error; err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load event stats")
		}

		data := map[string]any{
			"realmId": realmID,
			"tick":    tick,
			"behaviors": map[string]int64{
				"queued":    counts["queued"],
				"active":    counts["active"],
				"completed": counts["completed"],
				"failed":    counts["failed"],
			},
			"worldEvents": worldEvents,
		}

		return respondSuccess(c, http.StatusOK, "MMO system stats loaded.", data)
	}
}

func makePlayerStatsHandler(database *gorm.DB) echo.HandlerFunc {
	return func(c echo.Context) error {
		realmID, err := parseRealmID(c.QueryParam("realmId"))
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}

		var activeCharacters int64
		if err := database.WithContext(c.Request().Context()).
			Model(&dal.Character{}).
			Where("realm_id = ? AND status = ?", realmID, "active").
			Count(&activeCharacters).Error; err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load character stats")
		}

		var uniqueAccounts int64
		if err := database.WithContext(c.Request().Context()).
			Model(&dal.Character{}).
			Distinct("account_id").
			Where("realm_id = ? AND status = ?", realmID, "active").
			Count(&uniqueAccounts).Error; err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load account stats")
		}

		var uniquePlayers int64
		if err := database.WithContext(c.Request().Context()).
			Model(&dal.Character{}).
			Distinct("player_id").
			Where("realm_id = ? AND status = ?", realmID, "active").
			Count(&uniquePlayers).Error; err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load player stats")
		}

		var totalCoins int64
		if err := database.WithContext(c.Request().Context()).
			Model(&dal.InventoryEntry{}).
			Where("realm_id = ? AND owner_type = ? AND item_key = ?", realmID, gameplay.ActorPlayer, "coins").
			Select("COALESCE(SUM(quantity), 0)").
			Scan(&totalCoins).Error; err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load coin stats")
		}

		data := map[string]any{
			"realmId":          realmID,
			"activeCharacters": activeCharacters,
			"uniqueAccounts":   uniqueAccounts,
			"uniquePlayers":    uniquePlayers,
			"totalCoins":       totalCoins,
		}

		return respondSuccess(c, http.StatusOK, "MMO player stats loaded.", data)
	}
}

func makeEconomyStatsHandler(database *gorm.DB) echo.HandlerFunc {
	return func(c echo.Context) error {
		realmID, err := parseRealmID(c.QueryParam("realmId"))
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}

		marketRows := make([]dal.MarketPrice, 0)
		if err := database.WithContext(c.Request().Context()).
			Where("realm_id = ?", realmID).
			Order("item_key ASC").
			Find(&marketRows).Error; err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load market stats")
		}

		inventoryRows := make([]itemQuantity, 0)
		if err := database.WithContext(c.Request().Context()).
			Model(&dal.InventoryEntry{}).
			Select("item_key, COALESCE(SUM(quantity), 0) AS quantity").
			Where("realm_id = ? AND owner_type = ?", realmID, gameplay.ActorPlayer).
			Group("item_key").
			Order("item_key ASC").
			Scan(&inventoryRows).Error; err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load inventory aggregates")
		}

		tickers := make([]map[string]any, 0, len(marketRows))
		for _, row := range marketRows {
			tickers = append(tickers, map[string]any{
				"itemKey":     row.ItemKey,
				"price":       row.Price,
				"lastDelta":   row.LastDelta,
				"lastSource":  row.LastSource,
				"updatedTick": row.UpdatedTick,
			})
		}

		data := map[string]any{
			"realmId":               realmID,
			"marketPrices":          tickers,
			"inventoryTotalsByItem": inventoryRows,
		}

		return respondSuccess(c, http.StatusOK, "MMO economy stats loaded.", data)
	}
}

func parseRealmID(raw string) (uint, error) {
	if strings.TrimSpace(raw) == "" {
		return 1, nil
	}

	parsed, err := strconv.ParseUint(strings.TrimSpace(raw), 10, 64)
	if err != nil || parsed == 0 {
		return 0, echo.NewHTTPError(http.StatusBadRequest, "realmId must be a positive integer")
	}

	return uint(parsed), nil
}

func respondSuccess(c echo.Context, code int, message string, data any) error {
	requestID := c.Response().Header().Get(echo.HeaderXRequestID)
	return c.JSON(code, apiResponse{
		Status:    statusSuccess,
		Message:   message,
		RequestID: requestID,
		Data:      data,
	})
}
