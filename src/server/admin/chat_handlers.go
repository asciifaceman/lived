package admin

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/asciifaceman/lived/pkg/dal"
	"github.com/asciifaceman/lived/src/gameplay"
	serverAuth "github.com/asciifaceman/lived/src/server/auth"
	"github.com/asciifaceman/lived/src/server/requestbind"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

const (
	chatEventPrefixPlayer = "chat_message:"
	chatEventPrefixMod    = "chat_message_mod:"
	chatEventPrefixAdmin  = "chat_message_admin:"
	chatEventPrefixSystem = "chat_message_system:"

	chatBindingScopeRealm  = "realm"
	chatPolicyScopeGlobal  = "global"
	chatPolicyScopeAllText = "all_realms_all_channels"
	chatPolicyScopeKey     = "global"
)

type chatChannelCreateRequest struct {
	Command     string `json:"command,omitempty"`
	Scope       string `json:"scope,omitempty"`
	RealmID     uint   `json:"realmId"`
	Key         string `json:"key"`
	Name        string `json:"name"`
	Subject     string `json:"subject,omitempty"`
	Description string `json:"description,omitempty"`
	ReasonCode  string `json:"reasonCode,omitempty"`
	Note        string `json:"note,omitempty"`
}

var (
	errChatChannelAlreadyExists = errors.New("chat channel already exists")
	errChatChannelNotFound      = errors.New("chat channel not found")
)

type chatChannelActionRequest struct {
	Scope      string `json:"scope,omitempty"`
	RealmID    uint   `json:"realmId"`
	ReasonCode string `json:"reasonCode"`
	Note       string `json:"note,omitempty"`
}

type chatChannelModerationRequest struct {
	Scope           string `json:"scope,omitempty"`
	RealmID         uint   `json:"realmId"`
	AccountID       uint   `json:"accountId"`
	Action          string `json:"action"`
	DurationMinutes int64  `json:"durationMinutes,omitempty"`
	ReasonCode      string `json:"reasonCode"`
	Note            string `json:"note,omitempty"`
}

type chatSystemMessageRequest struct {
	Scope      string `json:"scope,omitempty"`
	RealmID    uint   `json:"realmId"`
	Message    string `json:"message"`
	ReasonCode string `json:"reasonCode"`
	Note       string `json:"note,omitempty"`
}

type chatWordlistRuleRequest struct {
	Scope      string `json:"scope,omitempty"`
	Term       string `json:"term"`
	MatchMode  string `json:"matchMode,omitempty"`
	ReasonCode string `json:"reasonCode"`
	Note       string `json:"note,omitempty"`
}

