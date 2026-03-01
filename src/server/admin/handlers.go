package admin

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/asciifaceman/lived/pkg/config"
	"github.com/asciifaceman/lived/pkg/dal"
	"github.com/asciifaceman/lived/src/gameplay"
	serverAuth "github.com/asciifaceman/lived/src/server/auth"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

const (
	statusSuccess           = "success"
	roleAdmin               = "admin"
	statusActive            = "active"
	statusLocked            = "locked"
	realmControlStateKey    = "realm_pause_state"
	defaultAuditLimit       = 100
	maxAuditLimit           = 500
	defaultStatsWindowTicks = int64(24 * 60)
	maxStatsWindowTicks     = int64(30 * 24 * 60)

	actionMarketResetDefaults = "market_reset_defaults"
	actionMarketSetPrice      = "market_set_price"
	actionRealmPause          = "realm_pause"
	actionRealmResume         = "realm_resume"
	actionRealmCreate         = "realm_create"
	actionAccountLock         = "account_lock"
	actionAccountUnlock       = "account_unlock"
	actionAccountStatusSet    = "account_status_set"
	actionRoleGrant           = "account_role_grant"
	actionRoleRevoke          = "account_role_revoke"
	actionCharacterModify     = "character_modify"
	actionRealmConfigSet      = "realm_config_set"
	actionRealmAccessGrant    = "realm_access_grant"
	actionRealmAccessRevoke   = "realm_access_revoke"
)

var (
	errCannotLockOwnAccount   = errors.New("cannot lock your own account")
	errCannotRevokeOwnAdmin   = errors.New("cannot revoke your own admin role")
	errLastActiveAdminBlocked = errors.New("operation would remove the last active admin account")
)

type apiResponse struct {
	Status    string `json:"status"`
	Message   string `json:"message"`
	RequestID string `json:"requestId,omitempty"`
	Data      any    `json:"data,omitempty"`
}

type realmActionRequest struct {
	Action     string `json:"action"`
	ReasonCode string `json:"reasonCode"`
	Note       string `json:"note,omitempty"`
	ItemKey    string `json:"itemKey,omitempty"`
	Price      int64  `json:"price,omitempty"`
}

type validatedRealmAction struct {
	Action     string
	ReasonCode string
	Note       string
	ItemKey    string
	Price      int64
}

type moderationActionRequest struct {
	ReasonCode string `json:"reasonCode"`
	Note       string `json:"note,omitempty"`
}

type moderationRoleRequest struct {
	RoleKey    string `json:"roleKey"`
	Action     string `json:"action"`
	ReasonCode string `json:"reasonCode"`
	Note       string `json:"note,omitempty"`
}

type moderationAccountStatusRequest struct {
	Status         string `json:"status"`
	ReasonCode     string `json:"reasonCode"`
	Note           string `json:"note,omitempty"`
	RevokeSessions *bool  `json:"revokeSessions,omitempty"`
}

type moderationCharacterRequest struct {
	Name       *string `json:"name,omitempty"`
	Status     *string `json:"status,omitempty"`
	IsPrimary  *bool   `json:"isPrimary,omitempty"`
	ReasonCode string  `json:"reasonCode"`
	Note       string  `json:"note,omitempty"`
}

type realmConfigRequest struct {
	Name          string `json:"name"`
	WhitelistOnly *bool  `json:"whitelistOnly,omitempty"`
}

type realmAccessRequest struct {
	AccountID  uint   `json:"accountId"`
	ReasonCode string `json:"reasonCode"`
	Note       string `json:"note,omitempty"`
}

type validatedRoleModeration struct {
	RoleKey    string
	Action     string
	ReasonCode string
	Note       string
}

type validatedAccountStatusModeration struct {
	Status         string
	ReasonCode     string
	Note           string
	RevokeSessions bool
}

type validatedCharacterModeration struct {
	Name       *string
	Status     *string
	IsPrimary  *bool
	ReasonCode string
	Note       string
}

type auditQuery struct {
	RealmID        uint
	ActorAccountID uint
	ActorUsername  string
	ActionKey      string
	BeforeID       uint
	IncludeRawJSON bool
	Limit          int
}

type characterModerationQuery struct {
	AccountID       uint
	AccountUsername string
	RealmID         uint
	Status          string
	NameLike        string
	BeforeID        uint
	Limit           int
}

func RegisterRoutes(group *echo.Group, database *gorm.DB, cfg config.Config) {
	if !cfg.MMOAuthEnabled {
		return
	}

	authMW := serverAuth.RequireAuth(database, cfg)
	group.GET("/realms", makeRealmsHandler(database), authMW, requireAdminRole())
	group.GET("/audit", makeAuditHandler(database), authMW, requireAdminRole())
	group.GET("/audit/export", makeAuditExportHandler(database), authMW, requireAdminRole())
	group.GET("/audit/:id", makeAuditDetailHandler(database), authMW, requireAdminRole())
	group.POST("/realms/:id/actions", makeRealmActionHandler(database), authMW, requireAdminRole())
	group.POST("/realms/:id/config", makeRealmConfigHandler(database), authMW, requireAdminRole())
	group.GET("/realms/:id/access", makeRealmAccessListHandler(database), authMW, requireAdminRole())
	group.POST("/realms/:id/access/grant", makeRealmAccessGrantHandler(database), authMW, requireAdminRole())
	group.POST("/realms/:id/access/revoke", makeRealmAccessRevokeHandler(database), authMW, requireAdminRole())
	group.POST("/moderation/accounts/:id/lock", makeAccountLockHandler(database), authMW, requireAdminRole())
	group.POST("/moderation/accounts/:id/unlock", makeAccountUnlockHandler(database), authMW, requireAdminRole())
	group.POST("/moderation/accounts/:id/status", makeAccountStatusHandler(database), authMW, requireAdminRole())
	group.POST("/moderation/accounts/:id/roles", makeAccountRoleModerationHandler(database), authMW, requireAdminRole())
	group.GET("/moderation/characters", makeCharacterModerationListHandler(database), authMW, requireAdminRole())
	group.POST("/moderation/characters/:id", makeCharacterModerationHandler(database), authMW, requireAdminRole())
	group.GET("/chat/channels", makeChatChannelListHandler(database), authMW, requireAdminRole())
	group.POST("/chat/channels", makeChatChannelCreateHandler(database), authMW, requireAdminRole())
	group.DELETE("/chat/channels/:key", makeChatChannelRemoveHandler(database), authMW, requireAdminRole())
	group.POST("/chat/channels/:key/flush", makeChatChannelFlushHandler(database), authMW, requireAdminRole())
	group.POST("/chat/channels/:key/moderation", makeChatChannelModerationHandler(database), authMW, requireAdminRole())
	group.POST("/chat/channels/:key/system-message", makeChatSystemMessageHandler(database), authMW, requireAdminRole())
	group.GET("/chat/wordlist", makeChatWordlistListHandler(database), authMW, requireAdminRole())
	group.POST("/chat/wordlist", makeChatWordlistAddHandler(database), authMW, requireAdminRole())
	group.DELETE("/chat/wordlist/:ruleId", makeChatWordlistRemoveHandler(database), authMW, requireAdminRole())
	group.GET("/stats", makeStatsHandler(database), authMW, requireAdminRole())
}

func makeAuditHandler(database *gorm.DB) echo.HandlerFunc {
	return func(c echo.Context) error {
		filters, err := parseAuditFilters(c)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}

		query := database.WithContext(c.Request().Context()).Model(&dal.AdminAuditEvent{})
		if filters.RealmID != 0 {
			query = query.Where("realm_id = ?", filters.RealmID)
		}
		if filters.ActorAccountID != 0 {
			query = query.Where("actor_account_id = ?", filters.ActorAccountID)
		}
		if filters.ActorUsername != "" {
			query = query.Joins("JOIN accounts actor_accounts ON actor_accounts.id = admin_audit_events.actor_account_id").Where("LOWER(actor_accounts.username) LIKE ?", "%"+strings.ToLower(filters.ActorUsername)+"%")
		}
		if filters.ActionKey != "" {
			query = query.Where("action_key = ?", filters.ActionKey)
		}
		if filters.BeforeID != 0 {
			query = query.Where("id < ?", filters.BeforeID)
		}

		rows := make([]dal.AdminAuditEvent, 0)
		if err := query.Order("id DESC").Limit(filters.Limit + 1).Find(&rows).Error; err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load admin audit entries")
		}

		hasMore := len(rows) > filters.Limit
		nextBeforeID := uint(0)
		if hasMore {
			rows = rows[:filters.Limit]
			nextBeforeID = rows[len(rows)-1].ID
		}

		events := make([]map[string]any, 0, len(rows))
		for _, row := range rows {
			entry := map[string]any{
				"id":             row.ID,
				"realmId":        row.RealmID,
				"actorAccountId": row.ActorAccountID,
				"actionKey":      row.ActionKey,
				"reasonCode":     row.ReasonCode,
				"note":           row.Note,
				"occurredTick":   row.OccurredTick,
				"createdAt":      row.CreatedAt,
			}
			if filters.IncludeRawJSON {
				entry["before"] = decodeMaybeJSON(row.BeforeJSON)
				entry["after"] = decodeMaybeJSON(row.AfterJSON)
			}
			events = append(events, entry)
		}

		return respondSuccess(c, http.StatusOK, "Admin audit entries loaded.", map[string]any{
			"filters": map[string]any{
				"realmId":        filters.RealmID,
				"actorAccountId": filters.ActorAccountID,
				"actorUsername":  filters.ActorUsername,
				"actionKey":      filters.ActionKey,
				"beforeId":       filters.BeforeID,
				"includeRawJson": filters.IncludeRawJSON,
				"limit":          filters.Limit,
			},
			"pagination": map[string]any{
				"hasMore":      hasMore,
				"nextBeforeId": nextBeforeID,
			},
			"entries": events,
		})
	}
}

