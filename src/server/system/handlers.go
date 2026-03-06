package system

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/asciifaceman/lived/pkg/config"
	"github.com/asciifaceman/lived/pkg/dal"
	"github.com/asciifaceman/lived/pkg/gamedata"
	"github.com/asciifaceman/lived/pkg/idempotency"
	"github.com/asciifaceman/lived/pkg/ratelimit"
	"github.com/asciifaceman/lived/pkg/version"
	"github.com/asciifaceman/lived/src/gameplay"
	serverAuth "github.com/asciifaceman/lived/src/server/auth"
	"github.com/asciifaceman/lived/src/server/requestbind"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

const statusSuccess = "success"

type saveGame struct {
	Version        int      `json:"v"`
	Players        []string `json:"p"`
	SimulationTick int64    `json:"t"`
}

type exportResponse struct {
	Save string `json:"save"`
}

type importRequest struct {
	Save string `json:"save"`
}

type newGameRequest struct {
	Name string `json:"name"`
}

type startBehaviorRequest struct {
	BehaviorKey string `json:"behaviorKey"`
	MarketWait  string `json:"marketWait,omitempty"`
	Mode        string `json:"mode,omitempty"`
	RepeatUntil string `json:"repeatUntil,omitempty"`
}

type cancelBehaviorRequest struct {
	BehaviorID uint `json:"behaviorId"`
}

type ascendRequest struct {
	Name string `json:"name"`
}

type purchaseUpgradeRequest struct {
	UpgradeKey string `json:"upgradeKey"`
}

type marketHistoryQuery struct {
	Symbol string `query:"symbol"`
	Limit  int    `query:"limit"`
	Realm  uint   `query:"realmId"`
}

type marketCandleQuery struct {
	Symbol      string `query:"symbol"`
	Limit       int    `query:"limit"`
	BucketTicks int64  `query:"bucketTicks"`
	Realm       uint   `query:"realmId"`
}

type placeMarketOrderRequest struct {
	ItemKey            string `json:"itemKey"`
	Side               string `json:"side"`
	Quantity           int64  `json:"quantity"`
	LimitPrice         int64  `json:"limitPrice"`
	CancelAfter        string `json:"cancelAfter,omitempty"`
	ManualCancelFeeBps int64  `json:"manualCancelFeeBps,omitempty"`
}

type cancelMarketOrderRequest struct {
	OrderID uint `json:"orderId"`
}

type marketOrderQuery struct {
	Symbol string `query:"symbol"`
	State  string `query:"state"`
	Limit  int    `query:"limit"`
	Depth  int    `query:"depth"`
	Realm  uint   `query:"realmId"`
}

type systemStatusData struct {
	Version             versionData                   `json:"version"`
	Save                string                        `json:"save"`
	Players             []string                      `json:"players"`
	SimulationTick      int64                         `json:"simulationTick"`
	WorldAgeMinutes     int64                         `json:"worldAgeMinutes"`
	WorldAgeHours       int64                         `json:"worldAgeHours"`
	WorldAgeDays        int64                         `json:"worldAgeDays"`
	TickInterval        string                        `json:"tickInterval"`
	GameMinutesRate     float64                       `json:"gameMinutesPerRealMinute"`
	AutoMigrate         bool                          `json:"autoMigrate"`
	PendingBehaviorsRaw string                        `json:"pendingBehaviorsRaw"`
	Inventory           map[string]int64              `json:"inventory"`
	CoreStats           map[string]int64              `json:"coreStats"`
	DerivedStats        map[string]int64              `json:"derivedStats"`
	Stats               map[string]int64              `json:"stats"`
	MarketPrices        map[string]int64              `json:"marketPrices"`
	Behaviors           []gameplay.BehaviorView       `json:"behaviors"`
	RecentEvents        []gameplay.RecentEventView    `json:"recentEvents"`
	AscensionCount      int64                         `json:"ascensionCount"`
	WealthBonusPct      float64                       `json:"wealthBonusPct"`
	Ascension           gameplay.AscensionEligibility `json:"ascension"`
}

type versionData struct {
	API      string          `json:"api"`
	Backend  string          `json:"backend"`
	Frontend string          `json:"frontend"`
	GameData versionGameData `json:"gameData"`
}

type versionGameData struct {
	ManifestVersion int    `json:"manifestVersion"`
	FilesHash       string `json:"filesHash"`
}

type apiResponse struct {
	Status    string `json:"status"`
	Message   string `json:"message"`
	RequestID string `json:"requestId,omitempty"`
	Data      any    `json:"data,omitempty"`
}

func RegisterRoutes(group *echo.Group, database *gorm.DB, cfg config.Config) {
	group.GET("/export", makeExportHandler(database, cfg))
	group.POST("/import", makeImportHandler(database, cfg))
	group.POST("/new", makeNewGameHandler(database, cfg))
	statusHandler := makeStatusHandler(database, cfg)
	idempotencyScope := idempotency.ClientIPScope
	if cfg.RateLimitIdentity == "account_or_ip" {
		idempotencyScope = idempotency.AccountOrIPScope(func(ctx context.Context) (uint, bool) {
			actor, ok := serverAuth.ActorFromContext(ctx)
			if !ok || actor.AccountID == 0 {
				return 0, false
			}
			return actor.AccountID, true
		})
	}
	idempotencyStore := idempotency.NewStore(cfg.IdempotencyTTL, idempotencyScope)
	idempotencyMW := idempotencyStore.Middleware()

	behaviorIdentifier := ratelimit.ClientIPIdentifier
	if cfg.RateLimitIdentity == "account_or_ip" {
		behaviorIdentifier = ratelimit.AccountOrIPIdentifier(func(ctx context.Context) (uint, bool) {
			actor, ok := serverAuth.ActorFromContext(ctx)
			if !ok || actor.AccountID == 0 {
				return 0, false
			}
			return actor.AccountID, true
		})
	}
	behaviorLimiter := ratelimit.NewFixedWindowLimiter(cfg.RateLimitWindow, behaviorIdentifier)
	if cfg.MMOAuthEnabled {
		group.GET("/status", statusHandler, serverAuth.RequireAuth(database, cfg))
	} else {
		group.GET("/status", statusHandler)
	}
	group.GET("/version", makeVersionHandler())
	startBehaviorHandler := makeStartBehaviorHandler(database, cfg)
	cancelBehaviorHandler := makeCancelBehaviorHandler(database, cfg)
	if cfg.MMOAuthEnabled {
		if cfg.RateLimitEnabled {
			if cfg.IdempotencyEnabled {
				group.POST("/behaviors/start", startBehaviorHandler, serverAuth.RequireAuth(database, cfg), idempotencyMW, behaviorLimiter.Middleware("behavior_start", cfg.RateLimitBehaviorMax))
			} else {
				group.POST("/behaviors/start", startBehaviorHandler, serverAuth.RequireAuth(database, cfg), behaviorLimiter.Middleware("behavior_start", cfg.RateLimitBehaviorMax))
			}
		} else {
			if cfg.IdempotencyEnabled {
				group.POST("/behaviors/start", startBehaviorHandler, serverAuth.RequireAuth(database, cfg), idempotencyMW)
			} else {
				group.POST("/behaviors/start", startBehaviorHandler, serverAuth.RequireAuth(database, cfg))
			}
		}
		if cfg.RateLimitEnabled {
			group.POST("/behaviors/cancel", cancelBehaviorHandler, serverAuth.RequireAuth(database, cfg), behaviorLimiter.Middleware("behavior_cancel", cfg.RateLimitBehaviorMax))
		} else {
			group.POST("/behaviors/cancel", cancelBehaviorHandler, serverAuth.RequireAuth(database, cfg))
		}
	} else {
		if cfg.RateLimitEnabled {
			if cfg.IdempotencyEnabled {
				group.POST("/behaviors/start", startBehaviorHandler, idempotencyMW, behaviorLimiter.Middleware("behavior_start", cfg.RateLimitBehaviorMax))
			} else {
				group.POST("/behaviors/start", startBehaviorHandler, behaviorLimiter.Middleware("behavior_start", cfg.RateLimitBehaviorMax))
			}
		} else {
			if cfg.IdempotencyEnabled {
				group.POST("/behaviors/start", startBehaviorHandler, idempotencyMW)
			} else {
				group.POST("/behaviors/start", startBehaviorHandler)
			}
		}
		if cfg.RateLimitEnabled {
			group.POST("/behaviors/cancel", cancelBehaviorHandler, behaviorLimiter.Middleware("behavior_cancel", cfg.RateLimitBehaviorMax))
		} else {
			group.POST("/behaviors/cancel", cancelBehaviorHandler)
		}
	}
	catalogHandler := makeBehaviorCatalogHandler(database, cfg)
	if cfg.MMOAuthEnabled {
		group.GET("/behaviors/catalog", catalogHandler, serverAuth.RequireAuth(database, cfg))
	} else {
		group.GET("/behaviors/catalog", catalogHandler)
	}
	marketStatusHandler := makeMarketStatusHandler(database, cfg)
	marketHistoryHandler := makeMarketHistoryHandler(database, cfg)
	marketCandlesHandler := makeMarketCandlesHandler(database, cfg)
	marketOverviewHandler := makeMarketOverviewHandler(database, cfg)
	marketPlaceOrderHandler := makeMarketPlaceOrderHandler(database, cfg)
	marketCancelOrderHandler := makeMarketCancelOrderHandler(database, cfg)
	marketMyOrdersHandler := makeMarketMyOrdersHandler(database, cfg)
	marketOrderBookHandler := makeMarketOrderBookHandler(database, cfg)
	marketTradesHandler := makeMarketTradesHandler(database, cfg)
	if cfg.MMOAuthEnabled {
		group.GET("/market/status", marketStatusHandler, serverAuth.RequireAuth(database, cfg))
		group.GET("/market/history", marketHistoryHandler, serverAuth.RequireAuth(database, cfg))
		group.GET("/market/candles", marketCandlesHandler, serverAuth.RequireAuth(database, cfg))
		group.GET("/market/overview", marketOverviewHandler, serverAuth.RequireAuth(database, cfg))
		group.POST("/market/orders/place", marketPlaceOrderHandler, serverAuth.RequireAuth(database, cfg))
		group.POST("/market/orders/cancel", marketCancelOrderHandler, serverAuth.RequireAuth(database, cfg))
		group.GET("/market/orders/my", marketMyOrdersHandler, serverAuth.RequireAuth(database, cfg))
		group.GET("/market/orders/book", marketOrderBookHandler, serverAuth.RequireAuth(database, cfg))
		group.GET("/market/trades", marketTradesHandler, serverAuth.RequireAuth(database, cfg))
	} else {
		group.GET("/market/status", marketStatusHandler)
		group.GET("/market/history", marketHistoryHandler)
		group.GET("/market/candles", marketCandlesHandler)
		group.GET("/market/overview", marketOverviewHandler)
		group.POST("/market/orders/place", marketPlaceOrderHandler)
		group.POST("/market/orders/cancel", marketCancelOrderHandler)
		group.GET("/market/orders/my", marketMyOrdersHandler)
		group.GET("/market/orders/book", marketOrderBookHandler)
		group.GET("/market/trades", marketTradesHandler)
	}
	ascendHandler := makeAscendHandler(database, cfg)
	upgradeCatalogHandler := makeUpgradeCatalogHandler(database, cfg)
	purchaseUpgradeHandler := makePurchaseUpgradeHandler(database, cfg)
	if cfg.MMOAuthEnabled {
		group.POST("/ascend", ascendHandler, serverAuth.RequireAuth(database, cfg))
		group.GET("/upgrades/catalog", upgradeCatalogHandler, serverAuth.RequireAuth(database, cfg))
		group.POST("/upgrades/purchase", purchaseUpgradeHandler, serverAuth.RequireAuth(database, cfg))
	} else {
		group.POST("/ascend", ascendHandler)
		group.GET("/upgrades/catalog", upgradeCatalogHandler)
		group.POST("/upgrades/purchase", purchaseUpgradeHandler)
	}
}