func makeChatChannelCreateHandler(database *gorm.DB) echo.HandlerFunc {
	return func(c echo.Context) error {
		actor, ok := serverAuth.ActorFromContext(c.Request().Context())
		if !ok {
			return echo.NewHTTPError(http.StatusUnauthorized, "missing actor context")
		}

		var req chatChannelCreateRequest
		if err := requestbind.JSON(c, &req, "invalid channel create payload"); err != nil {
			return err
		}

		command, err := parseChatChannelCommand(req.Command)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}

		bindingScope, err := parseChatBindingScope(req.Scope)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}

		realmID := req.RealmID
		if realmID == 0 {
			realmID = 1
		}
		bindingScopeKey := chatBindingScopeKey(bindingScope, realmID)

		channelKey, err := normalizeChatChannelKey(req.Key)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}

		displayName := strings.TrimSpace(req.Name)
		if command != "attach" && (len(displayName) < 2 || len(displayName) > 64) {
			return echo.NewHTTPError(http.StatusBadRequest, "name must be between 2 and 64 characters")
		}

		description := strings.TrimSpace(req.Description)
		if len(description) > 255 {
			return echo.NewHTTPError(http.StatusBadRequest, "description must be 255 characters or less")
		}

		subject := strings.TrimSpace(req.Subject)
		if len(subject) > 140 {
			return echo.NewHTTPError(http.StatusBadRequest, "subject must be 140 characters or less")
		}

		defaultReason := "chat_channel_create"
		switch command {
		case "create":
			defaultReason = "chat_channel_create"
		case "edit":
			defaultReason = "chat_channel_edit"
		case "attach":
			defaultReason = "chat_channel_attach"
		}

		reasonInput := strings.TrimSpace(req.ReasonCode)
		if reasonInput == "" {
			reasonInput = defaultReason
		}

		reasonCode, note, err := validateReasonAndNote(reasonInput, req.Note)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}

		tick, err := adminAuditTick(c.Request().Context(), database, realmID)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load world tick")
		}

		created := false
		err = database.WithContext(c.Request().Context()).Transaction(func(tx *gorm.DB) error {
			existing := make([]dal.ChatChannel, 0)
			if err := tx.WithContext(c.Request().Context()).Where("channel_key = ?", channelKey).Find(&existing).Error; err != nil {
				return err
			}
			if command == "create" && len(existing) > 0 {
				return errChatChannelAlreadyExists
			}
			if (command == "edit" || command == "attach") && len(existing) == 0 {
				return errChatChannelNotFound
			}

			beforeActiveBindings := make([]uint, 0)
			for _, row := range existing {
				if row.IsActive {
					beforeActiveBindings = append(beforeActiveBindings, row.RealmID)
				}
			}

			target := dal.ChatChannel{}
			targetResult := tx.WithContext(c.Request().Context()).Where("realm_id = ? AND channel_key = ?", realmID, channelKey).Limit(1).Find(&target)
			if targetResult.Error != nil {
				return targetResult.Error
			}

			canonical := dal.ChatChannel{}
			if len(existing) > 0 {
				canonical = existing[0]
			}

			switch command {
			case "edit":
				if err := tx.WithContext(c.Request().Context()).Model(&dal.ChatChannel{}).Where("channel_key = ?", channelKey).Updates(map[string]any{
					"display_name":   displayName,
					"subject":        subject,
					"description":    description,
					"managed_by_key": "admin",
				}).Error; err != nil {
					return err
				}

			case "attach":
				if targetResult.RowsAffected == 0 {
					target = dal.ChatChannel{
						RealmID:      realmID,
						ChannelKey:   channelKey,
						DisplayName:  canonical.DisplayName,
						Subject:      canonical.Subject,
						Description:  canonical.Description,
						IsActive:     true,
						ManagedByKey: "admin",
					}
					if err := tx.WithContext(c.Request().Context()).Create(&target).Error; err != nil {
						return err
					}
					created = true
				} else {
					if err := tx.WithContext(c.Request().Context()).Model(&dal.ChatChannel{}).Where("id = ?", target.ID).Updates(map[string]any{
						"is_active":      true,
						"managed_by_key": "admin",
					}).Error; err != nil {
						return err
					}
				}

			default:
				if targetResult.RowsAffected == 0 {
					target = dal.ChatChannel{
						RealmID:      realmID,
						ChannelKey:   channelKey,
						DisplayName:  displayName,
						Subject:      subject,
						Description:  description,
						IsActive:     true,
						ManagedByKey: "admin",
					}
					if err := tx.WithContext(c.Request().Context()).Create(&target).Error; err != nil {
						return err
					}
					created = true
				} else {
					if err := tx.WithContext(c.Request().Context()).Model(&dal.ChatChannel{}).Where("id = ?", target.ID).Updates(map[string]any{
						"is_active":      true,
						"managed_by_key": "admin",
					}).Error; err != nil {
						return err
					}
				}

				if err := tx.WithContext(c.Request().Context()).Model(&dal.ChatChannel{}).Where("channel_key = ?", channelKey).Updates(map[string]any{
					"display_name":   displayName,
					"subject":        subject,
					"description":    description,
					"managed_by_key": "admin",
				}).Error; err != nil {
					return err
				}
			}

			afterRows := make([]dal.ChatChannel, 0)
			if err := tx.WithContext(c.Request().Context()).Where("channel_key = ?", channelKey).Find(&afterRows).Error; err != nil {
				return err
			}
			afterActiveBindings := make([]uint, 0)
			for _, row := range afterRows {
				if row.IsActive {
					afterActiveBindings = append(afterActiveBindings, row.RealmID)
				}
			}

			before := map[string]any{
				"bindingExists":       targetResult.RowsAffected > 0,
				"metadataScope":       "channelKey",
				"activeBindingRealms": beforeActiveBindings,
			}
			after := map[string]any{
				"realmId":             realmID,
				"key":                 channelKey,
				"name":                displayName,
				"subject":             subject,
				"description":         description,
				"metadataScope":       "channelKey",
				"activeBindingRealms": afterActiveBindings,
				"bindingActive":       true,
			}

			auditAction := "chat_channel_create"
			switch command {
			case "create":
				auditAction = "chat_channel_create"
			case "edit":
				auditAction = "chat_channel_edit"
			case "attach":
				auditAction = "chat_channel_attach"
			}

			return appendAdminAudit(tx, actor.AccountID, auditAction, reasonCode, note, before, after, tick, realmID)
		})
		if err != nil {
			if errors.Is(err, errChatChannelAlreadyExists) {
				return echo.NewHTTPError(http.StatusConflict, "channel key already exists; use edit or attach")
			}
			if errors.Is(err, errChatChannelNotFound) {
				return echo.NewHTTPError(http.StatusNotFound, "channel key not found; create it first")
			}
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to create channel")
		}

		return respondSuccess(c, http.StatusOK, "Chat channel command applied.", map[string]any{
			"command":       command,
			"scope":         bindingScope,
			"scopeKey":      bindingScopeKey,
			"realmId":       realmID,
			"key":           channelKey,
			"name":          displayName,
			"subject":       subject,
			"metadataScope": "channelKey",
			"created":       created,
		})
	}
}

