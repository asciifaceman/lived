package player

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/asciifaceman/lived/pkg/config"
	"github.com/asciifaceman/lived/pkg/dal"
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

type playerStatusData struct {
	Version          versionData                   `json:"version"`
	Save             string                        `json:"save"`
	Players          []string                      `json:"players"`
	SimulationTick   int64                         `json:"simulationTick"`
	WorldAgeMinutes  int64                         `json:"worldAgeMinutes"`
	WorldAgeHours    int64                         `json:"worldAgeHours"`
	WorldAgeDays     int64                         `json:"worldAgeDays"`
	HasPrimaryPlayer bool                          `json:"hasPrimaryPlayer"`
	PlayerName       string                        `json:"playerName,omitempty"`
	Inventory        map[string]int64              `json:"inventory"`
	Stats            map[string]int64              `json:"stats"`
	Behaviors        []gameplay.BehaviorView       `json:"behaviors"`
	AscensionCount   int64                         `json:"ascensionCount"`
	WealthBonusPct   float64                       `json:"wealthBonusPct"`
	Ascension        gameplay.AscensionEligibility `json:"ascension"`
}

type versionData struct {
	API      string `json:"api"`
	Backend  string `json:"backend"`
	Frontend string `json:"frontend"`
}

type playerInventoryData struct {
	HasPrimaryPlayer bool             `json:"hasPrimaryPlayer"`
	PlayerName       string           `json:"playerName,omitempty"`
	SimulationTick   int64            `json:"simulationTick"`
	Inventory        map[string]int64 `json:"inventory"`
}

type playerBehaviorsData struct {
	HasPrimaryPlayer bool                    `json:"hasPrimaryPlayer"`
	PlayerName       string                  `json:"playerName,omitempty"`
	SimulationTick   int64                   `json:"simulationTick"`
	Behaviors        []gameplay.BehaviorView `json:"behaviors"`
}

type apiResponse struct {
	Status    string `json:"status"`
	Message   string `json:"message"`
	RequestID string `json:"requestId,omitempty"`
	Data      any    `json:"data,omitempty"`
}

func RegisterRoutes(group *echo.Group, database *gorm.DB, cfg config.Config) {
	authMW := serverAuth.RequireAuth(database, cfg)
	inventoryHandler := makeInventoryHandler(database, cfg)
	behaviorsHandler := makeBehaviorsHandler(database, cfg)
	statusHandler := makeStatusHandler(database, cfg)
	if cfg.MMOAuthEnabled {
		group.GET("/status", statusHandler, authMW)
		group.GET("/inventory", inventoryHandler, authMW)
		group.GET("/behaviors", behaviorsHandler, authMW)
	} else {
		group.GET("/status", statusHandler)
		group.GET("/inventory", inventoryHandler)
		group.GET("/behaviors", behaviorsHandler)
	}
}

func makeStatusHandler(database *gorm.DB, cfg config.Config) echo.HandlerFunc {
	return func(c echo.Context) error {
		save, err := loadCurrentSave(c.Request().Context(), database)
		if err != nil {
			return err
		}

		encodedSave, err := encodeSave(save)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to encode save")
		}

		data := playerStatusData{
			Version:          currentVersionData(),
			Save:             encodedSave,
			Players:          save.Players,
			SimulationTick:   save.SimulationTick,
			WorldAgeMinutes:  save.SimulationTick,
			WorldAgeHours:    save.SimulationTick / 60,
			WorldAgeDays:     save.SimulationTick / (60 * 24),
			HasPrimaryPlayer: false,
			Inventory:        map[string]int64{},
			Stats:            map[string]int64{},
			Behaviors:        []gameplay.BehaviorView{},
			Ascension:        gameplay.AscensionEligibility{},
		}

		resolvedPlayer := (*dal.Player)(nil)
		resolvedName := ""

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
				data.Save = ""
				data.Players = []string{}
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
			resolvedName = character.Name
			data.Save = ""
			data.Players = []string{character.Name}
		} else {
			primaryPlayer, err := loadPrimaryPlayer(c.Request().Context(), database)
			if err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "failed to load primary player")
			}

			if primaryPlayer == nil {
				return respondSuccess(c, http.StatusOK, "No active player yet. Create a new game to begin.", data)
			}

			resolvedPlayer = primaryPlayer
			resolvedName = primaryPlayer.Name
		}

		snapshot, err := loadPrimaryPlayerSnapshot(c.Request().Context(), database, resolvedPlayer)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load player snapshot")
		}

		data.HasPrimaryPlayer = true
		data.PlayerName = resolvedName
		data.Inventory = snapshot.Inventory
		data.Stats = snapshot.Stats
		data.Behaviors = filterPlayerBehaviors(snapshot.Behaviors, resolvedPlayer.ID)
		data.AscensionCount = snapshot.AscensionCount
		data.WealthBonusPct = snapshot.WealthBonusPct
		data.Ascension = snapshot.Ascension

		return respondSuccess(c, http.StatusOK, "Player save status loaded.", data)
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