func makeAuditDetailHandler(database *gorm.DB) echo.HandlerFunc {
	return func(c echo.Context) error {
		includeRawJSON, err := parseOptionalBoolQuery(c.QueryParam("includeRawJson"), "includeRawJson")
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}

		auditID, err := parseAuditIDPathParam(c.Param("id"))
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}

		entry := dal.AdminAuditEvent{}
		result := database.WithContext(c.Request().Context()).Where("id = ?", auditID).Limit(1).Find(&entry)
		if result.Error != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load admin audit entry")
		}
		if result.RowsAffected == 0 {
			return echo.NewHTTPError(http.StatusNotFound, "admin audit entry not found")
		}

		payload := map[string]any{
			"id":             entry.ID,
			"realmId":        entry.RealmID,
			"actorAccountId": entry.ActorAccountID,
			"actionKey":      entry.ActionKey,
			"reasonCode":     entry.ReasonCode,
			"note":           entry.Note,
			"occurredTick":   entry.OccurredTick,
			"createdAt":      entry.CreatedAt,
		}
		if includeRawJSON {
			payload["before"] = decodeMaybeJSON(entry.BeforeJSON)
			payload["after"] = decodeMaybeJSON(entry.AfterJSON)
		}

		return respondSuccess(c, http.StatusOK, "Admin audit entry loaded.", payload)
	}
}

func makeAuditExportHandler(database *gorm.DB) echo.HandlerFunc {
	return func(c echo.Context) error {
		filters, err := parseAuditFilters(c)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}

		query := database.WithContext(c.Request().Context()).Model(&dal.AdminAuditEvent{})
		if filters.RealmID != 0 {
			query = query.Where("realm_id = ?", filters.RealmID)
		}
		if filters.ActorAccountID != 0 {
			query = query.Where("actor_account_id = ?", filters.ActorAccountID)
		}
		if filters.ActorUsername != "" {
			query = query.Joins("JOIN accounts actor_accounts ON actor_accounts.id = admin_audit_events.actor_account_id").Where("LOWER(actor_accounts.username) LIKE ?", "%"+strings.ToLower(filters.ActorUsername)+"%")
		}
		if filters.ActionKey != "" {
			query = query.Where("action_key = ?", filters.ActionKey)
		}
		if filters.BeforeID != 0 {
			query = query.Where("id < ?", filters.BeforeID)
		}

		rows := make([]dal.AdminAuditEvent, 0)
		if err := query.Order("id DESC").Limit(filters.Limit).Find(&rows).Error; err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load admin audit export rows")
		}

		encoded, err := buildAuditCSV(rows)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to encode audit csv")
		}

		fileName := fmt.Sprintf("admin-audit-export-%s.csv", time.Now().UTC().Format("20060102-150405"))
		c.Response().Header().Set(echo.HeaderContentType, "text/csv; charset=utf-8")
		c.Response().Header().Set(echo.HeaderContentDisposition, fmt.Sprintf("attachment; filename=\"%s\"", fileName))

		return c.Blob(http.StatusOK, "text/csv; charset=utf-8", encoded)
	}
}

func makeRealmsHandler(database *gorm.DB) echo.HandlerFunc {
	return func(c echo.Context) error {
		realmSet := map[uint]struct{}{}

		characterRows := make([]struct{ RealmID uint }, 0)
		if err := database.WithContext(c.Request().Context()).
			Model(&dal.Character{}).
			Distinct("realm_id").
			Where("realm_id > 0").
			Find(&characterRows).Error; err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load character realms")
		}
		for _, row := range characterRows {
			realmSet[row.RealmID] = struct{}{}
		}

		worldRows := make([]struct{ RealmID uint }, 0)
		if err := database.WithContext(c.Request().Context()).
			Model(&dal.WorldState{}).
			Distinct("realm_id").
			Where("realm_id > 0").
			Find(&worldRows).Error; err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load world realms")
		}
		for _, row := range worldRows {
			realmSet[row.RealmID] = struct{}{}
		}

		runtimeRows := make([]struct{ RealmID uint }, 0)
		if err := database.WithContext(c.Request().Context()).
			Model(&dal.WorldRuntimeState{}).
			Distinct("realm_id").
			Where("realm_id > 0").
			Find(&runtimeRows).Error; err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load runtime realms")
		}
		for _, row := range runtimeRows {
			realmSet[row.RealmID] = struct{}{}
		}

		realmIDs := make([]uint, 0, len(realmSet))
		for realmID := range realmSet {
			realmIDs = append(realmIDs, realmID)
		}

		configRows := make([]dal.RealmConfig, 0)
		if err := database.WithContext(c.Request().Context()).
			Where("realm_id > 0").
			Find(&configRows).Error; err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load realm metadata")
		}

		configsByRealmID := make(map[uint]dal.RealmConfig, len(configRows))
		for _, row := range configRows {
			configsByRealmID[row.RealmID] = row
			if _, ok := realmSet[row.RealmID]; !ok {
				realmSet[row.RealmID] = struct{}{}
				realmIDs = append(realmIDs, row.RealmID)
			}
		}

		sort.Slice(realmIDs, func(i, j int) bool { return realmIDs[i] < realmIDs[j] })

		realms := make([]map[string]any, 0, len(realmIDs))
		for _, realmID := range realmIDs {
			var activeCharacters int64
			if err := database.WithContext(c.Request().Context()).
				Model(&dal.Character{}).
				Where("realm_id = ? AND status = ?", realmID, "active").
				Count(&activeCharacters).Error; err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "failed to load realm character counts")
			}

			config := configsByRealmID[realmID]
			displayName := strings.TrimSpace(config.DisplayName)
			if displayName == "" {
				displayName = defaultRealmDisplayName(realmID)
			}

			realms = append(realms, map[string]any{
				"realmId":          realmID,
				"name":             displayName,
				"whitelistOnly":    config.WhitelistOnly,
				"decommissioned":   config.Decommissioned,
				"activeCharacters": activeCharacters,
			})
		}

		return respondSuccess(c, http.StatusOK, "Admin realm list loaded.", map[string]any{"realms": realms})
	}
}

func makeStatsHandler(database *gorm.DB) echo.HandlerFunc {
	return func(c echo.Context) error {
		windowTicks, err := parseWindowTicks(c.QueryParam("windowTicks"))
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}

		var activeAccounts int64
		if err := database.WithContext(c.Request().Context()).
			Model(&dal.Account{}).
			Where("status = ?", "active").
			Count(&activeAccounts).Error; err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load account stats")
		}

		var activeCharacters int64
		if err := database.WithContext(c.Request().Context()).
			Model(&dal.Character{}).
			Where("status = ?", "active").
			Count(&activeCharacters).Error; err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load character stats")
		}

		var activeSessions int64
		if err := database.WithContext(c.Request().Context()).
			Model(&dal.AccountSession{}).
			Where("revoked_at IS NULL").
			Count(&activeSessions).Error; err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load session stats")
		}

		var queuedBehaviors int64
		if err := database.WithContext(c.Request().Context()).
			Model(&dal.BehaviorInstance{}).
			Where("state IN ?", []string{"queued", "active"}).
			Count(&queuedBehaviors).Error; err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load behavior stats")
		}

		var worldEvents int64
		if err := database.WithContext(c.Request().Context()).
			Model(&dal.WorldEvent{}).
			Where("visibility = ?", "public").
			Count(&worldEvents).Error; err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load event stats")
		}

		currentTick, err := loadCurrentGlobalTick(c.Request().Context(), database)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load current global tick")
		}

		windowStartTick := currentTick - windowTicks
		if windowStartTick < 0 {
			windowStartTick = 0
		}

		var adminAuditTotal int64
		if err := database.WithContext(c.Request().Context()).
			Model(&dal.AdminAuditEvent{}).
			Count(&adminAuditTotal).Error; err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load admin audit totals")
		}

		var adminAuditWindow int64
		if err := database.WithContext(c.Request().Context()).
			Model(&dal.AdminAuditEvent{}).
			Where("occurred_tick >= ?", windowStartTick).
			Count(&adminAuditWindow).Error; err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load admin audit window totals")
		}

		actionRows := make([]struct {
			ActionKey string `gorm:"column:action_key"`
			Count     int64  `gorm:"column:count"`
		}, 0)
		if err := database.WithContext(c.Request().Context()).
			Model(&dal.AdminAuditEvent{}).
			Select("action_key, COUNT(*) AS count").
			Where("occurred_tick >= ?", windowStartTick).
			Group("action_key").
			Order("count DESC, action_key ASC").
			Scan(&actionRows).Error; err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load admin audit action aggregates")
		}

		realmRows := make([]struct {
			RealmID uint  `gorm:"column:realm_id"`
			Count   int64 `gorm:"column:count"`
		}, 0)
		if err := database.WithContext(c.Request().Context()).
			Model(&dal.AdminAuditEvent{}).
			Select("realm_id, COUNT(*) AS count").
			Where("occurred_tick >= ?", windowStartTick).
			Group("realm_id").
			Order("count DESC, realm_id ASC").
			Scan(&realmRows).Error; err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load admin audit realm aggregates")
		}

		auditByAction := make([]map[string]any, 0, len(actionRows))
		for _, row := range actionRows {
			auditByAction = append(auditByAction, map[string]any{
				"actionKey": row.ActionKey,
				"count":     row.Count,
			})
		}

		auditByRealm := make([]map[string]any, 0, len(realmRows))
		for _, row := range realmRows {
			auditByRealm = append(auditByRealm, map[string]any{
				"realmId": row.RealmID,
				"count":   row.Count,
			})
		}

		return respondSuccess(c, http.StatusOK, "Admin stats loaded.", map[string]any{
			"activeAccounts":    activeAccounts,
			"activeCharacters":  activeCharacters,
			"activeSessions":    activeSessions,
			"queuedOrActive":    queuedBehaviors,
			"publicWorldEvents": worldEvents,
			"adminAudit": map[string]any{
				"total":       adminAuditTotal,
				"windowTotal": adminAuditWindow,
				"byAction":    auditByAction,
				"byRealm":     auditByRealm,
				"window": map[string]any{
					"currentTick":     currentTick,
					"windowTicks":     windowTicks,
					"windowStartTick": windowStartTick,
				},
			},
		})
	}
}

