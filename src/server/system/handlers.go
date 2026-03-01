package system

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/asciifaceman/lived/pkg/config"
	"github.com/asciifaceman/lived/pkg/dal"
	"github.com/asciifaceman/lived/pkg/idempotency"
	"github.com/asciifaceman/lived/pkg/ratelimit"
	"github.com/asciifaceman/lived/pkg/version"
	"github.com/asciifaceman/lived/src/gameplay"
	serverAuth "github.com/asciifaceman/lived/src/server/auth"
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
}

type ascendRequest struct {
	Name string `json:"name"`
}

type marketHistoryQuery struct {
	Symbol string `query:"symbol"`
	Limit  int    `query:"limit"`
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
	Stats               map[string]int64              `json:"stats"`
	MarketPrices        map[string]int64              `json:"marketPrices"`
	Behaviors           []gameplay.BehaviorView       `json:"behaviors"`
	RecentEvents        []gameplay.RecentEventView    `json:"recentEvents"`
	AscensionCount      int64                         `json:"ascensionCount"`
	WealthBonusPct      float64                       `json:"wealthBonusPct"`
	Ascension           gameplay.AscensionEligibility `json:"ascension"`
}

type versionData struct {
	API      string `json:"api"`
	Backend  string `json:"backend"`
	Frontend string `json:"frontend"`
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
	}
	catalogHandler := makeBehaviorCatalogHandler(database, cfg)
	if cfg.MMOAuthEnabled {
		group.GET("/behaviors/catalog", catalogHandler, serverAuth.RequireAuth(database, cfg))
	} else {
		group.GET("/behaviors/catalog", catalogHandler)
	}
	group.GET("/market/status", makeMarketStatusHandler(database))
	group.GET("/market/history", makeMarketHistoryHandler(database))
	ascendHandler := makeAscendHandler(database, cfg)
	if cfg.MMOAuthEnabled {
		group.POST("/ascend", ascendHandler, serverAuth.RequireAuth(database, cfg))
	} else {
		group.POST("/ascend", ascendHandler)
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
		if err := c.Bind(&req); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid import payload")
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
		if err := c.Bind(&req); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid new game payload")
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
		simulationTick, err := gameplay.CurrentWorldTick(c.Request().Context(), database)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load world tick")
		}

		save := saveGame{SimulationTick: simulationTick, Players: []string{}}
		encodedSave := ""
		if !cfg.MMOAuthEnabled {
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
		if err := c.Bind(&req); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid behavior payload")
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

		if err := gameplay.QueuePlayerBehavior(
			c.Request().Context(),
			database,
			resolvedPlayer.ID,
			key,
			currentTick,
			gameplay.QueueBehaviorOptions{MarketWaitDurationMinutes: marketWaitMinutes, RealmID: resolvedRealmID},
		); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}

		response := map[string]any{"behaviorKey": key, "player": resolvedName}
		if marketWaitMinutes > 0 {
			response["marketWaitMinutes"] = marketWaitMinutes
		}

		return respondSuccess(c, http.StatusOK, "The task is set in motion.", response)
	}
}

