package onboarding

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/asciifaceman/lived/pkg/config"
	"github.com/asciifaceman/lived/pkg/dal"
	"github.com/asciifaceman/lived/pkg/ratelimit"
	serverAuth "github.com/asciifaceman/lived/src/server/auth"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

const statusSuccess = "success"
const realmPauseStateKey = "realm_pause_state"
const roleAdmin = "admin"

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
	Realms       []onboardingRealm     `json:"realms"`
	DefaultRealm uint                  `json:"defaultRealm"`
}

type onboardingRealm struct {
	RealmID        uint   `json:"realmId"`
	Name           string `json:"name"`
	WhitelistOnly  bool   `json:"whitelistOnly"`
	CanCreate      bool   `json:"canCreateCharacter"`
	Decommissioned bool   `json:"decommissioned"`
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
	if cfg.RateLimitEnabled {
		identifier := ratelimit.ClientIPIdentifier
		if cfg.RateLimitIdentity == "account_or_ip" {
			identifier = ratelimit.AccountOrIPIdentifier(func(ctx context.Context) (uint, bool) {
				actor, ok := serverAuth.ActorFromContext(ctx)
				if !ok || actor.AccountID == 0 {
					return 0, false
				}
				return actor.AccountID, true
			})
		}
		limiter := ratelimit.NewFixedWindowLimiter(cfg.RateLimitWindow, identifier)
		group.POST("/start", makeStartHandler(database), authMW, limiter.Middleware("onboarding_start", cfg.RateLimitOnboardMax))
	} else {
		group.POST("/start", makeStartHandler(database), authMW)
	}
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

		paused, err := isRealmPaused(c.Request().Context(), database, realmID)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to resolve realm maintenance state")
		}
		if paused {
			return echo.NewHTTPError(http.StatusLocked, "realm is under maintenance")
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

		canCreate, whitelistOnly, err := canCreateCharacterInRealm(c.Request().Context(), database, actor.AccountID, actor.Roles, realmID)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to resolve realm access policy")
		}
		if !canCreate {
			if whitelistOnly {
				return echo.NewHTTPError(http.StatusForbidden, "realm is whitelisted; request access from an admin")
			}
			return echo.NewHTTPError(http.StatusForbidden, "cannot create a character in this realm")
		}

		createdCharacter := dal.Character{}
		err = database.WithContext(c.Request().Context()).Transaction(func(tx *gorm.DB) error {
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

		realms, defaultRealm, err := loadOnboardingRealms(c.Request().Context(), database, actor.AccountID, actor.Roles)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load realm onboarding metadata")
		}

		return respondSuccess(c, http.StatusOK, "Onboarding status loaded.", onboardingStatusData{
			Onboarded:    len(characters) > 0,
			Characters:   characters,
			Realms:       realms,
			DefaultRealm: defaultRealm,
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

func isRealmPaused(ctx context.Context, database *gorm.DB, realmID uint) (bool, error) {
	state := dal.WorldRuntimeState{}
	result := database.WithContext(ctx).
		Where("realm_id = ? AND key = ?", realmID, realmPauseStateKey).
		Limit(1).
		Find(&state)
	if result.Error != nil {
		return false, result.Error
	}
	if result.RowsAffected == 0 {
		return false, nil
	}

	return state.CarryGameMinutes >= 1, nil
}

func loadOnboardingRealms(ctx context.Context, database *gorm.DB, accountID uint, roles []string) ([]onboardingRealm, uint, error) {
	realmSet := map[uint]struct{}{}

	characterRows := make([]struct{ RealmID uint }, 0)
	if err := database.WithContext(ctx).
		Model(&dal.Character{}).
		Distinct("realm_id").
		Where("realm_id > 0").
		Find(&characterRows).Error; err != nil {
		return nil, 1, err
	}
	for _, row := range characterRows {
		realmSet[row.RealmID] = struct{}{}
	}

	worldRows := make([]struct{ RealmID uint }, 0)
	if err := database.WithContext(ctx).
		Model(&dal.WorldState{}).
		Distinct("realm_id").
		Where("realm_id > 0").
		Find(&worldRows).Error; err != nil {
		return nil, 1, err
	}
	for _, row := range worldRows {
		realmSet[row.RealmID] = struct{}{}
	}

	runtimeRows := make([]struct{ RealmID uint }, 0)
	if err := database.WithContext(ctx).
		Model(&dal.WorldRuntimeState{}).
		Distinct("realm_id").
		Where("realm_id > 0").
		Find(&runtimeRows).Error; err != nil {
		return nil, 1, err
	}
	for _, row := range runtimeRows {
		realmSet[row.RealmID] = struct{}{}
	}

	configRows := make([]dal.RealmConfig, 0)
	if err := database.WithContext(ctx).Where("realm_id > 0").Find(&configRows).Error; err != nil {
		return nil, 1, err
	}
	configsByRealmID := make(map[uint]dal.RealmConfig, len(configRows))
	for _, row := range configRows {
		configsByRealmID[row.RealmID] = row
		realmSet[row.RealmID] = struct{}{}
	}

	if len(realmSet) == 0 {
		realmSet[1] = struct{}{}
	}

	realmIDs := make([]uint, 0, len(realmSet))
	for realmID := range realmSet {
		realmIDs = append(realmIDs, realmID)
	}
	sort.Slice(realmIDs, func(i, j int) bool { return realmIDs[i] < realmIDs[j] })

	realms := make([]onboardingRealm, 0, len(realmIDs))
	defaultRealm := uint(1)
	defaultSet := false
	for _, realmID := range realmIDs {
		config := configsByRealmID[realmID]
		name := strings.TrimSpace(config.DisplayName)
		if name == "" {
			name = defaultRealmName(realmID)
		}
		canCreate, _, err := canCreateCharacterInRealm(ctx, database, accountID, roles, realmID)
		if err != nil {
			return nil, 1, err
		}

		realm := onboardingRealm{
			RealmID:        realmID,
			Name:           name,
			WhitelistOnly:  config.WhitelistOnly,
			CanCreate:      canCreate,
			Decommissioned: config.Decommissioned,
		}
		realms = append(realms, realm)

		if !defaultSet && canCreate && !config.Decommissioned {
			defaultRealm = realmID
			defaultSet = true
		}
	}

	return realms, defaultRealm, nil
}

func canCreateCharacterInRealm(ctx context.Context, database *gorm.DB, accountID uint, roles []string, realmID uint) (bool, bool, error) {
	if realmID == 0 {
		realmID = 1
	}
	if hasRole(roles, roleAdmin) {
		return true, false, nil
	}

	config := dal.RealmConfig{}
	result := database.WithContext(ctx).Where("realm_id = ?", realmID).Limit(1).Find(&config)
	if result.Error != nil {
		return false, false, result.Error
	}
	if result.RowsAffected == 0 || !config.WhitelistOnly {
		return true, false, nil
	}

	var count int64
	if err := database.WithContext(ctx).
		Model(&dal.RealmAccessGrant{}).
		Where("realm_id = ? AND account_id = ? AND is_active = ?", realmID, accountID, true).
		Count(&count).Error; err != nil {
		return false, true, err
	}

	return count > 0, true, nil
}

func hasRole(roles []string, target string) bool {
	for _, role := range roles {
		if role == target {
			return true
		}
	}
	return false
}

func defaultRealmName(realmID uint) string {
	if realmID == 0 {
		realmID = 1
	}
	return "Realm " + strconv.FormatUint(uint64(realmID), 10)
}