func makeRealmActionHandler(database *gorm.DB) echo.HandlerFunc {
	return func(c echo.Context) error {
		realmID, err := parseRealmIDPathParam(c.Param("id"))
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}

		actor, ok := serverAuth.ActorFromContext(c.Request().Context())
		if !ok {
			return echo.NewHTTPError(http.StatusUnauthorized, "missing actor context")
		}

		var req realmActionRequest
		if err := c.Bind(&req); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid admin action payload")
		}

		action, err := validateRealmAction(req)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}

		tick, err := gameplay.CurrentWorldTickForRealm(c.Request().Context(), database, realmID)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load world tick")
		}

		resultData := map[string]any{}
		err = database.WithContext(c.Request().Context()).Transaction(func(tx *gorm.DB) error {
			var beforeState any
			var afterState any

			switch action.Action {
			case actionMarketResetDefaults:
				before, after, applyErr := applyMarketResetDefaults(c.Request().Context(), tx, realmID, tick)
				if applyErr != nil {
					return applyErr
				}
				beforeState = before
				afterState = after
				resultData["updatedSymbols"] = len(after)
			case actionMarketSetPrice:
				before, after, applyErr := applyMarketSetPrice(c.Request().Context(), tx, realmID, tick, action.ItemKey, action.Price)
				if applyErr != nil {
					return applyErr
				}
				beforeState = before
				afterState = after
				resultData["itemKey"] = action.ItemKey
				resultData["price"] = action.Price
			case actionRealmPause:
				before, after, revokedSessions, applyErr := applyRealmPause(c.Request().Context(), tx, realmID, action.ReasonCode, action.Note, tick)
				if applyErr != nil {
					return applyErr
				}
				beforeState = before
				afterState = after
				resultData["revokedSessions"] = revokedSessions
			case actionRealmResume:
				before, after, applyErr := applyRealmResume(c.Request().Context(), tx, realmID, action.ReasonCode, action.Note, tick)
				if applyErr != nil {
					return applyErr
				}
				beforeState = before
				afterState = after
			case actionRealmCreate:
				before, after, applyErr := applyRealmCreate(c.Request().Context(), tx, realmID)
				if applyErr != nil {
					return applyErr
				}
				beforeState = before
				afterState = after
			default:
				return fmt.Errorf("unsupported realm action")
			}

			beforeJSON, encodeErr := encodeJSON(beforeState)
			if encodeErr != nil {
				return encodeErr
			}
			afterJSON, encodeErr := encodeJSON(afterState)
			if encodeErr != nil {
				return encodeErr
			}

			audit := dal.AdminAuditEvent{
				RealmID:        realmID,
				ActorAccountID: actor.AccountID,
				ActionKey:      action.Action,
				ReasonCode:     action.ReasonCode,
				Note:           action.Note,
				BeforeJSON:     beforeJSON,
				AfterJSON:      afterJSON,
				OccurredTick:   tick,
			}
			if err := tx.WithContext(c.Request().Context()).Create(&audit).Error; err != nil {
				return err
			}

			return nil
		})
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to apply admin action")
		}

		resultData["realmId"] = realmID
		resultData["action"] = action.Action
		resultData["reasonCode"] = action.ReasonCode
		resultData["occurredTick"] = tick

		return respondSuccess(c, http.StatusOK, "Admin realm action applied.", resultData)
	}
}

func makeRealmConfigHandler(database *gorm.DB) echo.HandlerFunc {
	return func(c echo.Context) error {
		realmID, err := parseRealmIDPathParam(c.Param("id"))
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}

		actor, ok := serverAuth.ActorFromContext(c.Request().Context())
		if !ok {
			return echo.NewHTTPError(http.StatusUnauthorized, "missing actor context")
		}

		var req realmConfigRequest
		if err := c.Bind(&req); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid realm config payload")
		}

		name := strings.TrimSpace(req.Name)
		if name != "" && (len(name) < 2 || len(name) > 64) {
			return echo.NewHTTPError(http.StatusBadRequest, "name must be between 2 and 64 characters")
		}

		tick, err := adminAuditTick(c.Request().Context(), database, realmID)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load world tick")
		}

		result := map[string]any{}
		err = database.WithContext(c.Request().Context()).Transaction(func(tx *gorm.DB) error {
			config := dal.RealmConfig{}
			query := tx.WithContext(c.Request().Context()).Where("realm_id = ?", realmID).Limit(1).Find(&config)
			if query.Error != nil {
				return query.Error
			}

			beforeName := strings.TrimSpace(config.DisplayName)
			if beforeName == "" {
				beforeName = defaultRealmDisplayName(realmID)
			}
			beforeWhitelist := config.WhitelistOnly

			nextName := name
			if nextName == "" {
				if strings.TrimSpace(config.DisplayName) != "" {
					nextName = strings.TrimSpace(config.DisplayName)
				} else {
					nextName = defaultRealmDisplayName(realmID)
				}
			}

			nextWhitelist := beforeWhitelist
			if req.WhitelistOnly != nil {
				nextWhitelist = *req.WhitelistOnly
			}

			if query.RowsAffected == 0 {
				config = dal.RealmConfig{
					RealmID:       realmID,
					DisplayName:   nextName,
					WhitelistOnly: nextWhitelist,
				}
				if err := tx.WithContext(c.Request().Context()).Create(&config).Error; err != nil {
					return err
				}
			} else {
				if err := tx.WithContext(c.Request().Context()).Model(&dal.RealmConfig{}).Where("id = ?", config.ID).Updates(map[string]any{
					"display_name":   nextName,
					"whitelist_only": nextWhitelist,
				}).Error; err != nil {
					return err
				}
			}

			before := map[string]any{"name": beforeName, "whitelistOnly": beforeWhitelist}
			after := map[string]any{"name": nextName, "whitelistOnly": nextWhitelist}
			if err := appendAdminAudit(tx, actor.AccountID, actionRealmConfigSet, "realm_config", "", before, after, tick, realmID); err != nil {
				return err
			}

			result["realmId"] = realmID
			result["name"] = nextName
			result["whitelistOnly"] = nextWhitelist
			result["occurredTick"] = tick
			return nil
		})
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to update realm config")
		}

		return respondSuccess(c, http.StatusOK, "Realm config updated.", result)
	}
}

func makeRealmAccessListHandler(database *gorm.DB) echo.HandlerFunc {
	return func(c echo.Context) error {
		realmID, err := parseRealmIDPathParam(c.Param("id"))
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}

		accountID, err := parseOptionalUintQuery(c.QueryParam("accountId"), "accountId")
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}

		type row struct {
			ID              uint      `gorm:"column:id"`
			RealmID         uint      `gorm:"column:realm_id"`
			AccountID       uint      `gorm:"column:account_id"`
			AccountUsername string    `gorm:"column:account_username"`
			GrantedByID     uint      `gorm:"column:granted_by_id"`
			ReasonCode      string    `gorm:"column:reason_code"`
			Note            string    `gorm:"column:note"`
			UpdatedAt       time.Time `gorm:"column:updated_at"`
		}

		rows := make([]row, 0)
		query := database.WithContext(c.Request().Context()).
			Table("realm_access_grants rag").
			Select("rag.id, rag.realm_id, rag.account_id, acc.username AS account_username, rag.granted_by_id, rag.reason_code, rag.note, rag.updated_at").
			Joins("JOIN accounts acc ON acc.id = rag.account_id").
			Where("rag.realm_id = ? AND rag.is_active = ?", realmID, true)
		if accountID != 0 {
			query = query.Where("rag.account_id = ?", accountID)
		}
		if err := query.Order("rag.updated_at DESC, rag.id DESC").Find(&rows).Error; err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load realm access grants")
		}

		entries := make([]map[string]any, 0, len(rows))
		for _, row := range rows {
			entries = append(entries, map[string]any{
				"id":              row.ID,
				"realmId":         row.RealmID,
				"accountId":       row.AccountID,
				"accountUsername": row.AccountUsername,
				"grantedById":     row.GrantedByID,
				"reasonCode":      row.ReasonCode,
				"note":            row.Note,
				"updatedAt":       row.UpdatedAt,
			})
		}

		return respondSuccess(c, http.StatusOK, "Realm access grants loaded.", map[string]any{"realmId": realmID, "entries": entries})
	}
}

