package onboarding

import (
	"net/http"
	"strings"

	"github.com/asciifaceman/lived/pkg/config"
	"github.com/asciifaceman/lived/pkg/dal"
	serverAuth "github.com/asciifaceman/lived/src/server/auth"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

const statusSuccess = "success"

type startRequest struct {
	Name    string `json:"name"`
	RealmID uint   `json:"realmId"`
}

type onboardingStartData struct {
	Character onboardingCharacter `json:"character"`
	Created   bool                `json:"created"`
}

type onboardingStatusData struct {
	Onboarded    bool                  `json:"onboarded"`
	Characters   []onboardingCharacter `json:"characters"`
	DefaultRealm uint                  `json:"defaultRealm"`
}

type onboardingCharacter struct {
	ID        uint   `json:"id"`
	PlayerID  uint   `json:"playerId"`
	RealmID   uint   `json:"realmId"`
	Name      string `json:"name"`
	IsPrimary bool   `json:"isPrimary"`
	Status    string `json:"status"`
}

type apiResponse struct {
	Status    string `json:"status"`
	Message   string `json:"message"`
	RequestID string `json:"requestId,omitempty"`
	Data      any    `json:"data,omitempty"`
}

func RegisterRoutes(group *echo.Group, database *gorm.DB, cfg config.Config) {
	if !cfg.MMOAuthEnabled {
		return
	}

	authMW := serverAuth.RequireAuth(database, cfg)
	group.POST("/start", makeStartHandler(database), authMW)
	group.GET("/status", makeStatusHandler(database), authMW)
}

func makeStartHandler(database *gorm.DB) echo.HandlerFunc {
	return func(c echo.Context) error {
		actor, ok := serverAuth.ActorFromContext(c.Request().Context())
		if !ok {
			return echo.NewHTTPError(http.StatusUnauthorized, "missing actor context")
		}

		var req startRequest
		if err := c.Bind(&req); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid onboarding payload")
		}

		name := strings.TrimSpace(req.Name)
		if len(name) < 3 || len(name) > 64 {
			return echo.NewHTTPError(http.StatusBadRequest, "name must be between 3 and 64 characters")
		}

		realmID := req.RealmID
		if realmID == 0 {
			realmID = 1
		}

		existing := dal.Character{}
		res := database.WithContext(c.Request().Context()).
			Where("account_id = ? AND realm_id = ?", actor.AccountID, realmID).
			Order("is_primary DESC, id ASC").
			Limit(1).
			Find(&existing)
		if res.Error != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load existing character")
		}
		if res.RowsAffected > 0 {
			return respondSuccess(c, http.StatusOK, "Onboarding already completed for this realm.", onboardingStartData{Character: toOnboardingCharacter(existing), Created: false})
		}

		createdCharacter := dal.Character{}
		err := database.WithContext(c.Request().Context()).Transaction(func(tx *gorm.DB) error {
			var accountCharacterCount int64
			if err := tx.Model(&dal.Character{}).Where("account_id = ?", actor.AccountID).Count(&accountCharacterCount).Error; err != nil {
				return err
			}

			player := dal.Player{Name: name}
			if err := tx.Create(&player).Error; err != nil {
				return err
			}

			newCharacter := dal.Character{
				AccountID: actor.AccountID,
				PlayerID:  player.ID,
				RealmID:   realmID,
				Name:      name,
				IsPrimary: accountCharacterCount == 0,
				Status:    "active",
			}
			if err := tx.Create(&newCharacter).Error; err != nil {
				return err
			}

			createdCharacter = newCharacter
			return nil
		})
		if err != nil {
			if isUniqueConstraint(err) {
				return echo.NewHTTPError(http.StatusConflict, "character name is already taken")
			}
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to create character")
		}

		return respondSuccess(c, http.StatusCreated, "Onboarding completed.", onboardingStartData{Character: toOnboardingCharacter(createdCharacter), Created: true})
	}
}

func makeStatusHandler(database *gorm.DB) echo.HandlerFunc {
	return func(c echo.Context) error {
		actor, ok := serverAuth.ActorFromContext(c.Request().Context())
		if !ok {
			return echo.NewHTTPError(http.StatusUnauthorized, "missing actor context")
		}

		rows := make([]dal.Character, 0)
		if err := database.WithContext(c.Request().Context()).
			Where("account_id = ?", actor.AccountID).
			Order("is_primary DESC, id ASC").
			Find(&rows).Error; err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load account characters")
		}

		characters := make([]onboardingCharacter, 0, len(rows))
		for _, row := range rows {
			characters = append(characters, toOnboardingCharacter(row))
		}

		return respondSuccess(c, http.StatusOK, "Onboarding status loaded.", onboardingStatusData{
			Onboarded:    len(characters) > 0,
			Characters:   characters,
			DefaultRealm: 1,
		})
	}
}

func toOnboardingCharacter(character dal.Character) onboardingCharacter {
	return onboardingCharacter{
		ID:        character.ID,
		PlayerID:  character.PlayerID,
		RealmID:   character.RealmID,
		Name:      character.Name,
		IsPrimary: character.IsPrimary,
		Status:    character.Status,
	}
}

func respondSuccess(c echo.Context, code int, message string, data any) error {
	requestID := c.Response().Header().Get(echo.HeaderXRequestID)
	return c.JSON(code, apiResponse{Status: statusSuccess, Message: message, RequestID: requestID, Data: data})
}

func isUniqueConstraint(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "duplicate key") || strings.Contains(message, "unique constraint")
}