func makeExportHandler(database *gorm.DB, cfg config.Config) echo.HandlerFunc {
	return func(c echo.Context) error {
		if cfg.MMOAuthEnabled {
			return echo.NewHTTPError(http.StatusConflict, "save export is disabled in MMO mode")
		}

		save, err := loadCurrentSave(c.Request().Context(), database)
		if err != nil {
			return err
		}

		encoded, err := encodeSave(save)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to encode save")
		}

		return respondSuccess(c, http.StatusOK, "A chronicle of this world has been sealed.", exportResponse{Save: encoded})
	}
}

func makeImportHandler(database *gorm.DB, cfg config.Config) echo.HandlerFunc {
	return func(c echo.Context) error {
		if cfg.MMOAuthEnabled {
			return echo.NewHTTPError(http.StatusConflict, "save import is disabled in MMO mode")
		}

		var req importRequest
		if err := requestbind.JSON(c, &req, "invalid import payload"); err != nil {
			return err
		}

		if strings.TrimSpace(req.Save) == "" {
			return echo.NewHTTPError(http.StatusBadRequest, "save is required")
		}

		raw, err := base64.RawURLEncoding.DecodeString(req.Save)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "save must be valid base64url data")
		}

		var save saveGame
		if err := json.Unmarshal(raw, &save); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "save payload is invalid")
		}

		if save.Version != 1 {
			return echo.NewHTTPError(http.StatusBadRequest, "unsupported save version")
		}

		if err := replaceGameState(c.Request().Context(), database, save); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to import save")
		}

		return respondSuccess(c, http.StatusOK, "The world remembers, and the story continues.", nil)
	}
}

func makeNewGameHandler(database *gorm.DB, cfg config.Config) echo.HandlerFunc {
	return func(c echo.Context) error {
		if cfg.MMOAuthEnabled {
			return echo.NewHTTPError(http.StatusConflict, "new game is disabled in MMO mode; use onboarding")
		}

		var req newGameRequest
		if err := requestbind.JSON(c, &req, "invalid new game payload"); err != nil {
			return err
		}

		name := strings.TrimSpace(req.Name)
		if name == "" {
			return echo.NewHTTPError(http.StatusBadRequest, "name is required")
		}

		save := saveGame{
			Version:        1,
			Players:        []string{name},
			SimulationTick: 0,
		}

		if err := replaceGameState(c.Request().Context(), database, save); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to initialize new game")
		}

		return respondSuccess(c, http.StatusOK, "A new story has begun, "+name+" has entered the world.", nil)
	}
}

func makeStatusHandler(database *gorm.DB, cfg config.Config) echo.HandlerFunc {
	return func(c echo.Context) error {
		save := saveGame{SimulationTick: 0, Players: []string{}}
		encodedSave := ""
		if !cfg.MMOAuthEnabled {
			simulationTick, err := gameplay.CurrentWorldTick(c.Request().Context(), database)
			if err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "failed to load world tick")
			}
			save = saveGame{SimulationTick: simulationTick, Players: []string{}}

			save, err = loadCurrentSave(c.Request().Context(), database)
			if err != nil {
				return err
			}

			encodedSave, err = encodeSave(save)
			if err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "failed to encode save")
			}
		}

		runtimeState, err := loadOrInitRuntimeState(c.Request().Context(), database, 1)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load world runtime state")
		}

		data := systemStatusData{
			Version:             currentVersionData(),
			Save:                encodedSave,
			Players:             save.Players,
			SimulationTick:      save.SimulationTick,
			WorldAgeMinutes:     save.SimulationTick,
			WorldAgeHours:       save.SimulationTick / 60,
			WorldAgeDays:        save.SimulationTick / (60 * 24),
			TickInterval:        cfg.TickInterval.String(),
			GameMinutesRate:     cfg.GameMinutesRate,
			AutoMigrate:         cfg.AutoMigrate,
			PendingBehaviorsRaw: runtimeState.PendingBehaviorsJSON,
			Inventory:           map[string]int64{},
			CoreStats:           map[string]int64{},
			DerivedStats:        map[string]int64{},
			Stats:               map[string]int64{},
			MarketPrices:        map[string]int64{},
			Behaviors:           []gameplay.BehaviorView{},
			RecentEvents:        []gameplay.RecentEventView{},
			Ascension:           gameplay.AscensionEligibility{},
		}

		resolvedPlayer := (*dal.Player)(nil)
		resolvedPlayerName := ""
		resolvedRealmID := uint(1)
		if cfg.MMOAuthEnabled {
			actor, ok := serverAuth.ActorFromContext(c.Request().Context())
			if !ok {
				return echo.NewHTTPError(http.StatusUnauthorized, "missing actor context")
			}

			requestedCharacterID := uint(0)
			hasRequestedCharacter := false
			if rawCharacterID := c.QueryParam("characterId"); rawCharacterID != "" {
				parsedID, parseErr := strconv.ParseUint(rawCharacterID, 10, 64)
				if parseErr != nil || parsedID == 0 {
					return echo.NewHTTPError(http.StatusBadRequest, "characterId must be a positive integer")
				}
				hasRequestedCharacter = true
				requestedCharacterID = uint(parsedID)
			}

			character, err := loadActorCharacter(c.Request().Context(), database, actor.AccountID, requestedCharacterID)
			if err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "failed to load character")
			}
			if character == nil {
				if hasRequestedCharacter {
					return echo.NewHTTPError(http.StatusNotFound, "character not found for account")
				}
				return respondSuccess(c, http.StatusOK, "No character onboarded yet. Complete onboarding to begin.", data)
			}

			player, err := loadPlayerByID(c.Request().Context(), database, character.PlayerID)
			if err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "failed to load character player state")
			}
			if player == nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "character player state is missing")
			}

			resolvedPlayer = player
			resolvedPlayerName = character.Name
			resolvedRealmID = character.RealmID
			data.Players = []string{character.Name}
			data.Save = ""
		} else {
			primaryPlayer, err := loadPrimaryPlayer(c.Request().Context(), database)
			if err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "failed to load primary player")
			}
			resolvedPlayer = primaryPlayer
			if primaryPlayer != nil {
				resolvedPlayerName = primaryPlayer.Name
			}
		}

		if resolvedPlayer != nil {
			if cfg.MMOAuthEnabled {
				runtimeForRealm, runtimeErr := loadOrInitRuntimeState(c.Request().Context(), database, resolvedRealmID)
				if runtimeErr != nil {
					return echo.NewHTTPError(http.StatusInternalServerError, "failed to load world runtime state")
				}
				data.PendingBehaviorsRaw = runtimeForRealm.PendingBehaviorsJSON

				realmTick, tickErr := gameplay.CurrentWorldTickForRealm(c.Request().Context(), database, resolvedRealmID)
				if tickErr != nil {
					return echo.NewHTTPError(http.StatusInternalServerError, "failed to load world tick")
				}
				data.SimulationTick = realmTick
				data.WorldAgeMinutes = realmTick
				data.WorldAgeHours = realmTick / 60
				data.WorldAgeDays = realmTick / (60 * 24)
			}

			snapshot, err := gameplay.LoadWorldSnapshot(c.Request().Context(), database, resolvedPlayer.ID, resolvedRealmID)
			if err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "failed to load gameplay snapshot")
			}

			data.Inventory = snapshot.Inventory
			data.CoreStats = snapshot.CoreStats
			data.DerivedStats = snapshot.DerivedStats
			data.Stats = snapshot.Stats
			data.MarketPrices = snapshot.MarketPrices
			if cfg.MMOAuthEnabled {
				data.Behaviors = filterPlayerBehaviors(snapshot.Behaviors, resolvedPlayer.ID)
			} else {
				data.Behaviors = snapshot.Behaviors
			}
			data.RecentEvents = snapshot.RecentEvents
			data.AscensionCount = snapshot.AscensionCount
			data.WealthBonusPct = snapshot.WealthBonusPct
			data.Ascension = snapshot.Ascension

			if cfg.MMOAuthEnabled && resolvedPlayerName != "" {
				data.Players = []string{resolvedPlayerName}
			}
		}

		return respondSuccess(c, http.StatusOK, "The world turns, and its state is known.", data)
	}
}