func makeRealmAccessGrantHandler(database *gorm.DB) echo.HandlerFunc {
	return func(c echo.Context) error {
		realmID, err := parseRealmIDPathParam(c.Param("id"))
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}

		actor, ok := serverAuth.ActorFromContext(c.Request().Context())
		if !ok {
			return echo.NewHTTPError(http.StatusUnauthorized, "missing actor context")
		}

		var req realmAccessRequest
		if err := c.Bind(&req); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid realm access payload")
		}
		if req.AccountID == 0 {
			return echo.NewHTTPError(http.StatusBadRequest, "accountId is required")
		}

		reasonCode, note, err := validateReasonAndNote(req.ReasonCode, req.Note)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}

		tick, err := adminAuditTick(c.Request().Context(), database, realmID)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load world tick")
		}

		result := map[string]any{}
		err = database.WithContext(c.Request().Context()).Transaction(func(tx *gorm.DB) error {
			account := dal.Account{}
			accountResult := tx.WithContext(c.Request().Context()).Where("id = ?", req.AccountID).Limit(1).Find(&account)
			if accountResult.Error != nil {
				return accountResult.Error
			}
			if accountResult.RowsAffected == 0 {
				return gorm.ErrRecordNotFound
			}

			grant := dal.RealmAccessGrant{}
			grantResult := tx.WithContext(c.Request().Context()).Where("realm_id = ? AND account_id = ?", realmID, req.AccountID).Limit(1).Find(&grant)
			if grantResult.Error != nil {
				return grantResult.Error
			}

			before := map[string]any{"active": grant.IsActive, "reasonCode": grant.ReasonCode, "note": grant.Note}
			after := map[string]any{"active": true, "reasonCode": reasonCode, "note": note}

			if grantResult.RowsAffected == 0 {
				grant = dal.RealmAccessGrant{
					RealmID:      realmID,
					AccountID:    req.AccountID,
					GrantedByID:  actor.AccountID,
					LastActionBy: actor.AccountID,
					IsActive:     true,
					ReasonCode:   reasonCode,
					Note:         note,
				}
				if err := tx.WithContext(c.Request().Context()).Create(&grant).Error; err != nil {
					return err
				}
			} else {
				if err := tx.WithContext(c.Request().Context()).Model(&dal.RealmAccessGrant{}).Where("id = ?", grant.ID).Updates(map[string]any{
					"is_active":      true,
					"reason_code":    reasonCode,
					"note":           note,
					"granted_by_id":  actor.AccountID,
					"last_action_by": actor.AccountID,
				}).Error; err != nil {
					return err
				}
			}

			if err := appendAdminAudit(tx, actor.AccountID, actionRealmAccessGrant, reasonCode, note, before, after, tick, realmID); err != nil {
				return err
			}

			result["realmId"] = realmID
			result["accountId"] = req.AccountID
			result["accountUsername"] = account.Username
			result["active"] = true
			result["occurredTick"] = tick
			return nil
		})
		if err != nil {
			if err == gorm.ErrRecordNotFound {
				return echo.NewHTTPError(http.StatusNotFound, "account not found")
			}
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to grant realm access")
		}

		return respondSuccess(c, http.StatusOK, "Realm access granted.", result)
	}
}

func makeRealmAccessRevokeHandler(database *gorm.DB) echo.HandlerFunc {
	return func(c echo.Context) error {
		realmID, err := parseRealmIDPathParam(c.Param("id"))
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}

		actor, ok := serverAuth.ActorFromContext(c.Request().Context())
		if !ok {
			return echo.NewHTTPError(http.StatusUnauthorized, "missing actor context")
		}

		var req realmAccessRequest
		if err := c.Bind(&req); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid realm access payload")
		}
		if req.AccountID == 0 {
			return echo.NewHTTPError(http.StatusBadRequest, "accountId is required")
		}

		reasonCode, note, err := validateReasonAndNote(req.ReasonCode, req.Note)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}

		tick, err := adminAuditTick(c.Request().Context(), database, realmID)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load world tick")
		}

		result := map[string]any{}
		err = database.WithContext(c.Request().Context()).Transaction(func(tx *gorm.DB) error {
			grant := dal.RealmAccessGrant{}
			grantResult := tx.WithContext(c.Request().Context()).Where("realm_id = ? AND account_id = ?", realmID, req.AccountID).Limit(1).Find(&grant)
			if grantResult.Error != nil {
				return grantResult.Error
			}
			if grantResult.RowsAffected == 0 {
				return gorm.ErrRecordNotFound
			}

			before := map[string]any{"active": grant.IsActive, "reasonCode": grant.ReasonCode, "note": grant.Note}
			after := map[string]any{"active": false, "reasonCode": reasonCode, "note": note}

			if err := tx.WithContext(c.Request().Context()).Model(&dal.RealmAccessGrant{}).Where("id = ?", grant.ID).Updates(map[string]any{
				"is_active":      false,
				"reason_code":    reasonCode,
				"note":           note,
				"last_action_by": actor.AccountID,
			}).Error; err != nil {
				return err
			}

			if err := appendAdminAudit(tx, actor.AccountID, actionRealmAccessRevoke, reasonCode, note, before, after, tick, realmID); err != nil {
				return err
			}

			result["realmId"] = realmID
			result["accountId"] = req.AccountID
			result["active"] = false
			result["occurredTick"] = tick
			return nil
		})
		if err != nil {
			if err == gorm.ErrRecordNotFound {
				return echo.NewHTTPError(http.StatusNotFound, "realm access grant not found")
			}
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to revoke realm access")
		}

		return respondSuccess(c, http.StatusOK, "Realm access revoked.", result)
	}
}

func makeAccountLockHandler(database *gorm.DB) echo.HandlerFunc {
	return func(c echo.Context) error {
		accountID, err := parseAccountIDPathParam(c.Param("id"))
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}

		actor, ok := serverAuth.ActorFromContext(c.Request().Context())
		if !ok {
			return echo.NewHTTPError(http.StatusUnauthorized, "missing actor context")
		}

		var req moderationActionRequest
		if err := c.Bind(&req); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid moderation payload")
		}

		reasonCode, note, err := validateReasonAndNote(req.ReasonCode, req.Note)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}

		tick, err := adminAuditTick(c.Request().Context(), database, 1)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load world tick")
		}

		result := map[string]any{}
		err = database.WithContext(c.Request().Context()).Transaction(func(tx *gorm.DB) error {
			account := dal.Account{}
			query := tx.WithContext(c.Request().Context()).Where("id = ?", accountID).Limit(1).Find(&account)
			if query.Error != nil {
				return query.Error
			}
			if query.RowsAffected == 0 {
				return gorm.ErrRecordNotFound
			}
			if accountID == actor.AccountID {
				return errCannotLockOwnAccount
			}

			hasAdminRole, err := accountHasRole(c.Request().Context(), tx, accountID, roleAdmin)
			if err != nil {
				return err
			}
			if hasAdminRole {
				remainingAdmins, err := countActiveAdminAccountsExcluding(c.Request().Context(), tx, accountID)
				if err != nil {
					return err
				}
				if remainingAdmins == 0 {
					return errLastActiveAdminBlocked
				}
			}

			previous := map[string]any{"status": account.Status}
			if err := tx.WithContext(c.Request().Context()).Model(&dal.Account{}).Where("id = ?", accountID).Update("status", statusLocked).Error; err != nil {
				return err
			}

			updates := tx.WithContext(c.Request().Context()).Model(&dal.AccountSession{}).
				Where("account_id = ? AND revoked_at IS NULL", accountID).
				Update("revoked_at", gorm.Expr("NOW()"))
			if updates.Error != nil {
				return updates.Error
			}

			after := map[string]any{"status": statusLocked}
			if err := appendAdminAudit(tx, actor.AccountID, actionAccountLock, reasonCode, note, previous, after, tick, 1); err != nil {
				return err
			}

			result["accountId"] = accountID
			result["status"] = statusLocked
			result["revokedSessions"] = updates.RowsAffected
			result["reasonCode"] = reasonCode
			result["occurredTick"] = tick
			return nil
		})
		if err != nil {
			if err == gorm.ErrRecordNotFound {
				return echo.NewHTTPError(http.StatusNotFound, "account not found")
			}
			if errors.Is(err, errCannotLockOwnAccount) {
				return echo.NewHTTPError(http.StatusConflict, errCannotLockOwnAccount.Error())
			}
			if errors.Is(err, errLastActiveAdminBlocked) {
				return echo.NewHTTPError(http.StatusConflict, errLastActiveAdminBlocked.Error())
			}
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to lock account")
		}

		return respondSuccess(c, http.StatusOK, "Account locked.", result)
	}
}

func makeAccountUnlockHandler(database *gorm.DB) echo.HandlerFunc {
	return func(c echo.Context) error {
		accountID, err := parseAccountIDPathParam(c.Param("id"))
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}

		actor, ok := serverAuth.ActorFromContext(c.Request().Context())
		if !ok {
			return echo.NewHTTPError(http.StatusUnauthorized, "missing actor context")
		}

		var req moderationActionRequest
		if err := c.Bind(&req); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid moderation payload")
		}

		reasonCode, note, err := validateReasonAndNote(req.ReasonCode, req.Note)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}

		tick, err := adminAuditTick(c.Request().Context(), database, 1)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load world tick")
		}

		result := map[string]any{}
		err = database.WithContext(c.Request().Context()).Transaction(func(tx *gorm.DB) error {
			account := dal.Account{}
			query := tx.WithContext(c.Request().Context()).Where("id = ?", accountID).Limit(1).Find(&account)
			if query.Error != nil {
				return query.Error
			}
			if query.RowsAffected == 0 {
				return gorm.ErrRecordNotFound
			}

			previous := map[string]any{"status": account.Status}
			if err := tx.WithContext(c.Request().Context()).Model(&dal.Account{}).Where("id = ?", accountID).Update("status", statusActive).Error; err != nil {
				return err
			}

			after := map[string]any{"status": statusActive}
			if err := appendAdminAudit(tx, actor.AccountID, actionAccountUnlock, reasonCode, note, previous, after, tick, 1); err != nil {
				return err
			}

			result["accountId"] = accountID
			result["status"] = statusActive
			result["reasonCode"] = reasonCode
			result["occurredTick"] = tick
			return nil
		})
		if err != nil {
			if err == gorm.ErrRecordNotFound {
				return echo.NewHTTPError(http.StatusNotFound, "account not found")
			}
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to unlock account")
		}

		return respondSuccess(c, http.StatusOK, "Account unlocked.", result)
	}
}