func makeChatChannelRemoveHandler(database *gorm.DB) echo.HandlerFunc {
	return func(c echo.Context) error {
		key, err := normalizeChatChannelKey(c.Param("key"))
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
		if key == "global" {
			return echo.NewHTTPError(http.StatusConflict, "global channel cannot be removed")
		}

		realmID, err := parseOptionalUintQuery(c.QueryParam("realmId"), "realmId")
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
		if realmID == 0 {
			realmID = 1
		}

		bindingScope, err := parseChatBindingScope(c.QueryParam("scope"))
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
		bindingScopeKey := chatBindingScopeKey(bindingScope, realmID)

		actor, ok := serverAuth.ActorFromContext(c.Request().Context())
		if !ok {
			return echo.NewHTTPError(http.StatusUnauthorized, "missing actor context")
		}

		reasonCode, note, err := validateReasonAndNote("chat_channel_remove", "")
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}

		tick, err := adminAuditTick(c.Request().Context(), database, realmID)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load world tick")
		}

		err = database.WithContext(c.Request().Context()).Transaction(func(tx *gorm.DB) error {
			channel := dal.ChatChannel{}
			result := tx.WithContext(c.Request().Context()).Where("realm_id = ? AND channel_key = ?", realmID, key).Limit(1).Find(&channel)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected == 0 {
				return gorm.ErrRecordNotFound
			}

			before := map[string]any{"active": channel.IsActive}
			after := map[string]any{"active": false}

			if err := tx.WithContext(c.Request().Context()).Model(&dal.ChatChannel{}).Where("id = ?", channel.ID).Update("is_active", false).Error; err != nil {
				return err
			}

			return appendAdminAudit(tx, actor.AccountID, "chat_channel_remove", reasonCode, note, before, after, tick, realmID)
		})
		if err != nil {
			if err == gorm.ErrRecordNotFound {
				return echo.NewHTTPError(http.StatusNotFound, "channel not found")
			}
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to remove channel")
		}

		return respondSuccess(c, http.StatusOK, "Chat channel removed.", map[string]any{"scope": bindingScope, "scopeKey": bindingScopeKey, "realmId": realmID, "key": key})
	}
}