func filterPlayerBehaviors(behaviors []gameplay.BehaviorView, playerID uint) []gameplay.BehaviorView {
	playerBehaviors := make([]gameplay.BehaviorView, 0, len(behaviors))
	for _, behavior := range behaviors {
		if behavior.ActorType == gameplay.ActorPlayer && behavior.ActorID == playerID {
			playerBehaviors = append(playerBehaviors, behavior)
		}
	}

	return playerBehaviors
}

func makeVersionHandler() echo.HandlerFunc {
	return func(c echo.Context) error {
		return respondSuccess(c, http.StatusOK, "Version metadata loaded.", currentVersionData())
	}
}

func makeStartBehaviorHandler(database *gorm.DB, cfg config.Config) echo.HandlerFunc {
	return func(c echo.Context) error {
		var req startBehaviorRequest
		if err := requestbind.JSON(c, &req, "invalid behavior payload"); err != nil {
			return err
		}

		key := strings.TrimSpace(req.BehaviorKey)
		if key == "" {
			return echo.NewHTTPError(http.StatusBadRequest, "behaviorKey is required")
		}

		marketWaitMinutes, err := parseGameDurationMinutes(req.MarketWait)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
		if marketWaitMinutes > gameplay.MaxMarketWaitDurationMinutes() {
			return echo.NewHTTPError(http.StatusBadRequest, "marketWait exceeds maximum allowed duration")
		}

		queueMode, repeatUntilMinutes, err := parseBehaviorQueueMode(req.Mode, req.RepeatUntil)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}

		resolvedPlayer := (*dal.Player)(nil)
		resolvedName := ""
		resolvedRealmID := uint(1)

		if cfg.MMOAuthEnabled {
			actor, ok := serverAuth.ActorFromContext(c.Request().Context())
			if !ok {
				return echo.NewHTTPError(http.StatusUnauthorized, "missing actor context")
			}

			requestedCharacterID := uint(0)
			hasRequestedCharacter := false
			if rawCharacterID := c.QueryParam("characterId"); rawCharacterID != "" {
				parsedID, parseErr := strconv.ParseUint(rawCharacterID, 10, 64)
				if parseErr != nil || parsedID == 0 {
					return echo.NewHTTPError(http.StatusBadRequest, "characterId must be a positive integer")
				}
				hasRequestedCharacter = true
				requestedCharacterID = uint(parsedID)
			}

			character, err := loadActorCharacter(c.Request().Context(), database, actor.AccountID, requestedCharacterID)
			if err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "failed to load character")
			}
			if character == nil {
				if hasRequestedCharacter {
					return echo.NewHTTPError(http.StatusNotFound, "character not found for account")
				}
				return echo.NewHTTPError(http.StatusBadRequest, "complete onboarding first")
			}

			player, err := loadPlayerByID(c.Request().Context(), database, character.PlayerID)
			if err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "failed to load character player state")
			}
			if player == nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "character player state is missing")
			}

			resolvedPlayer = player
			resolvedName = character.Name
			if character.RealmID != 0 {
				resolvedRealmID = character.RealmID
			}
		} else {
			player, err := loadPrimaryPlayer(c.Request().Context(), database)
			if err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "failed to load primary player")
			}
			if player == nil {
				return echo.NewHTTPError(http.StatusBadRequest, "create a new game first")
			}

			resolvedPlayer = player
			resolvedName = player.Name
		}

		currentTick, err := gameplay.CurrentWorldTickForRealm(c.Request().Context(), database, resolvedRealmID)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load world tick")
		}

		repeatUntilTick := int64(0)
		if queueMode == "repeat-until" {
			repeatUntilTick = currentTick + repeatUntilMinutes
		}

		if err := gameplay.QueuePlayerBehavior(
			c.Request().Context(),
			database,
			resolvedPlayer.ID,
			key,
			currentTick,
			gameplay.QueueBehaviorOptions{MarketWaitDurationMinutes: marketWaitMinutes, RealmID: resolvedRealmID, Mode: queueMode, RepeatUntilTick: repeatUntilTick},
		); err != nil {
			return echo.NewHTTPError(queueBehaviorErrorStatus(err), err.Error())
		}

		behaviorName := gameplay.HumanizeIdentifier(key)
		if definition, ok := gameplay.GetBehaviorDefinition(key); ok {
			behaviorName = gameplay.BehaviorDisplayName(definition)
		}

		response := map[string]any{"behaviorKey": key, "behaviorName": behaviorName, "player": resolvedName}
		if queueMode != "" {
			response["mode"] = queueMode
		}
		if marketWaitMinutes > 0 {
			response["marketWaitMinutes"] = marketWaitMinutes
		}
		if repeatUntilMinutes > 0 {
			response["repeatUntilMinutes"] = repeatUntilMinutes
		}
		if repeatUntilTick > 0 {
			response["repeatUntilTick"] = repeatUntilTick
		}

		return respondSuccess(c, http.StatusOK, "The task is set in motion.", response)
	}
}

func makeCancelBehaviorHandler(database *gorm.DB, cfg config.Config) echo.HandlerFunc {
	return func(c echo.Context) error {
		var req cancelBehaviorRequest
		if err := requestbind.JSON(c, &req, "invalid behavior cancel payload"); err != nil {
			return err
		}

		if req.BehaviorID == 0 {
			return echo.NewHTTPError(http.StatusBadRequest, "behaviorId is required")
		}

		resolvedPlayer := (*dal.Player)(nil)
		resolvedName := ""
		resolvedRealmID := uint(1)

		if cfg.MMOAuthEnabled {
			actor, ok := serverAuth.ActorFromContext(c.Request().Context())
			if !ok {
				return echo.NewHTTPError(http.StatusUnauthorized, "missing actor context")
			}

			requestedCharacterID := uint(0)
			if rawCharacterID := c.QueryParam("characterId"); rawCharacterID != "" {
				parsedID, parseErr := strconv.ParseUint(rawCharacterID, 10, 64)
				if parseErr != nil || parsedID == 0 {
					return echo.NewHTTPError(http.StatusBadRequest, "characterId must be a positive integer")
				}
				requestedCharacterID = uint(parsedID)
			}

			character, err := loadActorCharacter(c.Request().Context(), database, actor.AccountID, requestedCharacterID)
			if err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "failed to load character")
			}
			if character == nil {
				return echo.NewHTTPError(http.StatusBadRequest, "no active character found")
			}

			player, err := loadPlayerByID(c.Request().Context(), database, character.PlayerID)
			if err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "failed to load character player state")
			}
			if player == nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "character player state is missing")
			}

			resolvedPlayer = player
			resolvedName = character.Name
			if character.RealmID != 0 {
				resolvedRealmID = character.RealmID
			}
		} else {
			player, err := loadPrimaryPlayer(c.Request().Context(), database)
			if err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "failed to load primary player")
			}
			if player == nil {
				return echo.NewHTTPError(http.StatusBadRequest, "create a new game first")
			}

			resolvedPlayer = player
			resolvedName = player.Name
		}

		currentTick, err := gameplay.CurrentWorldTickForRealm(c.Request().Context(), database, resolvedRealmID)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load world tick")
		}

		cancelled, err := gameplay.CancelPlayerBehavior(c.Request().Context(), database, resolvedPlayer.ID, req.BehaviorID, currentTick, resolvedRealmID)
		if err != nil {
			if errors.Is(err, gameplay.ErrBehaviorNotCancelable) {
				return echo.NewHTTPError(http.StatusConflict, err.Error())
			}
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}

		return respondSuccess(c, http.StatusOK, "The task was cancelled.", map[string]any{"behavior": cancelled, "player": resolvedName})
	}
}

func loadActorCharacter(ctx context.Context, database *gorm.DB, accountID uint, characterID uint) (*dal.Character, error) {
	const defaultRealmID uint = 1

	character := &dal.Character{}
	query := database.WithContext(ctx).
		Where("account_id = ? AND status = ?", accountID, "active")

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

func loadPlayerByID(ctx context.Context, database *gorm.DB, playerID uint) (*dal.Player, error) {
	player := &dal.Player{}
	result := database.WithContext(ctx).Where("id = ?", playerID).Limit(1).Find(player)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}

	return player, nil
}