func makeAccountRoleModerationHandler(database *gorm.DB) echo.HandlerFunc {
	return func(c echo.Context) error {
		accountID, err := parseAccountIDPathParam(c.Param("id"))
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}

		actor, ok := serverAuth.ActorFromContext(c.Request().Context())
		if !ok {
			return echo.NewHTTPError(http.StatusUnauthorized, "missing actor context")
		}

		var req moderationRoleRequest
		if err := c.Bind(&req); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid moderation role payload")
		}

		validated, err := validateRoleModeration(req)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}

		tick, err := adminAuditTick(c.Request().Context(), database, 1)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load world tick")
		}

		result := map[string]any{}
		err = database.WithContext(c.Request().Context()).Transaction(func(tx *gorm.DB) error {
			account := dal.Account{}
			query := tx.WithContext(c.Request().Context()).Where("id = ?", accountID).Limit(1).Find(&account)
			if query.Error != nil {
				return query.Error
			}
			if query.RowsAffected == 0 {
				return gorm.ErrRecordNotFound
			}

			beforeRoles, err := loadAccountRoles(c.Request().Context(), tx, accountID)
			if err != nil {
				return err
			}

			switch validated.Action {
			case "grant":
				var count int64
				if err := tx.WithContext(c.Request().Context()).Model(&dal.AccountRole{}).
					Where("account_id = ? AND role_key = ?", accountID, validated.RoleKey).
					Count(&count).Error; err != nil {
					return err
				}
				if count == 0 {
					if err := tx.WithContext(c.Request().Context()).Create(&dal.AccountRole{AccountID: accountID, RoleKey: validated.RoleKey}).Error; err != nil {
						return err
					}
				}
			case "revoke":
				if validated.RoleKey == roleAdmin {
					if accountID == actor.AccountID {
						return errCannotRevokeOwnAdmin
					}

					remainingAdmins, err := countActiveAdminAccountsExcluding(c.Request().Context(), tx, accountID)
					if err != nil {
						return err
					}
					if remainingAdmins == 0 {
						return errLastActiveAdminBlocked
					}
				}

				if err := tx.WithContext(c.Request().Context()).Where("account_id = ? AND role_key = ?", accountID, validated.RoleKey).Delete(&dal.AccountRole{}).Error; err != nil {
					return err
				}
			}

			afterRoles, err := loadAccountRoles(c.Request().Context(), tx, accountID)
			if err != nil {
				return err
			}

			auditAction := actionRoleGrant
			if validated.Action == "revoke" {
				auditAction = actionRoleRevoke
			}

			if err := appendAdminAudit(tx, actor.AccountID, auditAction, validated.ReasonCode, validated.Note, map[string]any{"roles": beforeRoles}, map[string]any{"roles": afterRoles, "roleKey": validated.RoleKey}, tick, 1); err != nil {
				return err
			}

			result["accountId"] = accountID
			result["roleKey"] = validated.RoleKey
			result["action"] = validated.Action
			result["roles"] = afterRoles
			result["reasonCode"] = validated.ReasonCode
			result["occurredTick"] = tick
			return nil
		})
		if err != nil {
			if err == gorm.ErrRecordNotFound {
				return echo.NewHTTPError(http.StatusNotFound, "account not found")
			}
			if errors.Is(err, errCannotRevokeOwnAdmin) {
				return echo.NewHTTPError(http.StatusConflict, errCannotRevokeOwnAdmin.Error())
			}
			if errors.Is(err, errLastActiveAdminBlocked) {
				return echo.NewHTTPError(http.StatusConflict, errLastActiveAdminBlocked.Error())
			}
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to moderate account role")
		}

		return respondSuccess(c, http.StatusOK, "Account role moderation applied.", result)
	}
}

func makeAccountStatusHandler(database *gorm.DB) echo.HandlerFunc {
	return func(c echo.Context) error {
		accountID, err := parseAccountIDPathParam(c.Param("id"))
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}

		actor, ok := serverAuth.ActorFromContext(c.Request().Context())
		if !ok {
			return echo.NewHTTPError(http.StatusUnauthorized, "missing actor context")
		}

		var req moderationAccountStatusRequest
		if err := c.Bind(&req); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid account status payload")
		}

		validated, err := validateAccountStatusModeration(req)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}

		tick, err := adminAuditTick(c.Request().Context(), database, 1)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load world tick")
		}

		result := map[string]any{}
		err = database.WithContext(c.Request().Context()).Transaction(func(tx *gorm.DB) error {
			account := dal.Account{}
			query := tx.WithContext(c.Request().Context()).Where("id = ?", accountID).Limit(1).Find(&account)
			if query.Error != nil {
				return query.Error
			}
			if query.RowsAffected == 0 {
				return gorm.ErrRecordNotFound
			}

			if validated.Status == statusLocked {
				if accountID == actor.AccountID {
					return errCannotLockOwnAccount
				}

				hasAdminRole, err := accountHasRole(c.Request().Context(), tx, accountID, roleAdmin)
				if err != nil {
					return err
				}
				if hasAdminRole {
					remainingAdmins, err := countActiveAdminAccountsExcluding(c.Request().Context(), tx, accountID)
					if err != nil {
						return err
					}
					if remainingAdmins == 0 {
						return errLastActiveAdminBlocked
					}
				}
			}

			previous := map[string]any{"status": account.Status}
			if err := tx.WithContext(c.Request().Context()).Model(&dal.Account{}).Where("id = ?", accountID).Update("status", validated.Status).Error; err != nil {
				return err
			}

			revokedSessions := int64(0)
			if validated.RevokeSessions {
				updates := tx.WithContext(c.Request().Context()).Model(&dal.AccountSession{}).
					Where("account_id = ? AND revoked_at IS NULL", accountID).
					Update("revoked_at", gorm.Expr("NOW()"))
				if updates.Error != nil {
					return updates.Error
				}
				revokedSessions = updates.RowsAffected
			}

			after := map[string]any{
				"status":          validated.Status,
				"revokeSessions":  validated.RevokeSessions,
				"revokedSessions": revokedSessions,
			}
			if err := appendAdminAudit(tx, actor.AccountID, actionAccountStatusSet, validated.ReasonCode, validated.Note, previous, after, tick, 1); err != nil {
				return err
			}

			result["accountId"] = accountID
			result["status"] = validated.Status
			result["revokeSessions"] = validated.RevokeSessions
			result["revokedSessions"] = revokedSessions
			result["reasonCode"] = validated.ReasonCode
			result["occurredTick"] = tick
			return nil
		})
		if err != nil {
			if err == gorm.ErrRecordNotFound {
				return echo.NewHTTPError(http.StatusNotFound, "account not found")
			}
			if errors.Is(err, errCannotLockOwnAccount) {
				return echo.NewHTTPError(http.StatusConflict, errCannotLockOwnAccount.Error())
			}
			if errors.Is(err, errLastActiveAdminBlocked) {
				return echo.NewHTTPError(http.StatusConflict, errLastActiveAdminBlocked.Error())
			}
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to update account status")
		}

		return respondSuccess(c, http.StatusOK, "Account status updated.", result)
	}
}

func makeCharacterModerationHandler(database *gorm.DB) echo.HandlerFunc {
	return func(c echo.Context) error {
		characterID, err := parseCharacterIDPathParam(c.Param("id"))
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}

		actor, ok := serverAuth.ActorFromContext(c.Request().Context())
		if !ok {
			return echo.NewHTTPError(http.StatusUnauthorized, "missing actor context")
		}

		var req moderationCharacterRequest
		if err := c.Bind(&req); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid character moderation payload")
		}

		validated, err := validateCharacterModeration(req)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}

		tick, err := adminAuditTick(c.Request().Context(), database, 1)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load world tick")
		}

		result := map[string]any{}
		err = database.WithContext(c.Request().Context()).Transaction(func(tx *gorm.DB) error {
			character := dal.Character{}
			query := tx.WithContext(c.Request().Context()).Where("id = ?", characterID).Limit(1).Find(&character)
			if query.Error != nil {
				return query.Error
			}
			if query.RowsAffected == 0 {
				return gorm.ErrRecordNotFound
			}

			before := map[string]any{
				"name":      character.Name,
				"status":    character.Status,
				"isPrimary": character.IsPrimary,
			}

			updates := map[string]any{}
			if validated.Name != nil {
				updates["name"] = *validated.Name
			}
			if validated.Status != nil {
				updates["status"] = *validated.Status
			}

			if len(updates) > 0 {
				if err := tx.WithContext(c.Request().Context()).Model(&dal.Character{}).Where("id = ?", characterID).Updates(updates).Error; err != nil {
					return err
				}
			}

			if validated.IsPrimary != nil {
				if *validated.IsPrimary {
					if err := tx.WithContext(c.Request().Context()).Model(&dal.Character{}).
						Where("account_id = ?", character.AccountID).
						Update("is_primary", false).Error; err != nil {
						return err
					}
				}
				if err := tx.WithContext(c.Request().Context()).Model(&dal.Character{}).
					Where("id = ?", characterID).
					Update("is_primary", *validated.IsPrimary).Error; err != nil {
					return err
				}
			}

			updated := dal.Character{}
			if err := tx.WithContext(c.Request().Context()).Where("id = ?", characterID).Limit(1).Find(&updated).Error; err != nil {
				return err
			}

			after := map[string]any{
				"name":      updated.Name,
				"status":    updated.Status,
				"isPrimary": updated.IsPrimary,
			}
			if err := appendAdminAudit(tx, actor.AccountID, actionCharacterModify, validated.ReasonCode, validated.Note, before, after, tick, updated.RealmID); err != nil {
				return err
			}

			result["characterId"] = updated.ID
			result["accountId"] = updated.AccountID
			result["playerId"] = updated.PlayerID
			result["realmId"] = updated.RealmID
			result["name"] = updated.Name
			result["status"] = updated.Status
			result["isPrimary"] = updated.IsPrimary
			result["reasonCode"] = validated.ReasonCode
			result["occurredTick"] = tick
			return nil
		})
		if err != nil {
			if err == gorm.ErrRecordNotFound {
				return echo.NewHTTPError(http.StatusNotFound, "character not found")
			}
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to moderate character")
		}

		return respondSuccess(c, http.StatusOK, "Character moderation applied.", result)
	}
}