func loadActorCharacter(ctx context.Context, database *gorm.DB, accountID uint, characterID uint) (*dal.Character, error) {
	character := &dal.Character{}
	query := database.WithContext(ctx).
		Where("account_id = ? AND status = ?", accountID, "active")

	if characterID != 0 {
		query = query.Where("id = ?", characterID)
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

func makeBehaviorCatalogHandler(database *gorm.DB, cfg config.Config) echo.HandlerFunc {
	return func(c echo.Context) error {
		inventory := map[string]int64{}
		stats := map[string]int64{}
		unlockSet := map[string]struct{}{}
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
		}

		definitions := gameplay.SortBehaviorDefinitions(gameplay.ListBehaviorDefinitions())
		catalog := make([]map[string]any, 0, len(definitions))
		for _, definition := range definitions {
			if definition.ActorType != gameplay.ActorPlayer {
				continue
			}

			available, queueVisible, unavailableReason := evaluateBehaviorAvailability(definition, inventory, stats, unlockSet, hasPrimaryPlayer)

			catalog = append(catalog, map[string]any{
				"key":               definition.Key,
				"actorType":         definition.ActorType,
				"durationMinutes":   definition.DurationMinutes,
				"staminaCost":       definition.StaminaCost,
				"available":         available,
				"queueVisible":      queueVisible,
				"unavailableReason": unavailableReason,
				"requirements": map[string]any{
					"unlocks": definition.Requirements.Unlocks,
					"items":   definition.Requirements.Items,
				},
				"costs":                    definition.Costs,
				"statDeltas":               definition.StatDeltas,
				"outputs":                  definition.Outputs,
				"outputExpressions":        definition.OutputExpressions,
				"outputChances":            definition.OutputChances,
				"grantsUnlocks":            definition.GrantsUnlocks,
				"requiresMarketOpen":       definition.RequiresMarketOpen,
				"marketWaitDefaultMinutes": gameplay.DefaultMarketWaitDurationMinutes(),
				"marketWaitMaxMinutes":     gameplay.MaxMarketWaitDurationMinutes(),
				"marketEffects":            definition.MarketEffects,
			})
		}

		return respondSuccess(c, http.StatusOK, "Known activities are listed.", map[string]any{"behaviors": catalog})
	}
}

func evaluateBehaviorAvailability(
	definition gameplay.BehaviorDefinition,
	inventory map[string]int64,
	stats map[string]int64,
	unlockSet map[string]struct{},
	hasPrimaryPlayer bool,
) (bool, bool, string) {
	if !hasPrimaryPlayer {
		return false, false, "Create a new game to unlock behaviors."
	}

	for _, unlock := range definition.Requirements.Unlocks {
		if _, ok := unlockSet[unlock]; !ok {
			return false, false, "Requires unlock: " + unlock
		}
	}

	for itemKey, requiredQuantity := range definition.Requirements.Items {
		if requiredQuantity <= 0 {
			continue
		}

		if inventory[itemKey] < requiredQuantity {
			return false, true, "Requires " + strconv.FormatInt(requiredQuantity, 10) + " " + itemKey
		}
	}

	if definition.StaminaCost > 0 && stats["stamina"] < definition.StaminaCost {
		return false, true, "Requires " + strconv.FormatInt(definition.StaminaCost, 10) + " stamina"
	}

	return true, true, ""
}

func makeMarketStatusHandler(database *gorm.DB) echo.HandlerFunc {
	return func(c echo.Context) error {
		realmID := uint(1)
		if rawRealmID := strings.TrimSpace(c.QueryParam("realmId")); rawRealmID != "" {
			parsedID, parseErr := strconv.ParseUint(rawRealmID, 10, 64)
			if parseErr != nil || parsedID == 0 {
				return echo.NewHTTPError(http.StatusBadRequest, "realmId must be a positive integer")
			}
			realmID = uint(parsedID)
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

func makeMarketHistoryHandler(database *gorm.DB) echo.HandlerFunc {
	return func(c echo.Context) error {
		query := marketHistoryQuery{Limit: 100}
		if err := c.Bind(&query); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid market history query")
		}
		if query.Realm == 0 {
			query.Realm = 1
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
			if err := c.Bind(&req); err != nil {
				return echo.NewHTTPError(http.StatusBadRequest, "invalid ascension payload")
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
				return echo.NewHTTPError(http.StatusBadRequest, eligibility.Reason)
			}

			count, bonus, err := gameplay.AscendForPlayerRealm(c.Request().Context(), database, player.ID, character.RealmID, name)
			if err != nil {
				if errors.Is(err, gameplay.ErrAscensionNotEligible) {
					return echo.NewHTTPError(http.StatusBadRequest, err.Error())
				}
				return echo.NewHTTPError(http.StatusInternalServerError, "failed to ascend")
			}

			return respondSuccess(c, http.StatusOK, "A new cycle begins for your character, tempered by prior echoes.", map[string]any{"ascensionCount": count, "wealthBonusPct": bonus, "realmId": character.RealmID, "characterId": character.ID})
		}

		var req ascendRequest
		if err := c.Bind(&req); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid ascension payload")
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
			return echo.NewHTTPError(http.StatusBadRequest, eligibility.Reason)
		}

		count, bonus, err := gameplay.Ascend(c.Request().Context(), database, name)
		if err != nil {
			if errors.Is(err, gameplay.ErrAscensionNotEligible) {
				return echo.NewHTTPError(http.StatusBadRequest, err.Error())
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
	player := &dal.Player{}
	result := database.WithContext(ctx).Order("id ASC").Limit(1).Find(player)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	return player, nil
}

func currentVersionData() versionData {
	return versionData{
		API:      version.APIVersion,
		Backend:  version.BackendVersion,
		Frontend: version.FrontendVersion,
	}
}