func makeChatChannelFlushHandler(database *gorm.DB) echo.HandlerFunc {
	return func(c echo.Context) error {
		key, err := normalizeChatChannelKey(c.Param("key"))
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}

		var req chatChannelActionRequest
		if err := requestbind.JSON(c, &req, "invalid flush payload"); err != nil {
			return err
		}

		bindingScope, err := parseChatBindingScope(req.Scope)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}

		realmID := req.RealmID
		if realmID == 0 {
			realmID = 1
		}
		bindingScopeKey := chatBindingScopeKey(bindingScope, realmID)

		reasonCode, note, err := validateReasonAndNote(req.ReasonCode, req.Note)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}

		actor, ok := serverAuth.ActorFromContext(c.Request().Context())
		if !ok {
			return echo.NewHTTPError(http.StatusUnauthorized, "missing actor context")
		}

		tick, err := adminAuditTick(c.Request().Context(), database, realmID)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load world tick")
		}

		deleted := int64(0)
		err = database.WithContext(c.Request().Context()).Transaction(func(tx *gorm.DB) error {
			if err := ensureActiveChatBinding(c.Request().Context(), tx, realmID, key); err != nil {
				return err
			}

			eventTypes := []string{
				chatEventPrefixPlayer + key,
				chatEventPrefixMod + key,
				chatEventPrefixAdmin + key,
				chatEventPrefixSystem + key,
			}
			result := tx.WithContext(c.Request().Context()).Where("realm_id = ? AND event_type IN ?", realmID, eventTypes).Delete(&dal.WorldEvent{})
			if result.Error != nil {
				return result.Error
			}
			deleted = result.RowsAffected

			return appendAdminAudit(tx, actor.AccountID, "chat_channel_flush", reasonCode, note, map[string]any{"channel": key}, map[string]any{"deleted": deleted}, tick, realmID)
		})
		if err != nil {
			if err == gorm.ErrRecordNotFound {
				return echo.NewHTTPError(http.StatusNotFound, "channel binding not found for this realm")
			}
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to flush channel")
		}

		return respondSuccess(c, http.StatusOK, "Chat channel flushed.", map[string]any{"scope": bindingScope, "scopeKey": bindingScopeKey, "realmId": realmID, "key": key, "deleted": deleted})
	}
}

func makeChatChannelModerationHandler(database *gorm.DB) echo.HandlerFunc {
	return func(c echo.Context) error {
		key, err := normalizeChatChannelKey(c.Param("key"))
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}

		var req chatChannelModerationRequest
		if err := requestbind.JSON(c, &req, "invalid moderation payload"); err != nil {
			return err
		}

		bindingScope, err := parseChatBindingScope(req.Scope)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}

		realmID := req.RealmID
		if realmID == 0 {
			realmID = 1
		}
		bindingScopeKey := chatBindingScopeKey(bindingScope, realmID)
		if req.AccountID == 0 {
			return echo.NewHTTPError(http.StatusBadRequest, "accountId is required")
		}

		action := strings.ToLower(strings.TrimSpace(req.Action))
		if action != "ban" && action != "unban" && action != "kick" {
			return echo.NewHTTPError(http.StatusBadRequest, "action must be ban, unban, or kick")
		}

		reasonCode, note, err := validateReasonAndNote(req.ReasonCode, req.Note)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}

		actor, ok := serverAuth.ActorFromContext(c.Request().Context())
		if !ok {
			return echo.NewHTTPError(http.StatusUnauthorized, "missing actor context")
		}

		tick, err := adminAuditTick(c.Request().Context(), database, realmID)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load world tick")
		}

		var expiresAt *time.Time
		if action == "kick" {
			durationMinutes := req.DurationMinutes
			if durationMinutes <= 0 {
				durationMinutes = 15
			}
			if durationMinutes > 7*24*60 {
				durationMinutes = 7 * 24 * 60
			}
			expires := time.Now().UTC().Add(time.Duration(durationMinutes) * time.Minute)
			expiresAt = &expires
		}

		err = database.WithContext(c.Request().Context()).Transaction(func(tx *gorm.DB) error {
			if err := ensureActiveChatBinding(c.Request().Context(), tx, realmID, key); err != nil {
				return err
			}

			before := map[string]any{"action": action, "accountId": req.AccountID}
			after := map[string]any{"action": action, "accountId": req.AccountID}

			switch action {
			case "unban":
				if err := tx.WithContext(c.Request().Context()).Model(&dal.ChatChannelModeration{}).
					Where("realm_id = ? AND channel_key = ? AND account_id = ? AND active = ?", realmID, key, req.AccountID, true).
					Update("active", false).Error; err != nil {
					return err
				}
			default:
				record := dal.ChatChannelModeration{
					RealmID:     realmID,
					ChannelKey:  key,
					AccountID:   req.AccountID,
					ActionKey:   action,
					Active:      true,
					ExpiresAt:   expiresAt,
					ReasonCode:  reasonCode,
					Note:        note,
					CreatedByID: actor.AccountID,
				}
				if err := tx.WithContext(c.Request().Context()).Create(&record).Error; err != nil {
					return err
				}
				if expiresAt != nil {
					after["expiresAt"] = expiresAt.UTC().Format(time.RFC3339)
				}
			}

			return appendAdminAudit(tx, actor.AccountID, "chat_channel_moderation", reasonCode, note, before, after, tick, realmID)
		})
		if err != nil {
			if err == gorm.ErrRecordNotFound {
				return echo.NewHTTPError(http.StatusNotFound, "channel binding not found for this realm")
			}
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to apply chat moderation")
		}

		response := map[string]any{"scope": bindingScope, "scopeKey": bindingScopeKey, "realmId": realmID, "key": key, "accountId": req.AccountID, "action": action}
		if expiresAt != nil {
			response["expiresAt"] = expiresAt.UTC().Format(time.RFC3339)
		}
		return respondSuccess(c, http.StatusOK, "Chat moderation applied.", response)
	}
}

