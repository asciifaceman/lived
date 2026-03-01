package admin

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
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
	defaultAuditLimit       = 100
	maxAuditLimit           = 500
	defaultStatsWindowTicks = int64(24 * 60)
	maxStatsWindowTicks     = int64(30 * 24 * 60)

	actionMarketResetDefaults = "market_reset_defaults"
	actionMarketSetPrice      = "market_set_price"
	actionAccountLock         = "account_lock"
	actionAccountUnlock       = "account_unlock"
	actionRoleGrant           = "account_role_grant"
	actionRoleRevoke          = "account_role_revoke"
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

type validatedRoleModeration struct {
	RoleKey    string
	Action     string
	ReasonCode string
	Note       string
}

type auditQuery struct {
	RealmID        uint
	ActorAccountID uint
	ActionKey      string
	BeforeID       uint
	IncludeRawJSON bool
	Limit          int
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
	group.POST("/moderation/accounts/:id/lock", makeAccountLockHandler(database), authMW, requireAdminRole())
	group.POST("/moderation/accounts/:id/unlock", makeAccountUnlockHandler(database), authMW, requireAdminRole())
	group.POST("/moderation/accounts/:id/roles", makeAccountRoleModerationHandler(database), authMW, requireAdminRole())
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

			realms = append(realms, map[string]any{
				"realmId":          realmID,
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

		tick, err := adminAuditTick(c.Request().Context(), database)
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

		tick, err := adminAuditTick(c.Request().Context(), database)
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

		tick, err := adminAuditTick(c.Request().Context(), database)
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
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to moderate account role")
		}

		return respondSuccess(c, http.StatusOK, "Account role moderation applied.", result)
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

	limit, err := parseAuditLimit(c.QueryParam("limit"))
	if err != nil {
		return auditQuery{}, err
	}

	return auditQuery{
		RealmID:        realmID,
		ActorAccountID: actorAccountID,
		ActionKey:      actionKey,
		BeforeID:       beforeID,
		IncludeRawJSON: includeRawJSON,
		Limit:          limit,
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

func adminAuditTick(ctx context.Context, database *gorm.DB) (int64, error) {
	return gameplay.CurrentWorldTickForRealm(ctx, database, 1)
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