func makeCharacterModerationListHandler(database *gorm.DB) echo.HandlerFunc {
	return func(c echo.Context) error {
		filters, err := parseCharacterModerationFilters(c)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}

		query := database.WithContext(c.Request().Context()).Model(&dal.Character{})
		if filters.AccountID != 0 {
			query = query.Where("account_id = ?", filters.AccountID)
		}
		if filters.AccountUsername != "" {
			query = query.Joins("JOIN accounts ON accounts.id = characters.account_id").Where("LOWER(accounts.username) LIKE ?", "%"+strings.ToLower(filters.AccountUsername)+"%")
		}
		if filters.RealmID != 0 {
			query = query.Where("realm_id = ?", filters.RealmID)
		}
		if filters.Status != "" {
			query = query.Where("status = ?", filters.Status)
		}
		if filters.NameLike != "" {
			query = query.Where("LOWER(name) LIKE ?", "%"+strings.ToLower(filters.NameLike)+"%")
		}
		if filters.BeforeID != 0 {
			query = query.Where("id < ?", filters.BeforeID)
		}

		rows := make([]dal.Character, 0)
		if err := query.Order("id DESC").Limit(filters.Limit + 1).Find(&rows).Error; err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load characters")
		}

		hasMore := len(rows) > filters.Limit
		nextBeforeID := uint(0)
		if hasMore {
			rows = rows[:filters.Limit]
			nextBeforeID = rows[len(rows)-1].ID
		}

		accountIDs := make([]uint, 0, len(rows))
		seenAccount := map[uint]struct{}{}
		for _, row := range rows {
			if _, exists := seenAccount[row.AccountID]; exists {
				continue
			}
			seenAccount[row.AccountID] = struct{}{}
			accountIDs = append(accountIDs, row.AccountID)
		}

		accountRows := make([]dal.Account, 0, len(accountIDs))
		if len(accountIDs) > 0 {
			if err := database.WithContext(c.Request().Context()).
				Select("id, username").
				Where("id IN ?", accountIDs).
				Find(&accountRows).Error; err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "failed to load character account info")
			}
		}

		accountUsernameByID := map[uint]string{}
		for _, account := range accountRows {
			accountUsernameByID[account.ID] = account.Username
		}

		entries := make([]map[string]any, 0, len(rows))
		for _, row := range rows {
			entries = append(entries, map[string]any{
				"id":              row.ID,
				"accountId":       row.AccountID,
				"accountUsername": accountUsernameByID[row.AccountID],
				"playerId":        row.PlayerID,
				"realmId":         row.RealmID,
				"name":            row.Name,
				"isPrimary":       row.IsPrimary,
				"status":          row.Status,
				"updatedAt":       row.UpdatedAt,
			})
		}

		return respondSuccess(c, http.StatusOK, "Character moderation list loaded.", map[string]any{
			"filters": map[string]any{
				"accountId":       filters.AccountID,
				"accountUsername": filters.AccountUsername,
				"realmId":         filters.RealmID,
				"status":          filters.Status,
				"nameLike":        filters.NameLike,
				"beforeId":        filters.BeforeID,
				"limit":           filters.Limit,
			},
			"pagination": map[string]any{
				"hasMore":      hasMore,
				"nextBeforeId": nextBeforeID,
			},
			"entries": entries,
		})
	}
}

func parseRealmIDPathParam(raw string) (uint, error) {
	parsed, err := strconv.ParseUint(strings.TrimSpace(raw), 10, 64)
	if err != nil || parsed == 0 {
		return 0, fmt.Errorf("realm id must be a positive integer")
	}
	return uint(parsed), nil
}

func parseAccountIDPathParam(raw string) (uint, error) {
	parsed, err := strconv.ParseUint(strings.TrimSpace(raw), 10, 64)
	if err != nil || parsed == 0 {
		return 0, fmt.Errorf("account id must be a positive integer")
	}
	return uint(parsed), nil
}

func parseCharacterIDPathParam(raw string) (uint, error) {
	parsed, err := strconv.ParseUint(strings.TrimSpace(raw), 10, 64)
	if err != nil || parsed == 0 {
		return 0, fmt.Errorf("character id must be a positive integer")
	}
	return uint(parsed), nil
}

func parseAuditIDPathParam(raw string) (uint, error) {
	parsed, err := strconv.ParseUint(strings.TrimSpace(raw), 10, 64)
	if err != nil || parsed == 0 {
		return 0, fmt.Errorf("audit id must be a positive integer")
	}
	return uint(parsed), nil
}

func parseAuditFilters(c echo.Context) (auditQuery, error) {
	realmID, err := parseOptionalUintQuery(c.QueryParam("realmId"), "realmId")
	if err != nil {
		return auditQuery{}, err
	}

	actorAccountID, err := parseOptionalUintQuery(c.QueryParam("actorAccountId"), "actorAccountId")
	if err != nil {
		return auditQuery{}, err
	}

	beforeID, err := parseOptionalUintQuery(c.QueryParam("beforeId"), "beforeId")
	if err != nil {
		return auditQuery{}, err
	}

	includeRawJSON, err := parseOptionalBoolQuery(c.QueryParam("includeRawJson"), "includeRawJson")
	if err != nil {
		return auditQuery{}, err
	}

	actionKey := strings.TrimSpace(strings.ToLower(c.QueryParam("actionKey")))
	if len(actionKey) > 64 {
		return auditQuery{}, fmt.Errorf("actionKey must be 64 characters or less")
	}

	actorUsername := strings.TrimSpace(strings.ToLower(c.QueryParam("actorUsername")))
	if len(actorUsername) > 64 {
		return auditQuery{}, fmt.Errorf("actorUsername must be 64 characters or less")
	}

	limit, err := parseAuditLimit(c.QueryParam("limit"))
	if err != nil {
		return auditQuery{}, err
	}

	return auditQuery{
		RealmID:        realmID,
		ActorAccountID: actorAccountID,
		ActorUsername:  actorUsername,
		ActionKey:      actionKey,
		BeforeID:       beforeID,
		IncludeRawJSON: includeRawJSON,
		Limit:          limit,
	}, nil
}

func parseCharacterModerationFilters(c echo.Context) (characterModerationQuery, error) {
	accountID, err := parseOptionalUintQuery(c.QueryParam("accountId"), "accountId")
	if err != nil {
		return characterModerationQuery{}, err
	}

	accountUsername := strings.TrimSpace(strings.ToLower(c.QueryParam("accountUsername")))
	if len(accountUsername) > 64 {
		return characterModerationQuery{}, fmt.Errorf("accountUsername must be 64 characters or less")
	}

	realmID, err := parseOptionalUintQuery(c.QueryParam("realmId"), "realmId")
	if err != nil {
		return characterModerationQuery{}, err
	}

	beforeID, err := parseOptionalUintQuery(c.QueryParam("beforeId"), "beforeId")
	if err != nil {
		return characterModerationQuery{}, err
	}

	status := strings.TrimSpace(strings.ToLower(c.QueryParam("status")))
	if status != "" && status != statusActive && status != statusLocked {
		return characterModerationQuery{}, fmt.Errorf("status must be active or locked")
	}

	nameLike := strings.TrimSpace(c.QueryParam("nameLike"))
	if len(nameLike) > 64 {
		return characterModerationQuery{}, fmt.Errorf("nameLike must be 64 characters or less")
	}

	limit, err := parseAuditLimit(c.QueryParam("limit"))
	if err != nil {
		return characterModerationQuery{}, err
	}

	return characterModerationQuery{
		AccountID:       accountID,
		AccountUsername: accountUsername,
		RealmID:         realmID,
		Status:          status,
		NameLike:        nameLike,
		BeforeID:        beforeID,
		Limit:           limit,
	}, nil
}

func parseOptionalUintQuery(raw string, field string) (uint, error) {
	if strings.TrimSpace(raw) == "" {
		return 0, nil
	}

	parsed, err := strconv.ParseUint(strings.TrimSpace(raw), 10, 64)
	if err != nil || parsed == 0 {
		return 0, fmt.Errorf("%s must be a positive integer", field)
	}

	return uint(parsed), nil
}

func parseOptionalBoolQuery(raw string, field string) (bool, error) {
	trimmed := strings.TrimSpace(strings.ToLower(raw))
	if trimmed == "" {
		return false, nil
	}

	if trimmed == "true" || trimmed == "1" {
		return true, nil
	}
	if trimmed == "false" || trimmed == "0" {
		return false, nil
	}

	return false, fmt.Errorf("%s must be a boolean (true/false)", field)
}

func parseAuditLimit(raw string) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return defaultAuditLimit, nil
	}

	parsed, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("limit must be a positive integer")
	}
	if parsed > maxAuditLimit {
		parsed = maxAuditLimit
	}

	return parsed, nil
}

func parseWindowTicks(raw string) (int64, error) {
	if strings.TrimSpace(raw) == "" {
		return defaultStatsWindowTicks, nil
	}

	parsed, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("windowTicks must be a positive integer")
	}
	if parsed > maxStatsWindowTicks {
		parsed = maxStatsWindowTicks
	}

	return parsed, nil
}

func loadCurrentGlobalTick(ctx context.Context, database *gorm.DB) (int64, error) {
	var tick int64
	if err := database.WithContext(ctx).
		Model(&dal.WorldState{}).
		Select("COALESCE(MAX(simulation_tick), 0)").
		Scan(&tick).Error; err != nil {
		return 0, err
	}

	return tick, nil
}