func resolveSystemPlayerContext(c echo.Context, database *gorm.DB, cfg config.Config) (*dal.Player, string, uint, error) {
	resolvedRealmID := uint(1)
	if cfg.MMOAuthEnabled {
		actor, ok := serverAuth.ActorFromContext(c.Request().Context())
		if !ok {
			return nil, "", 0, echo.NewHTTPError(http.StatusUnauthorized, "missing actor context")
		}

		requestedCharacterID := uint(0)
		if rawCharacterID := c.QueryParam("characterId"); rawCharacterID != "" {
			parsedID, parseErr := strconv.ParseUint(rawCharacterID, 10, 64)
			if parseErr != nil || parsedID == 0 {
				return nil, "", 0, echo.NewHTTPError(http.StatusBadRequest, "characterId must be a positive integer")
			}
			requestedCharacterID = uint(parsedID)
		}

		character, err := loadActorCharacter(c.Request().Context(), database, actor.AccountID, requestedCharacterID)
		if err != nil {
			return nil, "", 0, echo.NewHTTPError(http.StatusInternalServerError, "failed to load character")
		}
		if character == nil {
			return nil, "", 0, echo.NewHTTPError(http.StatusBadRequest, "complete onboarding first")
		}

		player, err := loadPlayerByID(c.Request().Context(), database, character.PlayerID)
		if err != nil {
			return nil, "", 0, echo.NewHTTPError(http.StatusInternalServerError, "failed to load character player state")
		}
		if player == nil {
			return nil, "", 0, echo.NewHTTPError(http.StatusInternalServerError, "character player state is missing")
		}

		if character.RealmID != 0 {
			resolvedRealmID = character.RealmID
		}
		return player, character.Name, resolvedRealmID, nil
	}

	player, err := loadPrimaryPlayer(c.Request().Context(), database)
	if err != nil {
		return nil, "", 0, echo.NewHTTPError(http.StatusInternalServerError, "failed to load primary player")
	}
	if player == nil {
		return nil, "", 0, echo.NewHTTPError(http.StatusBadRequest, "create a new game first")
	}
	return player, player.Name, resolvedRealmID, nil
}

func makeBehaviorCatalogHandler(database *gorm.DB, cfg config.Config) echo.HandlerFunc {
	return func(c echo.Context) error {
		inventory := map[string]int64{}
		stats := map[string]int64{}
		unlockSet := map[string]struct{}{}
		consumedBehaviorSet := map[string]struct{}{}
		hasPrimaryPlayer := false

		resolvedPlayer := (*dal.Player)(nil)
		resolvedRealmID := uint(1)
		if cfg.MMOAuthEnabled {
			actor, ok := serverAuth.ActorFromContext(c.Request().Context())
			if !ok {
				return echo.NewHTTPError(http.StatusUnauthorized, "missing actor context")
			}

			requestedCharacterID := uint(0)
			if rawCharacterID := c.QueryParam("characterId"); rawCharacterID != "" {
				parsedID, parseErr := strconv.ParseUint(rawCharacterID, 10, 64)
				if parseErr != nil || parsedID == 0 {
					return echo.NewHTTPError(http.StatusBadRequest, "characterId must be a positive integer")
				}
				requestedCharacterID = uint(parsedID)
			}

			character, err := loadActorCharacter(c.Request().Context(), database, actor.AccountID, requestedCharacterID)
			if err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "failed to load character")
			}
			if character != nil {
				resolvedRealmID = character.RealmID
				player, err := loadPlayerByID(c.Request().Context(), database, character.PlayerID)
				if err != nil {
					return echo.NewHTTPError(http.StatusInternalServerError, "failed to load character player state")
				}
				if player != nil {
					resolvedPlayer = player
				}
			}
		} else {
			primaryPlayer, err := loadPrimaryPlayer(c.Request().Context(), database)
			if err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "failed to load primary player")
			}
			resolvedPlayer = primaryPlayer
		}

		if resolvedPlayer != nil {
			hasPrimaryPlayer = true

			snapshot, err := gameplay.LoadWorldSnapshot(c.Request().Context(), database, resolvedPlayer.ID, resolvedRealmID)
			if err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "failed to load gameplay snapshot")
			}
			inventory = snapshot.Inventory
			stats = snapshot.Stats

			unlocks := make([]dal.PlayerUnlock, 0)
			if err := database.WithContext(c.Request().Context()).
				Where("realm_id = ? AND player_id = ?", resolvedRealmID, resolvedPlayer.ID).
				Find(&unlocks).Error; err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "failed to load player unlocks")
			}

			for _, unlock := range unlocks {
				unlockSet[unlock.UnlockKey] = struct{}{}
			}

			consumed, consumedErr := loadConsumedBehaviorSet(c.Request().Context(), database, resolvedPlayer.ID, resolvedRealmID)
			if consumedErr != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "failed to load consumed behaviors")
			}
			consumedBehaviorSet = consumed
		}

		definitions := gameplay.SortBehaviorDefinitions(gameplay.ListBehaviorDefinitions())
		catalog := make([]map[string]any, 0, len(definitions))
		for _, definition := range definitions {
			if definition.ActorType != gameplay.ActorPlayer {
				continue
			}

			available, queueVisible, unavailableReason := evaluateBehaviorAvailability(definition, inventory, stats, unlockSet, consumedBehaviorSet, hasPrimaryPlayer)
			displayName := gameplay.BehaviorDisplayName(definition)

			entry := map[string]any{
				"key":                   definition.Key,
				"name":                  displayName,
				"label":                 displayName,
				"category":              behaviorCategory(definition),
				"summary":               definition.Summary,
				"actorType":             definition.ActorType,
				"exclusiveGroup":        definition.ExclusiveGroup,
				"durationMinutes":       definition.DurationMinutes,
				"staminaCost":           definition.StaminaCost,
				"scheduleModes":         normalizeScheduleModes(definition.ScheduleModes),
				"singleUsePerAscension": definition.SingleUsePerAscension,
				"consumedThisAscension": definition.SingleUsePerAscension && containsSetKey(consumedBehaviorSet, definition.Key),
				"available":             available,
				"queueVisible":          queueVisible,
				"unavailableReason":     unavailableReason,
				"requirements": map[string]any{
					"unlocks": definition.Requirements.Unlocks,
					"items":   definition.Requirements.Items,
				},
				"costs":              definition.Costs,
				"statDeltas":         definition.StatDeltas,
				"outputs":            definition.Outputs,
				"outputExpressions":  definition.OutputExpressions,
				"outputChances":      definition.OutputChances,
				"grantsUnlocks":      definition.GrantsUnlocks,
				"requiresMarketOpen": definition.RequiresMarketOpen,
				"requiresNight":      definition.RequiresNight,
				"marketEffects":      definition.MarketEffects,
			}
			if definition.RequiresMarketOpen {
				entry["marketWaitDefaultMinutes"] = gameplay.DefaultMarketWaitDurationMinutes()
				entry["marketWaitMaxMinutes"] = gameplay.MaxMarketWaitDurationMinutes()
			}
			catalog = append(catalog, entry)
		}

		return respondSuccess(c, http.StatusOK, "Known activities are listed.", map[string]any{"behaviors": catalog})
	}
}

func evaluateBehaviorAvailability(
	definition gameplay.BehaviorDefinition,
	inventory map[string]int64,
	stats map[string]int64,
	unlockSet map[string]struct{},
	consumedBehaviorSet map[string]struct{},
	hasPrimaryPlayer bool,
) (bool, bool, string) {
	if !hasPrimaryPlayer {
		return false, false, "Create a new game to unlock behaviors."
	}

	if definition.SingleUsePerAscension {
		if _, consumed := consumedBehaviorSet[definition.Key]; consumed {
			return false, false, "Already completed this ascension"
		}
	}

	for _, unlock := range definition.Requirements.Unlocks {
		if _, ok := unlockSet[unlock]; !ok {
			return false, false, "Requires unlock: " + gameplay.HumanizeIdentifier(unlock)
		}
	}

	for itemKey, requiredQuantity := range definition.Requirements.Items {
		if requiredQuantity <= 0 {
			continue
		}

		if inventory[itemKey] < requiredQuantity {
			return false, true, "Requires " + strconv.FormatInt(requiredQuantity, 10) + " " + gameplay.HumanizeIdentifier(itemKey)
		}
	}

	if definition.StaminaCost > 0 && stats["stamina"] < definition.StaminaCost {
		return false, true, "Requires " + strconv.FormatInt(definition.StaminaCost, 10) + " stamina"
	}

	return true, true, ""
}

func behaviorCategory(definition gameplay.BehaviorDefinition) string {
	key := strings.TrimSpace(definition.Key)
	if strings.HasPrefix(key, "player_sell_") || definition.RequiresMarketOpen {
		return "Market"
	}

	if len(definition.GrantsUnlocks) > 0 {
		return "Unlocks"
	}

	if strings.Contains(key, "rest") {
		return "Recovery"
	}

	if definition.ExclusiveGroup != "" || len(definition.StatDeltas) > 0 || strings.Contains(key, "training") || strings.Contains(key, "pushups") {
		return "Training"
	}

	if strings.Contains(key, "chop") || strings.Contains(key, "scavenge") || len(definition.Outputs) > 0 || len(definition.OutputExpressions) > 0 {
		return "Gathering"
	}

	return "General"
}