func makeChatSystemMessageHandler(database *gorm.DB) echo.HandlerFunc {
	return func(c echo.Context) error {
		key, err := normalizeChatChannelKey(c.Param("key"))
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}

		var req chatSystemMessageRequest
		if err := requestbind.JSON(c, &req, "invalid system message payload"); err != nil {
			return err
		}

		bindingScope, err := parseChatBindingScope(req.Scope)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}

		realmID := req.RealmID
		if realmID == 0 {
			realmID = 1
		}
		bindingScopeKey := chatBindingScopeKey(bindingScope, realmID)

		reasonCode, note, err := validateReasonAndNote(req.ReasonCode, req.Note)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}

		message := strings.TrimSpace(req.Message)
		if len(message) == 0 || len(message) > 280 {
			return echo.NewHTTPError(http.StatusBadRequest, "message must be between 1 and 280 characters")
		}

		actor, ok := serverAuth.ActorFromContext(c.Request().Context())
		if !ok {
			return echo.NewHTTPError(http.StatusUnauthorized, "missing actor context")
		}

		tick, err := gameplay.CurrentWorldTickForRealm(c.Request().Context(), database, realmID)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load world tick")
		}

		event := dal.WorldEvent{
			RealmID:    realmID,
			Tick:       tick,
			EventType:  chatEventPrefixSystem + key,
			Message:    message,
			Visibility: "public",
			Source:     "System",
		}

		err = database.WithContext(c.Request().Context()).Transaction(func(tx *gorm.DB) error {
			if err := ensureActiveChatBinding(c.Request().Context(), tx, realmID, key); err != nil {
				return err
			}

			if err := tx.WithContext(c.Request().Context()).Create(&event).Error; err != nil {
				return err
			}

			return appendAdminAudit(tx, actor.AccountID, "chat_system_message", reasonCode, note, map[string]any{"channel": key}, map[string]any{"message": message}, tick, realmID)
		})
		if err != nil {
			if err == gorm.ErrRecordNotFound {
				return echo.NewHTTPError(http.StatusNotFound, "channel binding not found for this realm")
			}
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to publish system message")
		}

		return respondSuccess(c, http.StatusCreated, "System message published.", map[string]any{"scope": bindingScope, "scopeKey": bindingScopeKey, "realmId": realmID, "channel": key, "tick": tick, "id": event.ID})
	}
}

func makeChatWordlistListHandler(database *gorm.DB) echo.HandlerFunc {
	return func(c echo.Context) error {
		rows := make([]dal.ChatChannelWordRule, 0)
		if err := database.WithContext(c.Request().Context()).
			Where("is_active = ?", true).
			Order("id DESC").
			Find(&rows).Error; err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load wordlist")
		}

		rules := make([]map[string]any, 0, len(rows))
		for _, row := range rows {
			rules = append(rules, map[string]any{
				"id":         row.ID,
				"term":       row.Term,
				"matchMode":  row.MatchMode,
				"reasonCode": row.ReasonCode,
				"updatedAt":  row.UpdatedAt,
			})
		}

		return respondSuccess(c, http.StatusOK, "Chat wordlist loaded.", map[string]any{"policyScope": chatPolicyScopeGlobal, "policyScopeKey": chatPolicyScopeKey, "scope": chatPolicyScopeAllText, "rules": rules})
	}
}