func validateRealmAction(req realmActionRequest) (validatedRealmAction, error) {
	action := strings.TrimSpace(strings.ToLower(req.Action))
	reasonCode := strings.TrimSpace(strings.ToLower(req.ReasonCode))
	note := strings.TrimSpace(req.Note)
	itemKey := strings.TrimSpace(strings.ToLower(req.ItemKey))

	if action == "" {
		return validatedRealmAction{}, fmt.Errorf("action is required")
	}
	if reasonCode == "" {
		return validatedRealmAction{}, fmt.Errorf("reasonCode is required")
	}
	if len(reasonCode) > 64 {
		return validatedRealmAction{}, fmt.Errorf("reasonCode must be 64 characters or less")
	}
	if len(note) > 500 {
		return validatedRealmAction{}, fmt.Errorf("note must be 500 characters or less")
	}

	validated := validatedRealmAction{
		Action:     action,
		ReasonCode: reasonCode,
		Note:       note,
		ItemKey:    itemKey,
		Price:      req.Price,
	}

	switch action {
	case actionMarketResetDefaults:
		return validated, nil
	case actionMarketSetPrice:
		if itemKey == "" {
			return validatedRealmAction{}, fmt.Errorf("itemKey is required for market_set_price")
		}
		if req.Price <= 0 {
			return validatedRealmAction{}, fmt.Errorf("price must be a positive integer for market_set_price")
		}
		return validated, nil
	case actionRealmPause, actionRealmResume, actionRealmCreate:
		return validated, nil
	default:
		return validatedRealmAction{}, fmt.Errorf("unsupported action")
	}
}

func validateReasonAndNote(rawReasonCode string, rawNote string) (string, string, error) {
	reasonCode := strings.TrimSpace(strings.ToLower(rawReasonCode))
	note := strings.TrimSpace(rawNote)

	if reasonCode == "" {
		return "", "", fmt.Errorf("reasonCode is required")
	}
	if len(reasonCode) > 64 {
		return "", "", fmt.Errorf("reasonCode must be 64 characters or less")
	}
	if len(note) > 500 {
		return "", "", fmt.Errorf("note must be 500 characters or less")
	}

	return reasonCode, note, nil
}

func validateRoleModeration(req moderationRoleRequest) (validatedRoleModeration, error) {
	action := strings.TrimSpace(strings.ToLower(req.Action))
	roleKey := strings.TrimSpace(strings.ToLower(req.RoleKey))
	reasonCode, note, err := validateReasonAndNote(req.ReasonCode, req.Note)
	if err != nil {
		return validatedRoleModeration{}, err
	}

	if action != "grant" && action != "revoke" {
		return validatedRoleModeration{}, fmt.Errorf("action must be grant or revoke")
	}
	if roleKey == "" {
		return validatedRoleModeration{}, fmt.Errorf("roleKey is required")
	}
	if len(roleKey) > 32 {
		return validatedRoleModeration{}, fmt.Errorf("roleKey must be 32 characters or less")
	}
	for _, r := range roleKey {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' {
			continue
		}
		return validatedRoleModeration{}, fmt.Errorf("roleKey may only contain letters, numbers, underscores, or hyphens")
	}

	return validatedRoleModeration{
		RoleKey:    roleKey,
		Action:     action,
		ReasonCode: reasonCode,
		Note:       note,
	}, nil
}

func validateAccountStatusModeration(req moderationAccountStatusRequest) (validatedAccountStatusModeration, error) {
	status := strings.TrimSpace(strings.ToLower(req.Status))
	if status != statusActive && status != statusLocked {
		return validatedAccountStatusModeration{}, fmt.Errorf("status must be active or locked")
	}

	reasonCode, note, err := validateReasonAndNote(req.ReasonCode, req.Note)
	if err != nil {
		return validatedAccountStatusModeration{}, err
	}

	revokeSessions := status == statusLocked
	if req.RevokeSessions != nil {
		revokeSessions = *req.RevokeSessions
	}

	return validatedAccountStatusModeration{
		Status:         status,
		ReasonCode:     reasonCode,
		Note:           note,
		RevokeSessions: revokeSessions,
	}, nil
}

func validateCharacterModeration(req moderationCharacterRequest) (validatedCharacterModeration, error) {
	reasonCode, note, err := validateReasonAndNote(req.ReasonCode, req.Note)
	if err != nil {
		return validatedCharacterModeration{}, err
	}

	validated := validatedCharacterModeration{
		ReasonCode: reasonCode,
		Note:       note,
	}

	if req.Name != nil {
		trimmed := strings.TrimSpace(*req.Name)
		if len(trimmed) < 3 || len(trimmed) > 64 {
			return validatedCharacterModeration{}, fmt.Errorf("name must be between 3 and 64 characters")
		}
		validated.Name = &trimmed
	}

	if req.Status != nil {
		trimmedStatus := strings.TrimSpace(strings.ToLower(*req.Status))
		if trimmedStatus != statusActive && trimmedStatus != statusLocked {
			return validatedCharacterModeration{}, fmt.Errorf("status must be active or locked")
		}
		validated.Status = &trimmedStatus
	}

	if req.IsPrimary != nil {
		isPrimary := *req.IsPrimary
		validated.IsPrimary = &isPrimary
	}

	if validated.Name == nil && validated.Status == nil && validated.IsPrimary == nil {
		return validatedCharacterModeration{}, fmt.Errorf("at least one character field must be provided")
	}

	return validated, nil
}

func adminAuditTick(ctx context.Context, database *gorm.DB, realmID uint) (int64, error) {
	if realmID == 0 {
		realmID = 1
	}
	return gameplay.CurrentWorldTickForRealm(ctx, database, realmID)
}

func appendAdminAudit(tx *gorm.DB, actorAccountID uint, actionKey, reasonCode, note string, before any, after any, tick int64, realmID uint) error {
	beforeJSON, encodeErr := encodeJSON(before)
	if encodeErr != nil {
		return encodeErr
	}
	afterJSON, encodeErr := encodeJSON(after)
	if encodeErr != nil {
		return encodeErr
	}

	audit := dal.AdminAuditEvent{
		RealmID:        realmID,
		ActorAccountID: actorAccountID,
		ActionKey:      actionKey,
		ReasonCode:     reasonCode,
		Note:           note,
		BeforeJSON:     beforeJSON,
		AfterJSON:      afterJSON,
		OccurredTick:   tick,
	}

	return tx.Create(&audit).Error
}

func loadAccountRoles(ctx context.Context, database *gorm.DB, accountID uint) ([]string, error) {
	rows := make([]dal.AccountRole, 0)
	if err := database.WithContext(ctx).Where("account_id = ?", accountID).Find(&rows).Error; err != nil {
		return nil, err
	}

	roles := make([]string, 0, len(rows))
	for _, row := range rows {
		roles = append(roles, row.RoleKey)
	}
	sort.Strings(roles)
	return roles, nil
}

func accountHasRole(ctx context.Context, database *gorm.DB, accountID uint, roleKey string) (bool, error) {
	var count int64
	err := database.WithContext(ctx).
		Model(&dal.AccountRole{}).
		Where("account_id = ? AND role_key = ?", accountID, roleKey).
		Count(&count).Error
	if err != nil {
		return false, err
	}

	return count > 0, nil
}

func countActiveAdminAccountsExcluding(ctx context.Context, database *gorm.DB, excludedAccountID uint) (int64, error) {
	query := database.WithContext(ctx).
		Model(&dal.AccountRole{}).
		Joins("JOIN accounts ON accounts.id = account_roles.account_id").
		Where("account_roles.role_key = ? AND accounts.status = ?", roleAdmin, statusActive)
	if excludedAccountID != 0 {
		query = query.Where("account_roles.account_id <> ?", excludedAccountID)
	}

	var count int64
	err := query.Distinct("account_roles.account_id").Count(&count).Error
	if err != nil {
		return 0, err
	}

	return count, nil
}

func applyRealmPause(ctx context.Context, tx *gorm.DB, realmID uint, reasonCode, note string, tick int64) (map[string]any, map[string]any, int64, error) {
	beforePaused, beforeReason, err := loadRealmPauseState(ctx, tx, realmID)
	if err != nil {
		return nil, nil, 0, err
	}

	updates := tx.WithContext(ctx).Model(&dal.AccountSession{}).
		Where("account_id IN (SELECT DISTINCT account_id FROM characters WHERE realm_id = ? AND status = ?) AND revoked_at IS NULL", realmID, statusActive).
		Update("revoked_at", gorm.Expr("NOW()"))
	if updates.Error != nil {
		return nil, nil, 0, updates.Error
	}

	state := map[string]any{
		"paused":        true,
		"reasonCode":    reasonCode,
		"note":          note,
		"effectiveTick": tick,
	}
	encoded, err := encodeJSON(state)
	if err != nil {
		return nil, nil, 0, err
	}

	if err := upsertRealmControlState(ctx, tx, realmID, 1, encoded); err != nil {
		return nil, nil, 0, err
	}

	return map[string]any{"paused": beforePaused, "reasonCode": beforeReason}, state, updates.RowsAffected, nil
}

func applyRealmResume(ctx context.Context, tx *gorm.DB, realmID uint, reasonCode, note string, tick int64) (map[string]any, map[string]any, error) {
	beforePaused, beforeReason, err := loadRealmPauseState(ctx, tx, realmID)
	if err != nil {
		return nil, nil, err
	}

	state := map[string]any{
		"paused":        false,
		"reasonCode":    reasonCode,
		"note":          note,
		"effectiveTick": tick,
	}
	encoded, err := encodeJSON(state)
	if err != nil {
		return nil, nil, err
	}

	if err := upsertRealmControlState(ctx, tx, realmID, 0, encoded); err != nil {
		return nil, nil, err
	}

	return map[string]any{"paused": beforePaused, "reasonCode": beforeReason}, state, nil
}