func loadConsumedBehaviorSet(ctx context.Context, database *gorm.DB, playerID uint, realmID uint) (map[string]struct{}, error) {
	rows := make([]struct {
		Key string
	}, 0)
	err := database.WithContext(ctx).
		Model(&dal.BehaviorInstance{}).
		Select("DISTINCT key").
		Where("realm_id = ? AND actor_type = ? AND actor_id = ? AND state IN ?", realmID, gameplay.ActorPlayer, playerID, []string{"queued", "active", "completed"}).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}

	set := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		key := strings.TrimSpace(row.Key)
		if key == "" {
			continue
		}
		set[key] = struct{}{}
	}

	return set, nil
}

func containsSetKey(values map[string]struct{}, key string) bool {
	_, ok := values[key]
	return ok
}

func normalizeScheduleModes(configured []string) []string {
	if len(configured) == 0 {
		return []string{"once", "repeat", "repeat-until"}
	}

	normalized := make([]string, 0, len(configured))
	seen := map[string]struct{}{}
	for _, mode := range configured {
		candidate := strings.ToLower(strings.TrimSpace(mode))
		if candidate == "" {
			continue
		}
		if _, exists := seen[candidate]; exists {
			continue
		}
		normalized = append(normalized, candidate)
		seen[candidate] = struct{}{}
	}
	if len(normalized) == 0 {
		return []string{"once"}
	}
	return normalized
}

func makeUpgradeCatalogHandler(database *gorm.DB, cfg config.Config) echo.HandlerFunc {
	return func(c echo.Context) error {
		inventory := map[string]int64{}
		unlockSet := map[string]struct{}{}
		purchaseCounts := map[string]int64{}
		hasPrimaryPlayer := false

		resolvedPlayer := (*dal.Player)(nil)
		resolvedRealmID := uint(1)
		if cfg.MMOAuthEnabled {
			actor, ok := serverAuth.ActorFromContext(c.Request().Context())
			if !ok {
				return echo.NewHTTPError(http.StatusUnauthorized, "missing actor context")
			}

			requestedCharacterID := uint(0)
			if rawCharacterID := c.QueryParam("characterId"); rawCharacterID != "" {
				parsedID, parseErr := strconv.ParseUint(rawCharacterID, 10, 64)
				if parseErr != nil || parsedID == 0 {
					return echo.NewHTTPError(http.StatusBadRequest, "characterId must be a positive integer")
				}
				requestedCharacterID = uint(parsedID)
			}

			character, err := loadActorCharacter(c.Request().Context(), database, actor.AccountID, requestedCharacterID)
			if err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "failed to load character")
			}
			if character != nil {
				resolvedRealmID = character.RealmID
				player, err := loadPlayerByID(c.Request().Context(), database, character.PlayerID)
				if err != nil {
					return echo.NewHTTPError(http.StatusInternalServerError, "failed to load character player state")
				}
				if player != nil {
					resolvedPlayer = player
				}
			}
		} else {
			primaryPlayer, err := loadPrimaryPlayer(c.Request().Context(), database)
			if err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "failed to load primary player")
			}
			resolvedPlayer = primaryPlayer
		}

		if resolvedPlayer != nil {
			hasPrimaryPlayer = true

			snapshot, err := gameplay.LoadWorldSnapshot(c.Request().Context(), database, resolvedPlayer.ID, resolvedRealmID)
			if err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "failed to load gameplay snapshot")
			}
			inventory = snapshot.Inventory

			unlocks := make([]dal.PlayerUnlock, 0)
			if err := database.WithContext(c.Request().Context()).
				Where("realm_id = ? AND player_id = ?", resolvedRealmID, resolvedPlayer.ID).
				Find(&unlocks).Error; err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "failed to load player unlocks")
			}
			for _, unlock := range unlocks {
				unlockSet[unlock.UnlockKey] = struct{}{}
			}

			counts, countErr := gameplay.LoadPlayerUpgradeCounts(c.Request().Context(), database, resolvedPlayer.ID, resolvedRealmID)
			if countErr != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "failed to load upgrade state")
			}
			purchaseCounts = counts
		}

		definitions := gameplay.SortUpgradeDefinitions(gameplay.ListUpgradeDefinitions())
		catalog := make([]map[string]any, 0, len(definitions))
		for _, definition := range definitions {
			purchaseCount := purchaseCounts[definition.Key]
			nextCosts := gameplay.ProjectedUpgradeCosts(definition, purchaseCount)
			nextOutputs := gameplay.ProjectedUpgradeOutputs(definition, purchaseCount)
			available, unavailableReason := evaluateUpgradeAvailability(definition, inventory, unlockSet, purchaseCount, hasPrimaryPlayer)
			catalog = append(catalog, map[string]any{
				"key":               definition.Key,
				"name":              definition.Name,
				"summary":           definition.Summary,
				"category":          definition.Category,
				"gateTypes":         normalizeUpgradeGateTypes(definition),
				"maxPurchases":      definition.MaxPurchases,
				"purchaseCount":     purchaseCount,
				"costScaling":       definition.CostScaling,
				"outputScaling":     definition.OutputScaling,
				"available":         available,
				"unavailableReason": unavailableReason,
				"requirements": map[string]any{
					"unlocks": definition.Requirements.Unlocks,
					"items":   definition.Requirements.Items,
				},
				"costs":     definition.Costs,
				"nextCosts": nextCosts,
				"outputs": map[string]any{
					"queueSlotsDelta": definition.Outputs.QueueSlotsDelta,
					"unlocks":         definition.Outputs.Unlocks,
					"items":           definition.Outputs.Items,
					"statDeltas":      definition.Outputs.StatDeltas,
				},
				"nextOutputs": map[string]any{
					"queueSlotsDelta": nextOutputs.QueueSlotsDelta,
					"unlocks":         nextOutputs.Unlocks,
					"items":           nextOutputs.Items,
					"statDeltas":      nextOutputs.StatDeltas,
				},
			})
		}

		return respondSuccess(c, http.StatusOK, "Known upgrades are listed.", map[string]any{"upgrades": catalog})
	}
}

func makePurchaseUpgradeHandler(database *gorm.DB, cfg config.Config) echo.HandlerFunc {
	return func(c echo.Context) error {
		var req purchaseUpgradeRequest
		if err := requestbind.JSON(c, &req, "invalid upgrade payload"); err != nil {
			return err
		}

		key := strings.TrimSpace(req.UpgradeKey)
		if key == "" {
			return echo.NewHTTPError(http.StatusBadRequest, "upgradeKey is required")
		}

		resolvedPlayer := (*dal.Player)(nil)
		resolvedName := ""
		resolvedRealmID := uint(1)

		if cfg.MMOAuthEnabled {
			actor, ok := serverAuth.ActorFromContext(c.Request().Context())
			if !ok {
				return echo.NewHTTPError(http.StatusUnauthorized, "missing actor context")
			}

			requestedCharacterID := uint(0)
			hasRequestedCharacter := false
			if rawCharacterID := c.QueryParam("characterId"); rawCharacterID != "" {
				parsedID, parseErr := strconv.ParseUint(rawCharacterID, 10, 64)
				if parseErr != nil || parsedID == 0 {
					return echo.NewHTTPError(http.StatusBadRequest, "characterId must be a positive integer")
				}
				hasRequestedCharacter = true
				requestedCharacterID = uint(parsedID)
			}

			character, err := loadActorCharacter(c.Request().Context(), database, actor.AccountID, requestedCharacterID)
			if err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "failed to load character")
			}
			if character == nil {
				if hasRequestedCharacter {
					return echo.NewHTTPError(http.StatusNotFound, "character not found for account")
				}
				return echo.NewHTTPError(http.StatusBadRequest, "complete onboarding first")
			}

			player, err := loadPlayerByID(c.Request().Context(), database, character.PlayerID)
			if err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "failed to load character player state")
			}
			if player == nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "character player state is missing")
			}

			resolvedPlayer = player
			resolvedName = character.Name
			if character.RealmID != 0 {
				resolvedRealmID = character.RealmID
			}
		} else {
			player, err := loadPrimaryPlayer(c.Request().Context(), database)
			if err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "failed to load primary player")
			}
			if player == nil {
				return echo.NewHTTPError(http.StatusBadRequest, "create a new game first")
			}

			resolvedPlayer = player
			resolvedName = player.Name
		}

		nextCount, err := gameplay.PurchaseUpgradeForPlayer(c.Request().Context(), database, resolvedPlayer.ID, resolvedRealmID, key)
		if err != nil {
			if errors.Is(err, gameplay.ErrUpgradeNotFound) {
				return echo.NewHTTPError(http.StatusBadRequest, "unknown upgrade key")
			}
			if errors.Is(err, gameplay.ErrUpgradeMaxed) {
				return echo.NewHTTPError(http.StatusConflict, "upgrade already at maximum purchases")
			}
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}

		slots, slotErr := gameplay.QueueSlotSummaryForPlayer(c.Request().Context(), database, resolvedPlayer.ID, resolvedRealmID)
		if slotErr != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load queue slot summary")
		}

		return respondSuccess(c, http.StatusOK, "Upgrade purchased.", map[string]any{
			"upgradeKey":          key,
			"player":              resolvedName,
			"purchaseCount":       nextCount,
			"queueSlotsTotal":     slots.Total,
			"queueSlotsUsed":      slots.Used,
			"queueSlotsAvailable": slots.Available,
		})
	}
}