func makeChatChannelListHandler(database *gorm.DB) echo.HandlerFunc {
	return func(c echo.Context) error {
		realmID, err := parseOptionalUintQuery(c.QueryParam("realmId"), "realmId")
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}

		includeInactive, err := parseOptionalBoolQuery(c.QueryParam("includeInactive"), "includeInactive")
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}

		query := database.WithContext(c.Request().Context())
		if !includeInactive {
			query = query.Where("is_active = ?", true)
		}
		if realmID != 0 {
			query = query.Where("realm_id = ?", realmID)
		}

		rows := make([]dal.ChatChannel, 0)
		if err := query.Order("realm_id ASC, channel_key ASC").Find(&rows).Error; err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load chat channels")
		}

		channels := make([]map[string]any, 0, len(rows))
		for _, row := range rows {
			channels = append(channels, map[string]any{
				"scope":       chatBindingScopeRealm,
				"scopeKey":    chatBindingScopeKey(chatBindingScopeRealm, row.RealmID),
				"realmId":     row.RealmID,
				"key":         row.ChannelKey,
				"name":        row.DisplayName,
				"subject":     row.Subject,
				"description": row.Description,
				"active":      row.IsActive,
			})
		}

		response := map[string]any{
			"scope":           chatBindingScopeRealm,
			"includeInactive": includeInactive,
			"channels":        channels,
		}
		if realmID != 0 {
			response["realmId"] = realmID
		}

		return respondSuccess(c, http.StatusOK, "Chat channels loaded.", response)
	}
}

func makeChatWordlistAddHandler(database *gorm.DB) echo.HandlerFunc {
	return func(c echo.Context) error {
		actor, ok := serverAuth.ActorFromContext(c.Request().Context())
		if !ok {
			return echo.NewHTTPError(http.StatusUnauthorized, "missing actor context")
		}

		var req chatWordlistRuleRequest
		if err := requestbind.JSON(c, &req, "invalid wordlist payload"); err != nil {
			return err
		}

		policyScope, err := parseChatPolicyScope(req.Scope)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}

		term := strings.TrimSpace(strings.ToLower(req.Term))
		if len(term) < 2 || len(term) > 128 {
			return echo.NewHTTPError(http.StatusBadRequest, "term must be between 2 and 128 characters")
		}

		matchMode := strings.TrimSpace(strings.ToLower(req.MatchMode))
		if matchMode == "" {
			matchMode = "contains"
		}
		if matchMode != "contains" {
			return echo.NewHTTPError(http.StatusBadRequest, "matchMode must be contains")
		}

		reasonCode, note, err := validateReasonAndNote(req.ReasonCode, req.Note)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}

		tick, err := adminAuditTick(c.Request().Context(), database, 1)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load world tick")
		}

		created := false
		var rule dal.ChatChannelWordRule
		err = database.WithContext(c.Request().Context()).Transaction(func(tx *gorm.DB) error {
			result := tx.WithContext(c.Request().Context()).
				Where("term = ? AND match_mode = ?", term, matchMode).
				Limit(1).
				Find(&rule)
			if result.Error != nil {
				return result.Error
			}

			before := map[string]any{"exists": result.RowsAffected > 0}
			after := map[string]any{"scope": "all_realms_all_channels", "term": term, "matchMode": matchMode, "active": true}

			if result.RowsAffected == 0 {
				rule = dal.ChatChannelWordRule{
					Term:        term,
					MatchMode:   matchMode,
					IsActive:    true,
					ReasonCode:  reasonCode,
					Note:        note,
					CreatedByID: actor.AccountID,
				}
				if err := tx.WithContext(c.Request().Context()).Create(&rule).Error; err != nil {
					return err
				}
				created = true
			} else {
				if err := tx.WithContext(c.Request().Context()).Model(&dal.ChatChannelWordRule{}).Where("id = ?", rule.ID).Updates(map[string]any{
					"is_active":   true,
					"reason_code": reasonCode,
					"note":        note,
				}).Error; err != nil {
					return err
				}
			}

			return appendAdminAudit(tx, actor.AccountID, "chat_wordlist_add", reasonCode, note, before, after, tick, 1)
		})
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to add wordlist rule")
		}

		return respondSuccess(c, http.StatusOK, "Chat wordlist rule saved.", map[string]any{"policyScope": policyScope, "policyScopeKey": chatPolicyScopeKey, "scope": chatPolicyScopeAllText, "id": rule.ID, "term": term, "matchMode": matchMode, "created": created})
	}
}