func applyRealmCreate(ctx context.Context, tx *gorm.DB, realmID uint) (map[string]any, map[string]any, error) {
	before := map[string]any{"existed": false}
	created := map[string]any{"worldState": false, "worldRuntime": false, "realmControl": false, "realmConfig": false}

	state := dal.WorldState{}
	stateResult := tx.WithContext(ctx).Where("realm_id = ?", realmID).Limit(1).Find(&state)
	if stateResult.Error != nil {
		return nil, nil, stateResult.Error
	}
	if stateResult.RowsAffected > 0 {
		before["existed"] = true
	} else {
		if err := tx.WithContext(ctx).Create(&dal.WorldState{RealmID: realmID, SimulationTick: 0}).Error; err != nil {
			return nil, nil, err
		}
		created["worldState"] = true
	}

	runtime := dal.WorldRuntimeState{}
	runtimeResult := tx.WithContext(ctx).Where("realm_id = ? AND key = ?", realmID, "world").Limit(1).Find(&runtime)
	if runtimeResult.Error != nil {
		return nil, nil, runtimeResult.Error
	}
	if runtimeResult.RowsAffected == 0 {
		if err := tx.WithContext(ctx).Create(&dal.WorldRuntimeState{
			RealmID:              realmID,
			Key:                  "world",
			LastProcessedTickAt:  time.Now().UTC(),
			CarryGameMinutes:     0,
			PendingBehaviorsJSON: "[]",
		}).Error; err != nil {
			return nil, nil, err
		}
		created["worldRuntime"] = true
	}

	if err := upsertRealmControlState(ctx, tx, realmID, 0, `{"paused":false}`); err != nil {
		return nil, nil, err
	}
	created["realmControl"] = true

	config := dal.RealmConfig{}
	configResult := tx.WithContext(ctx).Where("realm_id = ?", realmID).Limit(1).Find(&config)
	if configResult.Error != nil {
		return nil, nil, configResult.Error
	}
	if configResult.RowsAffected == 0 {
		if err := tx.WithContext(ctx).Create(&dal.RealmConfig{RealmID: realmID, DisplayName: defaultRealmDisplayName(realmID), WhitelistOnly: false}).Error; err != nil {
			return nil, nil, err
		}
		created["realmConfig"] = true
	}

	after := map[string]any{"realmId": realmID, "created": created}
	return before, after, nil
}

func loadRealmPauseState(ctx context.Context, database *gorm.DB, realmID uint) (bool, string, error) {
	state := dal.WorldRuntimeState{}
	result := database.WithContext(ctx).Where("realm_id = ? AND key = ?", realmID, realmControlStateKey).Limit(1).Find(&state)
	if result.Error != nil {
		return false, "", result.Error
	}
	if result.RowsAffected == 0 {
		return false, "", nil
	}

	reasonCode := ""
	decoded := map[string]any{}
	if err := json.Unmarshal([]byte(state.PendingBehaviorsJSON), &decoded); err == nil {
		if rawReason, ok := decoded["reasonCode"].(string); ok {
			reasonCode = strings.TrimSpace(rawReason)
		}
	}

	return state.CarryGameMinutes >= 1, reasonCode, nil
}

func upsertRealmControlState(ctx context.Context, database *gorm.DB, realmID uint, pausedFlag float64, payload string) error {
	state := dal.WorldRuntimeState{}
	result := database.WithContext(ctx).Where("realm_id = ? AND key = ?", realmID, realmControlStateKey).Limit(1).Find(&state)
	if result.Error != nil {
		return result.Error
	}

	now := time.Now().UTC()
	if result.RowsAffected == 0 {
		return database.WithContext(ctx).Create(&dal.WorldRuntimeState{
			RealmID:              realmID,
			Key:                  realmControlStateKey,
			LastProcessedTickAt:  now,
			CarryGameMinutes:     pausedFlag,
			PendingBehaviorsJSON: payload,
		}).Error
	}

	return database.WithContext(ctx).Model(&dal.WorldRuntimeState{}).
		Where("id = ?", state.ID).
		Updates(map[string]any{
			"last_processed_tick_at": now,
			"carry_game_minutes":     pausedFlag,
			"pending_behaviors_json": payload,
		}).Error
}

func applyMarketResetDefaults(_ context.Context, tx *gorm.DB, realmID uint, tick int64) (map[string]int64, map[string]int64, error) {
	defaults := map[string]int64{"scrap": 8, "wood": 5}
	before := map[string]int64{}
	after := map[string]int64{}

	for itemKey, targetPrice := range defaults {
		entry := dal.MarketPrice{}
		result := tx.Where("realm_id = ? AND item_key = ?", realmID, itemKey).Limit(1).Find(&entry)
		if result.Error != nil {
			return nil, nil, result.Error
		}

		prevPrice := int64(0)
		if result.RowsAffected > 0 {
			prevPrice = entry.Price
			entry.Price = targetPrice
			entry.LastDelta = targetPrice - prevPrice
			entry.LastSource = actionMarketResetDefaults
			entry.UpdatedTick = tick
			if err := tx.Model(&dal.MarketPrice{}).Where("id = ?", entry.ID).Updates(map[string]any{
				"price":        entry.Price,
				"last_delta":   entry.LastDelta,
				"last_source":  entry.LastSource,
				"updated_tick": entry.UpdatedTick,
			}).Error; err != nil {
				return nil, nil, err
			}
		} else {
			entry = dal.MarketPrice{
				RealmID:     realmID,
				ItemKey:     itemKey,
				Price:       targetPrice,
				LastDelta:   targetPrice,
				LastSource:  actionMarketResetDefaults,
				UpdatedTick: tick,
			}
			if err := tx.Create(&entry).Error; err != nil {
				return nil, nil, err
			}
		}

		if err := tx.Create(&dal.MarketHistory{
			RealmID:      realmID,
			ItemKey:      itemKey,
			Tick:         tick,
			Price:        targetPrice,
			Delta:        targetPrice - prevPrice,
			Source:       actionMarketResetDefaults,
			SessionState: gameplay.MarketSessionState(tick),
		}).Error; err != nil {
			return nil, nil, err
		}

		before[itemKey] = prevPrice
		after[itemKey] = targetPrice
	}

	return before, after, nil
}

func applyMarketSetPrice(_ context.Context, tx *gorm.DB, realmID uint, tick int64, itemKey string, price int64) (map[string]any, map[string]any, error) {
	entry := dal.MarketPrice{}
	result := tx.Where("realm_id = ? AND item_key = ?", realmID, itemKey).Limit(1).Find(&entry)
	if result.Error != nil {
		return nil, nil, result.Error
	}

	previous := map[string]any{"itemKey": itemKey, "price": int64(0)}
	if result.RowsAffected > 0 {
		previous["price"] = entry.Price
		if err := tx.Model(&dal.MarketPrice{}).Where("id = ?", entry.ID).Updates(map[string]any{
			"price":        price,
			"last_delta":   price - entry.Price,
			"last_source":  actionMarketSetPrice,
			"updated_tick": tick,
		}).Error; err != nil {
			return nil, nil, err
		}
	} else {
		entry = dal.MarketPrice{
			RealmID:     realmID,
			ItemKey:     itemKey,
			Price:       price,
			LastDelta:   price,
			LastSource:  actionMarketSetPrice,
			UpdatedTick: tick,
		}
		if err := tx.Create(&entry).Error; err != nil {
			return nil, nil, err
		}
	}

	delta := price - previous["price"].(int64)
	if err := tx.Create(&dal.MarketHistory{
		RealmID:      realmID,
		ItemKey:      itemKey,
		Tick:         tick,
		Price:        price,
		Delta:        delta,
		Source:       actionMarketSetPrice,
		SessionState: gameplay.MarketSessionState(tick),
	}).Error; err != nil {
		return nil, nil, err
	}

	after := map[string]any{"itemKey": itemKey, "price": price}
	return previous, after, nil
}

func encodeJSON(value any) (string, error) {
	if value == nil {
		return "{}", nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func decodeMaybeJSON(raw string) any {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return map[string]any{}
	}

	decoded := any(nil)
	if err := json.Unmarshal([]byte(trimmed), &decoded); err != nil {
		return raw
	}
	if decoded == nil {
		return map[string]any{}
	}
	return decoded
}

func defaultRealmDisplayName(realmID uint) string {
	if realmID == 0 {
		realmID = 1
	}
	return fmt.Sprintf("Realm %d", realmID)
}

func buildAuditCSV(rows []dal.AdminAuditEvent) ([]byte, error) {
	buffer := bytes.NewBuffer(nil)
	writer := csv.NewWriter(buffer)

	header := []string{"id", "realm_id", "actor_account_id", "action_key", "reason_code", "note", "occurred_tick", "created_at", "before_json", "after_json"}
	if err := writer.Write(header); err != nil {
		return nil, err
	}

	for _, row := range rows {
		record := []string{
			strconv.FormatUint(uint64(row.ID), 10),
			strconv.FormatUint(uint64(row.RealmID), 10),
			strconv.FormatUint(uint64(row.ActorAccountID), 10),
			row.ActionKey,
			row.ReasonCode,
			row.Note,
			strconv.FormatInt(row.OccurredTick, 10),
			row.CreatedAt.UTC().Format(time.RFC3339),
			row.BeforeJSON,
			row.AfterJSON,
		}
		if err := writer.Write(record); err != nil {
			return nil, err
		}
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, err
	}

	return buffer.Bytes(), nil
}

func requireAdminRole() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			actor, ok := serverAuth.ActorFromContext(c.Request().Context())
			if !ok {
				return echo.NewHTTPError(http.StatusUnauthorized, "missing actor context")
			}
			if !hasRole(actor.Roles, roleAdmin) {
				return echo.NewHTTPError(http.StatusForbidden, "admin role required")
			}
			return next(c)
		}
	}
}

func hasRole(roles []string, role string) bool {
	for _, entry := range roles {
		if entry == role {
			return true
		}
	}
	return false
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
