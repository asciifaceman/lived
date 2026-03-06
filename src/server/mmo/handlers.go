package mmo

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/asciifaceman/lived/pkg/config"
	"github.com/asciifaceman/lived/pkg/dal"
	"github.com/asciifaceman/lived/src/gameplay"
	serverAuth "github.com/asciifaceman/lived/src/server/auth"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

const (
	statusSuccess = "success"
	statusActive  = "active"
)

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

func RegisterRoutes(group *echo.Group, database *gorm.DB, cfg config.Config) {
	if cfg.MMOAuthEnabled {
		authMW := serverAuth.RequireAuth(database, cfg)
		group.GET("/stats/system", makeSystemStatsHandler(database, cfg), authMW)
		group.GET("/stats/players", makePlayerStatsHandler(database, cfg), authMW)
		group.GET("/stats/economy", makeEconomyStatsHandler(database, cfg), authMW)
		return
	}

	group.GET("/stats/system", makeSystemStatsHandler(database, cfg))
	group.GET("/stats/players", makePlayerStatsHandler(database, cfg))
	group.GET("/stats/economy", makeEconomyStatsHandler(database, cfg))
}

func makeSystemStatsHandler(database *gorm.DB, cfg config.Config) echo.HandlerFunc {
	return func(c echo.Context) error {
		realmID, err := resolveRealmIDForStatsRequest(c, database, cfg)
		if err != nil {
			return err
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

func makePlayerStatsHandler(database *gorm.DB, cfg config.Config) echo.HandlerFunc {
	return func(c echo.Context) error {
		realmID, err := resolveRealmIDForStatsRequest(c, database, cfg)
		if err != nil {
			return err
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

func makeEconomyStatsHandler(database *gorm.DB, cfg config.Config) echo.HandlerFunc {
	return func(c echo.Context) error {
		realmID, err := resolveRealmIDForStatsRequest(c, database, cfg)
		if err != nil {
			return err
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

func resolveRealmIDForStatsRequest(c echo.Context, database *gorm.DB, cfg config.Config) (uint, error) {
	realmID := uint(1)
	rawRealmID := strings.TrimSpace(c.QueryParam("realmId"))
	if rawRealmID != "" {
		parsedRealmID, err := parseRealmID(rawRealmID)
		if err != nil {
			return 0, err
		}
		realmID = parsedRealmID
	}

	if !cfg.MMOAuthEnabled {
		return realmID, nil
	}

	actor, ok := serverAuth.ActorFromContext(c.Request().Context())
	if !ok {
		return 0, echo.NewHTTPError(http.StatusUnauthorized, "missing actor context")
	}

	characterID, err := parseOptionalPositiveUint(c.QueryParam("characterId"), "characterId")
	if err != nil {
		return 0, err
	}

	character, err := loadActorCharacter(c.Request().Context(), database, actor.AccountID, characterID)
	if err != nil {
		return 0, echo.NewHTTPError(http.StatusInternalServerError, "failed to load character")
	}
	if character == nil {
		return 0, echo.NewHTTPError(http.StatusBadRequest, "complete onboarding first")
	}

	if rawRealmID != "" && realmID != character.RealmID {
		return 0, echo.NewHTTPError(http.StatusForbidden, "realmId does not match authenticated character realm")
	}

	return character.RealmID, nil
}

func parseOptionalPositiveUint(raw string, fieldName string) (uint, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0, nil
	}

	parsed, err := strconv.ParseUint(trimmed, 10, 64)
	if err != nil || parsed == 0 {
		return 0, echo.NewHTTPError(http.StatusBadRequest, fieldName+" must be a positive integer")
	}

	return uint(parsed), nil
}

func loadActorCharacter(ctx context.Context, database *gorm.DB, accountID uint, characterID uint) (*dal.Character, error) {
	const defaultRealmID uint = 1

	character := &dal.Character{}
	query := database.WithContext(ctx).Where("account_id = ? AND status = ?", accountID, statusActive)
	if characterID != 0 {
		query = query.Where("id = ?", characterID)
	} else {
		query = query.Where("realm_id = ?", defaultRealmID)
	}

	result := query.Order("is_primary DESC, id ASC").Limit(1).Find(character)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}

	return character, nil
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