func makeChatWordlistRemoveHandler(database *gorm.DB) echo.HandlerFunc {
	return func(c echo.Context) error {
		ruleID, err := parseRuleIDPathParam(c.Param("ruleId"))
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}

		actor, ok := serverAuth.ActorFromContext(c.Request().Context())
		if !ok {
			return echo.NewHTTPError(http.StatusUnauthorized, "missing actor context")
		}

		reasonCode := "chat_wordlist_remove"
		note := ""
		tick, err := adminAuditTick(c.Request().Context(), database, 1)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load world tick")
		}

		err = database.WithContext(c.Request().Context()).Transaction(func(tx *gorm.DB) error {
			rule := dal.ChatChannelWordRule{}
			result := tx.WithContext(c.Request().Context()).
				Where("id = ?", ruleID).
				Limit(1).
				Find(&rule)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected == 0 {
				return gorm.ErrRecordNotFound
			}

			before := map[string]any{"active": rule.IsActive, "term": rule.Term, "matchMode": rule.MatchMode}
			after := map[string]any{"active": false}

			if err := tx.WithContext(c.Request().Context()).Model(&dal.ChatChannelWordRule{}).Where("id = ?", ruleID).Update("is_active", false).Error; err != nil {
				return err
			}

			return appendAdminAudit(tx, actor.AccountID, "chat_wordlist_remove", reasonCode, note, before, after, tick, 1)
		})
		if err != nil {
			if err == gorm.ErrRecordNotFound {
				return echo.NewHTTPError(http.StatusNotFound, "wordlist rule not found")
			}
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to remove wordlist rule")
		}

		return respondSuccess(c, http.StatusOK, "Chat wordlist rule removed.", map[string]any{"policyScope": chatPolicyScopeGlobal, "policyScopeKey": chatPolicyScopeKey, "scope": chatPolicyScopeAllText, "ruleId": ruleID})
	}
}

func chatBindingScopeKey(scope string, realmID uint) string {
	if scope == chatBindingScopeRealm {
		if realmID == 0 {
			realmID = 1
		}
		return fmt.Sprintf("realm:%d", realmID)
	}
	return scope
}

func parseChatBindingScope(raw string) (string, error) {
	scope := strings.ToLower(strings.TrimSpace(raw))
	if scope == "" {
		return chatBindingScopeRealm, nil
	}
	if scope != chatBindingScopeRealm {
		return "", fmt.Errorf("scope must be realm")
	}
	return scope, nil
}

func parseChatPolicyScope(raw string) (string, error) {
	scope := strings.ToLower(strings.TrimSpace(raw))
	if scope == "" {
		return chatPolicyScopeGlobal, nil
	}
	if scope != chatPolicyScopeGlobal {
		return "", fmt.Errorf("scope must be global")
	}
	return scope, nil
}

func normalizeChatChannelKey(raw string) (string, error) {
	channel := strings.ToLower(strings.TrimSpace(raw))
	if channel == "" {
		return "", fmt.Errorf("channel key is required")
	}
	if len(channel) > 32 {
		return "", fmt.Errorf("channel key must be 32 characters or less")
	}
	for _, r := range channel {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			continue
		}
		return "", fmt.Errorf("channel key may only contain letters, numbers, underscores, or hyphens")
	}
	return channel, nil
}

func parseRealmIDWithDefault(raw string) uint {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 1
	}
	value, err := strconv.ParseUint(trimmed, 10, 64)
	if err != nil || value == 0 {
		return 1
	}
	return uint(value)
}

func parseChatChannelCommand(raw string) (string, error) {
	command := strings.ToLower(strings.TrimSpace(raw))
	if command == "" {
		return "", fmt.Errorf("command is required")
	}
	switch command {
	case "create", "edit", "attach":
		return command, nil
	default:
		return "", fmt.Errorf("command must be create, edit, or attach")
	}
}

func parseRuleIDPathParam(raw string) (uint, error) {
	trimmed := strings.TrimSpace(raw)
	value, err := strconv.ParseUint(trimmed, 10, 64)
	if err != nil || value == 0 {
		return 0, fmt.Errorf("ruleId must be a positive integer")
	}

	return uint(value), nil
}

func ensureActiveChatBinding(ctx context.Context, tx *gorm.DB, realmID uint, key string) error {
	channel := dal.ChatChannel{}
	result := tx.WithContext(ctx).
		Where("realm_id = ? AND channel_key = ? AND is_active = ?", realmID, key, true).
		Limit(1).
		Find(&channel)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}