func evaluateUpgradeAvailability(definition gameplay.UpgradeDefinition, inventory map[string]int64, unlockSet map[string]struct{}, purchaseCount int64, hasPrimaryPlayer bool) (bool, string) {
	if !hasPrimaryPlayer {
		return false, "Create a new game to unlock upgrades."
	}

	if definition.MaxPurchases > 0 && purchaseCount >= definition.MaxPurchases {
		return false, "Upgrade already maxed for this ascension"
	}

	for _, unlock := range definition.Requirements.Unlocks {
		if _, ok := unlockSet[unlock]; !ok {
			return false, "Requires unlock: " + gameplay.HumanizeIdentifier(unlock)
		}
	}

	nextCosts := gameplay.ProjectedUpgradeCosts(definition, purchaseCount)
	for itemKey, requiredQuantity := range nextCosts {
		if requiredQuantity <= 0 {
			continue
		}
		if inventory[itemKey] < requiredQuantity {
			return false, "Requires " + strconv.FormatInt(requiredQuantity, 10) + " " + gameplay.HumanizeIdentifier(itemKey)
		}
	}

	for statKey, requiredValue := range definition.Requirements.Items {
		if requiredValue <= 0 {
			continue
		}
		if inventory[statKey] < requiredValue {
			return false, "Requires " + strconv.FormatInt(requiredValue, 10) + " " + gameplay.HumanizeIdentifier(statKey)
		}
	}

	return true, ""
}

func makeMarketStatusHandler(database *gorm.DB, cfg config.Config) echo.HandlerFunc {
	return func(c echo.Context) error {
		realmID, err := resolveRealmIDForSystemRead(c, database, cfg)
		if err != nil {
			return err
		}

		currentTick, err := gameplay.CurrentWorldTickForRealm(c.Request().Context(), database, realmID)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load world tick")
		}

		status, err := gameplay.GetMarketStatus(c.Request().Context(), database, currentTick, realmID)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load market status")
		}

		message := "Market ticker snapshot updated."
		if !status.IsOpen {
			message = "Market is currently closed for the overnight session."
		}

		return respondSuccess(c, http.StatusOK, message, status)
	}
}

func makeMarketHistoryHandler(database *gorm.DB, cfg config.Config) echo.HandlerFunc {
	return func(c echo.Context) error {
		realmID, err := resolveRealmIDForSystemRead(c, database, cfg)
		if err != nil {
			return err
		}

		query, err := parseMarketHistoryQuery(c, realmID)
		if err != nil {
			return err
		}

		history, err := gameplay.GetMarketHistory(c.Request().Context(), database, query.Symbol, query.Limit, query.Realm)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load market history")
		}

		return respondSuccess(c, http.StatusOK, "Market history loaded.", map[string]any{
			"symbol":  strings.TrimSpace(query.Symbol),
			"limit":   query.Limit,
			"realmId": query.Realm,
			"history": history,
		})
	}
}

func makeMarketCandlesHandler(database *gorm.DB, cfg config.Config) echo.HandlerFunc {
	return func(c echo.Context) error {
		realmID, err := resolveRealmIDForSystemRead(c, database, cfg)
		if err != nil {
			return err
		}

		query, err := parseMarketCandleQuery(c, realmID)
		if err != nil {
			return err
		}

		candles, err := gameplay.GetMarketCandles(c.Request().Context(), database, query.Symbol, query.BucketTicks, query.Limit, query.Realm)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load market candles")
		}

		return respondSuccess(c, http.StatusOK, "Market candles loaded.", map[string]any{
			"symbol":      strings.TrimSpace(query.Symbol),
			"limit":       query.Limit,
			"bucketTicks": query.BucketTicks,
			"realmId":     query.Realm,
			"candles":     candles,
		})
	}
}

func makeMarketOverviewHandler(database *gorm.DB, cfg config.Config) echo.HandlerFunc {
	return func(c echo.Context) error {
		realmID, err := resolveRealmIDForSystemRead(c, database, cfg)
		if err != nil {
			return err
		}

		currentTick, err := gameplay.CurrentWorldTickForRealm(c.Request().Context(), database, realmID)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load world tick")
		}

		status, err := gameplay.GetMarketStatus(c.Request().Context(), database, currentTick, realmID)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load market status")
		}

		bucketTicks := int64(30)
		if rawBucketTicks := strings.TrimSpace(c.QueryParam("bucketTicks")); rawBucketTicks != "" {
			parsed, parseErr := strconv.ParseInt(rawBucketTicks, 10, 64)
			if parseErr != nil || parsed <= 0 {
				return echo.NewHTTPError(http.StatusBadRequest, "bucketTicks must be a positive integer")
			}
			if parsed > 24*60 {
				parsed = 24 * 60
			}
			bucketTicks = parsed
		}

		limit := 60
		if rawLimit := strings.TrimSpace(c.QueryParam("limit")); rawLimit != "" {
			parsed, parseErr := strconv.Atoi(rawLimit)
			if parseErr != nil || parsed <= 0 {
				return echo.NewHTTPError(http.StatusBadRequest, "limit must be a positive integer")
			}
			if parsed > 500 {
				parsed = 500
			}
			limit = parsed
		}

		symbolRows := make([]map[string]any, 0, len(status.Tickers))
		for _, ticker := range status.Tickers {
			candles, candleErr := gameplay.GetMarketCandles(c.Request().Context(), database, ticker.Symbol, bucketTicks, limit, realmID)
			if candleErr != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "failed to load market overview candles")
			}

			symbolRows = append(symbolRows, map[string]any{
				"symbol":       ticker.Symbol,
				"currentPrice": ticker.Price,
				"delta":        ticker.Delta,
				"updatedTick":  ticker.UpdatedTick,
				"sessionState": ticker.SessionState,
				"liquidity":    ticker.Liquidity,
				"movement":     ticker.Movement,
				"candles":      candles,
			})
		}

		return respondSuccess(c, http.StatusOK, "Market overview loaded.", map[string]any{
			"tick":        status.Tick,
			"realmId":     realmID,
			"bucketTicks": bucketTicks,
			"limit":       limit,
			"symbols":     symbolRows,
		})
	}
}

func makeMarketPlaceOrderHandler(database *gorm.DB, cfg config.Config) echo.HandlerFunc {
	return func(c echo.Context) error {
		var req placeMarketOrderRequest
		if err := requestbind.JSON(c, &req, "invalid market order payload"); err != nil {
			return err
		}

		player, _, realmID, err := resolveSystemPlayerContext(c, database, cfg)
		if err != nil {
			return err
		}

		currentTick, tickErr := gameplay.CurrentWorldTickForRealm(c.Request().Context(), database, realmID)
		if tickErr != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load world tick")
		}

		cancelAfterMinutes, parseErr := parseDurationMinutes(strings.TrimSpace(req.CancelAfter), 24*60)
		if parseErr != nil {
			return echo.NewHTTPError(http.StatusBadRequest, parseErr.Error())
		}

		order, placeErr := gameplay.PlaceMarketOrder(c.Request().Context(), database, player.ID, realmID, currentTick, gameplay.PlaceMarketOrderRequest{
			ItemKey:            req.ItemKey,
			Side:               req.Side,
			Quantity:           req.Quantity,
			LimitPrice:         req.LimitPrice,
			CancelAfterMinutes: cancelAfterMinutes,
			ManualCancelFeeBps: req.ManualCancelFeeBps,
		})
		if placeErr != nil {
			return echo.NewHTTPError(http.StatusBadRequest, placeErr.Error())
		}

		return respondSuccess(c, http.StatusOK, "Market order placed.", map[string]any{"order": order})
	}
}

func makeMarketCancelOrderHandler(database *gorm.DB, cfg config.Config) echo.HandlerFunc {
	return func(c echo.Context) error {
		var req cancelMarketOrderRequest
		if err := requestbind.JSON(c, &req, "invalid market cancel payload"); err != nil {
			return err
		}
		if req.OrderID == 0 {
			return echo.NewHTTPError(http.StatusBadRequest, "orderId is required")
		}

		player, _, realmID, err := resolveSystemPlayerContext(c, database, cfg)
		if err != nil {
			return err
		}

		currentTick, tickErr := gameplay.CurrentWorldTickForRealm(c.Request().Context(), database, realmID)
		if tickErr != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load world tick")
		}

		order, cancelErr := gameplay.CancelMarketOrder(c.Request().Context(), database, player.ID, realmID, req.OrderID, currentTick)
		if cancelErr != nil {
			return echo.NewHTTPError(http.StatusBadRequest, cancelErr.Error())
		}

		return respondSuccess(c, http.StatusOK, "Market order cancelled.", map[string]any{"order": order})
	}
}

func makeMarketMyOrdersHandler(database *gorm.DB, cfg config.Config) echo.HandlerFunc {
	return func(c echo.Context) error {
		player, _, realmID, err := resolveSystemPlayerContext(c, database, cfg)
		if err != nil {
			return err
		}

		limit := 100
		if rawLimit := strings.TrimSpace(c.QueryParam("limit")); rawLimit != "" {
			parsed, parseErr := strconv.Atoi(rawLimit)
			if parseErr != nil || parsed <= 0 {
				return echo.NewHTTPError(http.StatusBadRequest, "limit must be a positive integer")
			}
			if parsed > 500 {
				parsed = 500
			}
			limit = parsed
		}

		state := strings.TrimSpace(c.QueryParam("state"))
		orders, listErr := gameplay.ListMarketOrdersForPlayer(c.Request().Context(), database, player.ID, realmID, state, limit)
		if listErr != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load market orders")
		}
		return respondSuccess(c, http.StatusOK, "Market orders loaded.", map[string]any{"orders": orders, "state": state, "limit": limit})
	}
}