func currentVersionData() versionData {
	return versionData{
		API:      version.APIVersion,
		Backend:  version.BackendVersion,
		Frontend: version.FrontendVersion,
	}
}

func makeInventoryHandler(database *gorm.DB, cfg config.Config) echo.HandlerFunc {
	return func(c echo.Context) error {
		simulationTick, err := gameplay.CurrentWorldTick(c.Request().Context(), database)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load world tick")
		}

		data := playerInventoryData{
			HasPrimaryPlayer: false,
			SimulationTick:   simulationTick,
			Inventory:        map[string]int64{},
		}

		resolvedPlayer := (*dal.Player)(nil)
		resolvedName := ""

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
			resolvedName = character.Name
		} else {
			primaryPlayer, err := loadPrimaryPlayer(c.Request().Context(), database)
			if err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "failed to load primary player")
			}

			if primaryPlayer == nil {
				return respondSuccess(c, http.StatusOK, "No active player yet. Create a new game to begin.", data)
			}

			resolvedPlayer = primaryPlayer
			resolvedName = primaryPlayer.Name
		}

		snapshot, err := loadPrimaryPlayerSnapshot(c.Request().Context(), database, resolvedPlayer)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load player snapshot")
		}

		data.HasPrimaryPlayer = true
		data.PlayerName = resolvedName
		data.Inventory = snapshot.Inventory

		return respondSuccess(c, http.StatusOK, "Player inventory loaded.", data)
	}
}

func makeBehaviorsHandler(database *gorm.DB, cfg config.Config) echo.HandlerFunc {
	return func(c echo.Context) error {
		simulationTick, err := gameplay.CurrentWorldTick(c.Request().Context(), database)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load world tick")
		}

		data := playerBehaviorsData{
			HasPrimaryPlayer: false,
			SimulationTick:   simulationTick,
			Behaviors:        []gameplay.BehaviorView{},
		}

		resolvedPlayer := (*dal.Player)(nil)
		resolvedName := ""

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
			resolvedName = character.Name
		} else {
			primaryPlayer, err := loadPrimaryPlayer(c.Request().Context(), database)
			if err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "failed to load primary player")
			}

			if primaryPlayer == nil {
				return respondSuccess(c, http.StatusOK, "No active player yet. Create a new game to begin.", data)
			}

			resolvedPlayer = primaryPlayer
			resolvedName = primaryPlayer.Name
		}

		snapshot, err := loadPrimaryPlayerSnapshot(c.Request().Context(), database, resolvedPlayer)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load player snapshot")
		}

		data.HasPrimaryPlayer = true
		data.PlayerName = resolvedName
		data.Behaviors = filterPlayerBehaviors(snapshot.Behaviors, resolvedPlayer.ID)

		return respondSuccess(c, http.StatusOK, "Player behaviors loaded.", data)
	}
}

func loadPrimaryPlayerSnapshot(ctx context.Context, database *gorm.DB, primaryPlayer *dal.Player) (gameplay.WorldSnapshot, error) {
	return gameplay.LoadWorldSnapshot(ctx, database, primaryPlayer.ID)
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
