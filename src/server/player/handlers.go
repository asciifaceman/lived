package player

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/asciifaceman/lived/pkg/dal"
	"github.com/asciifaceman/lived/pkg/version"
	"github.com/asciifaceman/lived/src/gameplay"
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

func RegisterRoutes(group *echo.Group, database *gorm.DB) {
	group.GET("/status", makeStatusHandler(database))
	group.GET("/inventory", makeInventoryHandler(database))
	group.GET("/behaviors", makeBehaviorsHandler(database))
}

func makeStatusHandler(database *gorm.DB) echo.HandlerFunc {
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

		primaryPlayer, err := loadPrimaryPlayer(c.Request().Context(), database)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load primary player")
		}

		if primaryPlayer == nil {
			return respondSuccess(c, http.StatusOK, "No active player yet. Create a new game to begin.", data)
		}

		snapshot, err := loadPrimaryPlayerSnapshot(c.Request().Context(), database, primaryPlayer)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load player snapshot")
		}

		data.HasPrimaryPlayer = true
		data.PlayerName = primaryPlayer.Name
		data.Inventory = snapshot.Inventory
		data.Stats = snapshot.Stats
		data.Behaviors = filterPlayerBehaviors(snapshot.Behaviors, primaryPlayer.ID)
		data.AscensionCount = snapshot.AscensionCount
		data.WealthBonusPct = snapshot.WealthBonusPct
		data.Ascension = snapshot.Ascension

		return respondSuccess(c, http.StatusOK, "Player save status loaded.", data)
	}
}

func currentVersionData() versionData {
	return versionData{
		API:      version.APIVersion,
		Backend:  version.BackendVersion,
		Frontend: version.FrontendVersion,
	}
}

func makeInventoryHandler(database *gorm.DB) echo.HandlerFunc {
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

		primaryPlayer, err := loadPrimaryPlayer(c.Request().Context(), database)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load primary player")
		}

		if primaryPlayer == nil {
			return respondSuccess(c, http.StatusOK, "No active player yet. Create a new game to begin.", data)
		}

		snapshot, err := loadPrimaryPlayerSnapshot(c.Request().Context(), database, primaryPlayer)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load player snapshot")
		}

		data.HasPrimaryPlayer = true
		data.PlayerName = primaryPlayer.Name
		data.Inventory = snapshot.Inventory

		return respondSuccess(c, http.StatusOK, "Player inventory loaded.", data)
	}
}

func makeBehaviorsHandler(database *gorm.DB) echo.HandlerFunc {
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

		primaryPlayer, err := loadPrimaryPlayer(c.Request().Context(), database)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load primary player")
		}

		if primaryPlayer == nil {
			return respondSuccess(c, http.StatusOK, "No active player yet. Create a new game to begin.", data)
		}

		snapshot, err := loadPrimaryPlayerSnapshot(c.Request().Context(), database, primaryPlayer)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load player snapshot")
		}

		data.HasPrimaryPlayer = true
		data.PlayerName = primaryPlayer.Name
		data.Behaviors = filterPlayerBehaviors(snapshot.Behaviors, primaryPlayer.ID)

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