func makeMarketOrderBookHandler(database *gorm.DB, cfg config.Config) echo.HandlerFunc {
	return func(c echo.Context) error {
		realmID, err := resolveRealmIDForSystemRead(c, database, cfg)
		if err != nil {
			return err
		}

		depth := 20
		if rawDepth := strings.TrimSpace(c.QueryParam("depth")); rawDepth != "" {
			parsed, parseErr := strconv.Atoi(rawDepth)
			if parseErr != nil || parsed <= 0 {
				return echo.NewHTTPError(http.StatusBadRequest, "depth must be a positive integer")
			}
			if parsed > 200 {
				parsed = 200
			}
			depth = parsed
		}

		book, bookErr := gameplay.GetMarketOrderBook(c.Request().Context(), database, realmID, strings.TrimSpace(c.QueryParam("symbol")), depth)
		if bookErr != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load market order book")
		}
		return respondSuccess(c, http.StatusOK, "Market order book loaded.", book)
	}
}

func makeMarketTradesHandler(database *gorm.DB, cfg config.Config) echo.HandlerFunc {
	return func(c echo.Context) error {
		realmID, err := resolveRealmIDForSystemRead(c, database, cfg)
		if err != nil {
			return err
		}

		limit := 100
		if rawLimit := strings.TrimSpace(c.QueryParam("limit")); rawLimit != "" {
			parsed, parseErr := strconv.Atoi(rawLimit)
			if parseErr != nil || parsed <= 0 {
				return echo.NewHTTPError(http.StatusBadRequest, "limit must be a positive integer")
			}
			if parsed > 500 {
				parsed = 500
			}
			limit = parsed
		}

		trades, tradesErr := gameplay.ListRecentMarketTrades(c.Request().Context(), database, realmID, strings.TrimSpace(c.QueryParam("symbol")), limit)
		if tradesErr != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load market trades")
		}
		return respondSuccess(c, http.StatusOK, "Recent market trades loaded.", map[string]any{"trades": trades, "limit": limit})
	}
}

func parseDurationMinutes(raw string, defaultMinutes int64) (int64, error) {
	if strings.TrimSpace(raw) == "" {
		return defaultMinutes, nil
	}
	duration, err := time.ParseDuration(strings.TrimSpace(raw))
	if err != nil {
		return 0, fmt.Errorf("cancelAfter must be a duration like 12h or 72h")
	}
	minutes := int64(duration.Minutes())
	if minutes <= 0 {
		return 0, fmt.Errorf("cancelAfter must be positive")
	}
	if minutes > 30*24*60 {
		minutes = 30 * 24 * 60
	}
	return minutes, nil
}

func parseMarketHistoryQuery(c echo.Context, fallbackRealmID uint) (marketHistoryQuery, error) {
	const defaultLimit = 100
	const maxLimit = 500

	limit := defaultLimit
	if rawLimit := strings.TrimSpace(c.QueryParam("limit")); rawLimit != "" {
		parsedLimit, parseErr := strconv.Atoi(rawLimit)
		if parseErr != nil || parsedLimit <= 0 {
			return marketHistoryQuery{}, echo.NewHTTPError(http.StatusBadRequest, "limit must be a positive integer")
		}
		if parsedLimit > maxLimit {
			parsedLimit = maxLimit
		}
		limit = parsedLimit
	}

	realmID, err := parseOptionalPositiveUintQuery(c.QueryParam("realmId"), "realmId", fallbackRealmID)
	if err != nil {
		return marketHistoryQuery{}, err
	}

	return marketHistoryQuery{
		Symbol: strings.TrimSpace(c.QueryParam("symbol")),
		Limit:  limit,
		Realm:  realmID,
	}, nil
}

func parseMarketCandleQuery(c echo.Context, fallbackRealmID uint) (marketCandleQuery, error) {
	const defaultLimit = 120
	const maxLimit = 500

	query := marketCandleQuery{Limit: defaultLimit, Realm: fallbackRealmID, BucketTicks: 30}

	if rawSymbol := strings.TrimSpace(c.QueryParam("symbol")); rawSymbol != "" {
		query.Symbol = rawSymbol
	}

	if rawLimit := strings.TrimSpace(c.QueryParam("limit")); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil || parsed <= 0 {
			return marketCandleQuery{}, echo.NewHTTPError(http.StatusBadRequest, "limit must be a positive integer")
		}
		if parsed > maxLimit {
			parsed = maxLimit
		}
		query.Limit = parsed
	}

	if rawBucketTicks := strings.TrimSpace(c.QueryParam("bucketTicks")); rawBucketTicks != "" {
		parsed, err := strconv.ParseInt(rawBucketTicks, 10, 64)
		if err != nil || parsed <= 0 {
			return marketCandleQuery{}, echo.NewHTTPError(http.StatusBadRequest, "bucketTicks must be a positive integer")
		}
		if parsed > 24*60 {
			parsed = 24 * 60
		}
		query.BucketTicks = parsed
	}

	if rawRealm := strings.TrimSpace(c.QueryParam("realmId")); rawRealm != "" {
		parsed, err := strconv.ParseUint(rawRealm, 10, 64)
		if err != nil || parsed == 0 {
			return marketCandleQuery{}, echo.NewHTTPError(http.StatusBadRequest, "realmId must be a positive integer")
		}
		query.Realm = uint(parsed)
	}

	if query.Symbol == "" {
		return marketCandleQuery{}, echo.NewHTTPError(http.StatusBadRequest, "symbol is required")
	}

	return query, nil
}

func resolveRealmIDForSystemRead(c echo.Context, database *gorm.DB, cfg config.Config) (uint, error) {
	realmID, err := parseOptionalPositiveUintQuery(c.QueryParam("realmId"), "realmId", 1)
	if err != nil {
		return 0, err
	}

	if !cfg.MMOAuthEnabled {
		return realmID, nil
	}

	actor, ok := serverAuth.ActorFromContext(c.Request().Context())
	if !ok {
		return 0, echo.NewHTTPError(http.StatusUnauthorized, "missing actor context")
	}

	characterID, err := parseOptionalPositiveUintQuery(c.QueryParam("characterId"), "characterId", 0)
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

	if strings.TrimSpace(c.QueryParam("realmId")) != "" && realmID != character.RealmID {
		return 0, echo.NewHTTPError(http.StatusForbidden, "realmId does not match authenticated character realm")
	}

	return character.RealmID, nil
}

func parseOptionalPositiveUintQuery(raw string, fieldName string, fallback uint) (uint, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return fallback, nil
	}

	parsed, err := strconv.ParseUint(trimmed, 10, 64)
	if err != nil || parsed == 0 {
		return 0, echo.NewHTTPError(http.StatusBadRequest, fieldName+" must be a positive integer")
	}

	return uint(parsed), nil
}

func makeAscendHandler(database *gorm.DB, cfg config.Config) echo.HandlerFunc {
	return func(c echo.Context) error {
		if cfg.MMOAuthEnabled {
			actor, ok := serverAuth.ActorFromContext(c.Request().Context())
			if !ok {
				return echo.NewHTTPError(http.StatusUnauthorized, "missing actor context")
			}

			requestedCharacterID := uint(0)
			hasRequestedCharacter := false
			if rawCharacterID := c.QueryParam("characterId"); rawCharacterID != "" {
				parsedID, parseErr := strconv.ParseUint(rawCharacterID, 10, 64)
				if parseErr != nil || parsedID == 0 {
					return echo.NewHTTPError(http.StatusBadRequest, "characterId must be a positive integer")
				}
				hasRequestedCharacter = true
				requestedCharacterID = uint(parsedID)
			}

			character, err := loadActorCharacter(c.Request().Context(), database, actor.AccountID, requestedCharacterID)
			if err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "failed to load character")
			}
			if character == nil {
				if hasRequestedCharacter {
					return echo.NewHTTPError(http.StatusNotFound, "character not found for account")
				}
				return echo.NewHTTPError(http.StatusBadRequest, "complete onboarding first")
			}

			player, err := loadPlayerByID(c.Request().Context(), database, character.PlayerID)
			if err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "failed to load character player state")
			}
			if player == nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "character player state is missing")
			}

			var req ascendRequest
			if err := requestbind.JSON(c, &req, "invalid ascension payload"); err != nil {
				return err
			}

			name := strings.TrimSpace(req.Name)
			if name == "" {
				name = player.Name
			}
			if name == "" {
				name = character.Name
			}
			if name == "" {
				name = "Wanderer"
			}

			eligibility, err := gameplay.GetAscensionEligibilityForPlayer(c.Request().Context(), database, player.ID, character.RealmID)
			if err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "failed to evaluate ascension eligibility")
			}
			if !eligibility.Available {
				return echo.NewHTTPError(http.StatusConflict, eligibility.Reason)
			}

			count, bonus, err := gameplay.AscendForPlayerRealm(c.Request().Context(), database, player.ID, character.RealmID, name)
			if err != nil {
				if errors.Is(err, gameplay.ErrAscensionNotEligible) {
					return echo.NewHTTPError(http.StatusConflict, err.Error())
				}
				return echo.NewHTTPError(http.StatusInternalServerError, "failed to ascend")
			}

			return respondSuccess(c, http.StatusOK, "A new cycle begins for your character, tempered by prior echoes.", map[string]any{"ascensionCount": count, "wealthBonusPct": bonus, "realmId": character.RealmID, "characterId": character.ID})
		}

		var req ascendRequest
		if err := requestbind.JSON(c, &req, "invalid ascension payload"); err != nil {
			return err
		}

		name := strings.TrimSpace(req.Name)
		if name == "" {
			player, err := loadPrimaryPlayer(c.Request().Context(), database)
			if err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "failed to load player")
			}
			if player != nil {
				name = player.Name
			}
		}
		if name == "" {
			name = "Wanderer"
		}

		player, err := loadPrimaryPlayer(c.Request().Context(), database)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load player")
		}
		if player == nil {
			return echo.NewHTTPError(http.StatusBadRequest, "create a new game first")
		}

		eligibility, err := gameplay.GetAscensionEligibility(c.Request().Context(), database)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to evaluate ascension eligibility")
		}
		if !eligibility.Available {
			return echo.NewHTTPError(http.StatusConflict, eligibility.Reason)
		}

		count, bonus, err := gameplay.Ascend(c.Request().Context(), database, name)
		if err != nil {
			if errors.Is(err, gameplay.ErrAscensionNotEligible) {
				return echo.NewHTTPError(http.StatusConflict, err.Error())
			}
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to ascend")
		}

		return respondSuccess(c, http.StatusOK, "The old life fades; power echoes into the next journey.", map[string]any{"ascensionCount": count, "wealthBonusPct": bonus})
	}
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

func queueBehaviorErrorStatus(err error) int {
	if errors.Is(err, gameplay.ErrBehaviorConflict) || errors.Is(err, gameplay.ErrQueueSlotsFull) || errors.Is(err, gameplay.ErrBehaviorNotCancelable) {
		return http.StatusConflict
	}

	return http.StatusBadRequest
}

func normalizeUpgradeGateTypes(definition gameplay.UpgradeDefinition) []string {
	if len(definition.GateTypes) > 0 {
		values := make([]string, 0, len(definition.GateTypes))
		seen := map[string]struct{}{}
		for _, entry := range definition.GateTypes {
			trimmed := strings.ToLower(strings.TrimSpace(entry))
			if trimmed == "" {
				continue
			}
			if _, exists := seen[trimmed]; exists {
				continue
			}
			values = append(values, trimmed)
			seen[trimmed] = struct{}{}
		}
		if len(values) > 0 {
			return values
		}
	}

	derived := make([]string, 0, 3)
	if len(definition.Requirements.Unlocks) > 0 {
		derived = append(derived, "unlock")
	}
	if len(definition.Requirements.Items) > 0 || len(definition.Costs) > 0 {
		derived = append(derived, "resource")
	}
	if len(derived) == 0 {
		return []string{"none"}
	}
	return derived
}

func loadCurrentSave(ctx context.Context, database *gorm.DB) (saveGame, error) {
	players := make([]dal.Player, 0)
	if err := database.WithContext(ctx).Order("id ASC").Find(&players).Error; err != nil {
		return saveGame{}, echo.NewHTTPError(http.StatusInternalServerError, "failed to load players")
	}

	worldState := dal.WorldState{}
	result := database.WithContext(ctx).Order("id ASC").First(&worldState)
	if result.Error != nil && !errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return saveGame{}, echo.NewHTTPError(http.StatusInternalServerError, "failed to load world state")
	}

	playerNames := make([]string, 0, len(players))
	for _, player := range players {
		playerNames = append(playerNames, player.Name)
	}

	return saveGame{
		Version:        1,
		Players:        playerNames,
		SimulationTick: worldState.SimulationTick,
	}, nil
}

func encodeSave(save saveGame) (string, error) {
	raw, err := json.Marshal(save)
	if err != nil {
		return "", err
	}

	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func parseGameDurationMinutes(raw string) (int64, error) {
	value := strings.TrimSpace(strings.ToLower(raw))
	if value == "" {
		return 0, nil
	}

	if strings.HasSuffix(value, "m") {
		minutes, err := strconv.ParseInt(strings.TrimSuffix(value, "m"), 10, 64)
		if err != nil || minutes <= 0 {
			return 0, errors.New("marketWait must be a positive duration such as 90m, 12h, or 2d")
		}
		return minutes, nil
	}

	if strings.HasSuffix(value, "h") {
		hours, err := strconv.ParseInt(strings.TrimSuffix(value, "h"), 10, 64)
		if err != nil || hours <= 0 {
			return 0, errors.New("marketWait must be a positive duration such as 90m, 12h, or 2d")
		}
		return hours * 60, nil
	}

	if strings.HasSuffix(value, "d") {
		days, err := strconv.ParseInt(strings.TrimSuffix(value, "d"), 10, 64)
		if err != nil || days <= 0 {
			return 0, errors.New("marketWait must be a positive duration such as 90m, 12h, or 2d")
		}
		return days * 24 * 60, nil
	}

	return 0, errors.New("marketWait must include a unit (m, h, d), for example 90m, 12h, or 2d")
}

func parseBehaviorQueueMode(rawMode string, rawRepeatUntil string) (string, int64, error) {
	mode := strings.ToLower(strings.TrimSpace(rawMode))
	if mode == "" {
		mode = "once"
	}
	if mode != "once" && mode != "repeat" && mode != "repeat-until" {
		return "", 0, errors.New("mode must be once, repeat, or repeat-until")
	}

	repeatUntilMinutes, err := parseGameDurationMinutes(rawRepeatUntil)
	if err != nil {
		return "", 0, err
	}

	if mode == "repeat-until" {
		if repeatUntilMinutes <= 0 {
			return "", 0, errors.New("repeatUntil is required when mode is repeat-until")
		}
		return mode, repeatUntilMinutes, nil
	}

	if repeatUntilMinutes > 0 {
		return "", 0, errors.New("repeatUntil is only supported when mode is repeat-until")
	}

	return mode, 0, nil
}

func replaceGameState(ctx context.Context, database *gorm.DB, save saveGame) error {
	return database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
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

		if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&dal.AscensionState{}).Error; err != nil {
			return err
		}

		if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&dal.MarketPrice{}).Error; err != nil {
			return err
		}

		if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&dal.Player{}).Error; err != nil {
			return err
		}

		if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&dal.WorldState{}).Error; err != nil {
			return err
		}

		players := make([]dal.Player, 0, len(save.Players))
		for _, name := range save.Players {
			trimmedName := strings.TrimSpace(name)
			if trimmedName == "" {
				continue
			}
			players = append(players, dal.Player{Name: trimmedName})
		}

		if len(players) > 0 {
			if err := tx.Create(&players).Error; err != nil {
				return err
			}
		}

		worldState := dal.WorldState{SimulationTick: save.SimulationTick}
		if err := tx.Create(&worldState).Error; err != nil {
			return err
		}

		runtimeState, err := loadOrInitRuntimeState(ctx, tx, 1)
		if err != nil {
			return err
		}

		if err := tx.Model(&dal.WorldRuntimeState{}).
			Where("id = ?", runtimeState.ID).
			Updates(map[string]any{
				"last_processed_tick_at": time.Now().UTC(),
				"carry_game_minutes":     0,
				"pending_behaviors_json": "[]",
			}).Error; err != nil {
			return err
		}

		return nil
	})
}

func loadOrInitRuntimeState(ctx context.Context, database *gorm.DB, realmID uint) (*dal.WorldRuntimeState, error) {
	if realmID == 0 {
		realmID = 1
	}
	runtimeState := &dal.WorldRuntimeState{}
	result := database.WithContext(ctx).Where("realm_id = ? AND key = ?", realmID, "world").Limit(1).Find(runtimeState)
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
		Key:                  "world",
		LastProcessedTickAt:  time.Now().UTC(),
		CarryGameMinutes:     0,
		PendingBehaviorsJSON: "[]",
	}

	if createErr := database.WithContext(ctx).Create(initialState).Error; createErr != nil {
		return nil, createErr
	}

	return initialState, nil
}

func loadPrimaryPlayer(ctx context.Context, database *gorm.DB) (*dal.Player, error) {
	const defaultRealmID uint = 1

	character := &dal.Character{}
	characterResult := database.WithContext(ctx).
		Where("realm_id = ? AND status = ?", defaultRealmID, "active").
		Order("is_primary DESC, id ASC").
		Limit(1).
		Find(character)
	if characterResult.Error != nil {
		return nil, characterResult.Error
	}
	if characterResult.RowsAffected == 0 {
		return nil, nil
	}

	player := &dal.Player{}
	result := database.WithContext(ctx).Where("id = ?", character.PlayerID).Limit(1).Find(player)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	return player, nil
}

func currentVersionData() versionData {
	gameDataMeta := versionGameData{}
	if info, err := gamedata.Info(); err == nil {
		gameDataMeta = versionGameData{
			ManifestVersion: info.ManifestVersion,
			FilesHash:       info.FilesHash,
		}
	}

	return versionData{
		API:      version.APIVersion,
		Backend:  version.BackendVersion,
		Frontend: version.FrontendVersion,
		GameData: gameDataMeta,
	}
}
