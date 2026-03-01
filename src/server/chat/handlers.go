package chat

import (
	"context"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/asciifaceman/lived/pkg/config"
	"github.com/asciifaceman/lived/pkg/dal"
	"github.com/asciifaceman/lived/pkg/idempotency"
	"github.com/asciifaceman/lived/pkg/ratelimit"
	"github.com/asciifaceman/lived/src/gameplay"
	serverAuth "github.com/asciifaceman/lived/src/server/auth"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	statusSuccess = "success"
	statusActive  = "active"

	messageClassPlayer    = "player"
	messageClassModerator = "moderator"
	messageClassAdmin     = "admin"
	messageClassSystem    = "system"

	eventPrefixPlayer = "chat_message:"
	eventPrefixMod    = "chat_message_mod:"
	eventPrefixAdmin  = "chat_message_admin:"
	eventPrefixSystem = "chat_message_system:"
)

type apiResponse struct {
	Status    string `json:"status"`
	Message   string `json:"message"`
	RequestID string `json:"requestId,omitempty"`
	Data      any    `json:"data,omitempty"`
}

type postMessageRequest struct {
	Message string `json:"message"`
	Channel string `json:"channel,omitempty"`
}

type chatMessage struct {
	ID           uint     `json:"id"`
	RealmID      uint     `json:"realmId"`
	Channel      string   `json:"channel"`
	MessageClass string   `json:"messageClass"`
	AuthorRole   string   `json:"authorRole"`
	AuthorBadges []string `json:"authorBadges"`
	Censored     bool     `json:"censored"`
	CensorHits   int64    `json:"censorHits,omitempty"`
	Tick         int64    `json:"tick"`
	Day          int64    `json:"day"`
	MinuteOfDay  int64    `json:"minuteOfDay"`
	Clock        string   `json:"clock"`
	Author       string   `json:"author"`
	Message      string   `json:"message"`
}

func RegisterRoutes(group *echo.Group, database *gorm.DB, cfg config.Config) {
	if cfg.MMOAuthEnabled {
		group.GET("/channels", makeChannelsHandler(database, cfg), serverAuth.RequireAuth(database, cfg))
		group.GET("/messages", makeGetMessagesHandler(database, cfg), serverAuth.RequireAuth(database, cfg))
	} else {
		group.GET("/channels", makeChannelsHandler(database, cfg))
		group.GET("/messages", makeGetMessagesHandler(database, cfg))
	}

	postMessageHandler := makePostMessageHandler(database, cfg)
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
	if cfg.MMOAuthEnabled {
		if cfg.RateLimitEnabled {
			if cfg.IdempotencyEnabled {
				group.POST("/messages", postMessageHandler, serverAuth.RequireAuth(database, cfg), idempotencyMW, limiter.Middleware("chat_post", cfg.RateLimitChatMax))
			} else {
				group.POST("/messages", postMessageHandler, serverAuth.RequireAuth(database, cfg), limiter.Middleware("chat_post", cfg.RateLimitChatMax))
			}
		} else {
			if cfg.IdempotencyEnabled {
				group.POST("/messages", postMessageHandler, serverAuth.RequireAuth(database, cfg), idempotencyMW)
			} else {
				group.POST("/messages", postMessageHandler, serverAuth.RequireAuth(database, cfg))
			}
		}
	} else {
		if cfg.RateLimitEnabled {
			if cfg.IdempotencyEnabled {
				group.POST("/messages", postMessageHandler, idempotencyMW, limiter.Middleware("chat_post", cfg.RateLimitChatMax))
			} else {
				group.POST("/messages", postMessageHandler, limiter.Middleware("chat_post", cfg.RateLimitChatMax))
			}
		} else {
			if cfg.IdempotencyEnabled {
				group.POST("/messages", postMessageHandler, idempotencyMW)
			} else {
				group.POST("/messages", postMessageHandler)
			}
		}
	}
}

func makeChannelsHandler(database *gorm.DB, cfg config.Config) echo.HandlerFunc {
	return func(c echo.Context) error {
		realmID, err := resolveRealmIDForChatRead(c, database, cfg)
		if err != nil {
			return err
		}

		if err := ensureDefaultChatChannel(c.Request().Context(), database, realmID); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to ensure default channel")
		}

		rows := make([]dal.ChatChannel, 0)
		if err := database.WithContext(c.Request().Context()).
			Where("realm_id = ? AND is_active = ?", realmID, true).
			Order("channel_key ASC").
			Find(&rows).Error; err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load chat channels")
		}

		channels := make([]map[string]any, 0, len(rows))
		scope, scopeKey := realmChannelBinding(realmID)
		for _, row := range rows {
			channels = append(channels, map[string]any{
				"key":         row.ChannelKey,
				"name":        row.DisplayName,
				"subject":     row.Subject,
				"description": row.Description,
				"scope":       scope,
				"scopeKey":    scopeKey,
			})
		}
		return respondSuccess(c, http.StatusOK, "Chat channels loaded.", map[string]any{"realmId": realmID, "channels": channels})
	}
}

func makeGetMessagesHandler(database *gorm.DB, cfg config.Config) echo.HandlerFunc {
	return func(c echo.Context) error {
		realmID, err := resolveRealmIDForChatRead(c, database, cfg)
		if err != nil {
			return err
		}

		channel, err := normalizeChannel(c.QueryParam("channel"))
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}

		if err := ensureDefaultChatChannel(c.Request().Context(), database, realmID); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to ensure default channel")
		}
		if err := ensureChannelActive(c.Request().Context(), database, realmID, channel); err != nil {
			return err
		}

		limit := 100
		if rawLimit := strings.TrimSpace(c.QueryParam("limit")); rawLimit != "" {
			parsedLimit, parseErr := strconv.Atoi(rawLimit)
			if parseErr != nil || parsedLimit <= 0 {
				return echo.NewHTTPError(http.StatusBadRequest, "limit must be a positive integer")
			}
			limit = parsedLimit
		}
		if limit > 200 {
			limit = 200
		}

		eventTypes := eventTypesForChannel(channel)
		rows := make([]dal.WorldEvent, 0)
		if err := database.WithContext(c.Request().Context()).
			Where("realm_id = ? AND visibility = ? AND event_type IN ?", realmID, "public", eventTypes).
			Order("id DESC").
			Limit(limit).
			Find(&rows).Error; err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load chat messages")
		}

		messages := make([]chatMessage, 0, len(rows))
		for i := len(rows) - 1; i >= 0; i-- {
			row := rows[i]
			minuteOfDay := positiveMinuteOfDay(row.Tick)
			messageClass := messageClassFromEventType(row.EventType, channel)
			messages = append(messages, chatMessage{
				ID:           row.ID,
				RealmID:      realmID,
				Channel:      channel,
				MessageClass: messageClass,
				AuthorRole:   authorRoleFromMessageClass(messageClass),
				AuthorBadges: authorBadgesFromMessageClass(messageClass),
				Censored:     false,
				CensorHits:   0,
				Tick:         row.Tick,
				Day:          row.Tick / (24 * 60),
				MinuteOfDay:  minuteOfDay,
				Clock:        clockLabel(minuteOfDay),
				Author:       row.Source,
				Message:      row.Message,
			})
		}

		scope, scopeKey := realmChannelBinding(realmID)
		return respondSuccess(c, http.StatusOK, "Chat messages loaded.", map[string]any{
			"realmId":  realmID,
			"channel":  channel,
			"scope":    scope,
			"scopeKey": scopeKey,
			"limit":    limit,
			"messages": messages,
		})
	}
}

func resolveRealmIDForChatRead(c echo.Context, database *gorm.DB, cfg config.Config) (uint, error) {
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

func makePostMessageHandler(database *gorm.DB, cfg config.Config) echo.HandlerFunc {
	return func(c echo.Context) error {
		var req postMessageRequest
		if err := c.Bind(&req); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid chat message payload")
		}

		message := strings.TrimSpace(req.Message)
		if message == "" {
			return echo.NewHTTPError(http.StatusBadRequest, "message is required")
		}
		if len(message) > 280 {
			return echo.NewHTTPError(http.StatusBadRequest, "message exceeds 280 characters")
		}

		channel, err := normalizeChannel(req.Channel)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}

		author := "Player"
		messageClass := messageClassPlayer
		realmID := uint(1)
		actorAccountID := uint(0)
		characterID := uint(0)

		if cfg.MMOAuthEnabled {
			actor, ok := serverAuth.ActorFromContext(c.Request().Context())
			if !ok {
				return echo.NewHTTPError(http.StatusUnauthorized, "missing actor context")
			}

			characterID = uint(0)
			if rawCharacterID := strings.TrimSpace(c.QueryParam("characterId")); rawCharacterID != "" {
				parsedID, parseErr := strconv.ParseUint(rawCharacterID, 10, 64)
				if parseErr != nil || parsedID == 0 {
					return echo.NewHTTPError(http.StatusBadRequest, "characterId must be a positive integer")
				}
				characterID = uint(parsedID)
			}

			character, lookupErr := loadActorCharacter(c.Request().Context(), database, actor.AccountID, characterID)
			if lookupErr != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "failed to load character")
			}
			if character == nil {
				return echo.NewHTTPError(http.StatusBadRequest, "complete onboarding first")
			}
			author = strings.TrimSpace(character.Name)
			realmID = character.RealmID
			actorAccountID = actor.AccountID

			if hasRole(actor.Roles, "admin") {
				messageClass = messageClassAdmin
			} else if hasRole(actor.Roles, "moderator") {
				messageClass = messageClassModerator
			}

			if err := ensureDefaultChatChannel(c.Request().Context(), database, realmID); err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "failed to ensure default channel")
			}
			if err := ensureChannelActive(c.Request().Context(), database, realmID, channel); err != nil {
				return err
			}

			actionKey, expiresAt, restrictionErr := activeChannelRestriction(c.Request().Context(), database, realmID, channel, actor.AccountID)
			if restrictionErr != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "failed to load channel moderation state")
			}
			if actionKey != "" {
				if actionKey == "kick" && expiresAt != nil {
					return echo.NewHTTPError(http.StatusForbidden, "temporarily removed from channel until "+expiresAt.UTC().Format(time.RFC3339))
				}
				return echo.NewHTTPError(http.StatusForbidden, "blocked from channel")
			}
		} else {
			player, lookupErr := loadPrimaryPlayer(c.Request().Context(), database)
			if lookupErr != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "failed to load primary player")
			}
			if player != nil {
				author = strings.TrimSpace(player.Name)
			}

			if err := ensureDefaultChatChannel(c.Request().Context(), database, realmID); err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "failed to ensure default channel")
			}
			if err := ensureChannelActive(c.Request().Context(), database, realmID, channel); err != nil {
				return err
			}
		}

		if author == "" {
			author = "Player"
		}

		originalLength := int64(len(message))
		redactedMessage, censorshipHitCount, matchedRuleCount, censorErr := applyWordlistCensorship(c.Request().Context(), database, message)
		if censorErr != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to evaluate chat wordlist")
		}
		censored := censorshipHitCount > 0
		if censored {
			message = redactedMessage
		}

		tick, err := gameplay.CurrentWorldTickForRealm(c.Request().Context(), database, realmID)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load world tick")
		}

		entry := dal.WorldEvent{
			RealmID:    realmID,
			Tick:       tick,
			EventType:  eventTypeForChannel(channel, messageClass),
			Message:    message,
			Visibility: "public",
			Source:     author,
		}
		if err := database.WithContext(c.Request().Context()).Create(&entry).Error; err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to post chat message")
		}

		if censored {
			trace := dal.ChatMessageCensorshipTrace{
				RealmID:        realmID,
				ChannelKey:     channel,
				AccountID:      actorAccountID,
				CharacterID:    characterID,
				MessageID:      entry.ID,
				MessageClass:   messageClass,
				OriginalLength: originalLength,
				CensoredCount:  censorshipHitCount,
				MatchedRules:   matchedRuleCount,
			}
			if err := database.WithContext(c.Request().Context()).Create(&trace).Error; err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "failed to persist chat censorship trace")
			}
		}

		minuteOfDay := positiveMinuteOfDay(tick)
		statusCode := http.StatusCreated
		statusMessage := "Chat message posted."
		if censored {
			statusCode = http.StatusOK
			statusMessage = "Chat message posted with censorship."
		}

		return respondSuccess(c, statusCode, statusMessage, chatMessage{
			ID:           entry.ID,
			RealmID:      realmID,
			Channel:      channel,
			MessageClass: messageClass,
			AuthorRole:   authorRoleFromMessageClass(messageClass),
			AuthorBadges: authorBadgesFromMessageClass(messageClass),
			Censored:     censored,
			CensorHits:   censorshipHitCount,
			Tick:         tick,
			Day:          tick / (24 * 60),
			MinuteOfDay:  minuteOfDay,
			Clock:        clockLabel(minuteOfDay),
			Author:       author,
			Message:      message,
		})
	}
}

func eventTypeForChannel(channel string, messageClass string) string {
	switch messageClass {
	case messageClassAdmin:
		return eventPrefixAdmin + channel
	case messageClassModerator:
		return eventPrefixMod + channel
	case messageClassSystem:
		return eventPrefixSystem + channel
	default:
		return eventPrefixPlayer + channel
	}
}

func eventTypesForChannel(channel string) []string {
	return []string{
		eventPrefixPlayer + channel,
		eventPrefixMod + channel,
		eventPrefixAdmin + channel,
		eventPrefixSystem + channel,
	}
}

func messageClassFromEventType(eventType string, channel string) string {
	switch eventType {
	case eventPrefixAdmin + channel:
		return messageClassAdmin
	case eventPrefixMod + channel:
		return messageClassModerator
	case eventPrefixSystem + channel:
		return messageClassSystem
	default:
		return messageClassPlayer
	}
}

func authorRoleFromMessageClass(messageClass string) string {
	switch messageClass {
	case messageClassAdmin:
		return "admin"
	case messageClassModerator:
		return "moderator"
	case messageClassSystem:
		return "system"
	default:
		return "player"
	}
}

func authorBadgesFromMessageClass(messageClass string) []string {
	switch messageClass {
	case messageClassAdmin:
		return []string{"admin"}
	case messageClassModerator:
		return []string{"moderator"}
	case messageClassSystem:
		return []string{"system"}
	default:
		return []string{}
	}
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

func normalizeChannel(raw string) (string, error) {
	channel := strings.ToLower(strings.TrimSpace(raw))
	if channel == "" {
		return "global", nil
	}
	if len(channel) > 32 {
		return "", echo.NewHTTPError(http.StatusBadRequest, "channel must be 32 characters or less")
	}
	for _, r := range channel {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' {
			continue
		}
		return "", echo.NewHTTPError(http.StatusBadRequest, "channel may only contain letters, numbers, underscores, or hyphens")
	}

	return channel, nil
}

func positiveMinuteOfDay(tick int64) int64 {
	minute := tick % (24 * 60)
	if minute < 0 {
		minute += 24 * 60
	}
	return minute
}

func clockLabel(minuteOfDay int64) string {
	hours := minuteOfDay / 60
	minutes := minuteOfDay % 60
	return strings.TrimSpace(
		strconv.FormatInt(hours/10, 10) + strconv.FormatInt(hours%10, 10) + ":" +
			strconv.FormatInt(minutes/10, 10) + strconv.FormatInt(minutes%10, 10),
	)
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

func respondSuccess(c echo.Context, code int, message string, data any) error {
	requestID := c.Response().Header().Get(echo.HeaderXRequestID)
	return c.JSON(code, apiResponse{
		Status:    statusSuccess,
		Message:   message,
		RequestID: requestID,
		Data:      data,
	})
}

func realmChannelBinding(realmID uint) (string, string) {
	if realmID == 0 {
		realmID = 1
	}
	return "realm", "realm:" + strconv.FormatUint(uint64(realmID), 10)
}

func ensureDefaultChatChannel(ctx context.Context, database *gorm.DB, realmID uint) error {
	return database.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "realm_id"}, {Name: "channel_key"}},
		DoNothing: true,
	}).Create(&dal.ChatChannel{
		RealmID:      realmID,
		ChannelKey:   "global",
		DisplayName:  "Global",
		Subject:      "General realm discussion",
		Description:  "Realm-wide chat channel.",
		IsActive:     true,
		ManagedByKey: "system",
	}).Error
}

func ensureChannelActive(ctx context.Context, database *gorm.DB, realmID uint, channel string) error {
	entry := dal.ChatChannel{}
	result := database.WithContext(ctx).Where("realm_id = ? AND channel_key = ?", realmID, channel).Limit(1).Find(&entry)
	if result.Error != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to load channel")
	}
	if result.RowsAffected == 0 || !entry.IsActive {
		return echo.NewHTTPError(http.StatusNotFound, "channel not found")
	}

	return nil
}

func activeChannelRestriction(ctx context.Context, database *gorm.DB, realmID uint, channel string, accountID uint) (string, *time.Time, error) {
	now := time.Now().UTC()
	row := dal.ChatChannelModeration{}
	result := database.WithContext(ctx).
		Where("realm_id = ? AND channel_key = ? AND account_id = ? AND active = ? AND (expires_at IS NULL OR expires_at > ?)", realmID, channel, accountID, true, now).
		Order("created_at DESC, id DESC").
		Limit(1).
		Find(&row)
	if result.Error != nil {
		return "", nil, result.Error
	}
	if result.RowsAffected == 0 {
		return "", nil, nil
	}

	return row.ActionKey, row.ExpiresAt, nil
}

func hasRole(roles []string, roleKey string) bool {
	for _, role := range roles {
		if strings.EqualFold(strings.TrimSpace(role), roleKey) {
			return true
		}
	}
	return false
}

func PublishSystemMessage(ctx context.Context, database *gorm.DB, realmID uint, channel string, source string, message string) error {
	if realmID == 0 {
		realmID = 1
	}

	normalizedChannel, err := normalizeChannel(channel)
	if err != nil {
		return err
	}
	if err := ensureDefaultChatChannel(ctx, database, realmID); err != nil {
		return err
	}
	if err := ensureChannelActive(ctx, database, realmID, normalizedChannel); err != nil {
		return err
	}

	trimmedMessage := strings.TrimSpace(message)
	if trimmedMessage == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "message is required")
	}
	if len(trimmedMessage) > 280 {
		return echo.NewHTTPError(http.StatusBadRequest, "message exceeds 280 characters")
	}

	author := strings.TrimSpace(source)
	if author == "" {
		author = "System"
	}

	tick, err := gameplay.CurrentWorldTickForRealm(ctx, database, realmID)
	if err != nil {
		return err
	}

	return database.WithContext(ctx).Create(&dal.WorldEvent{
		RealmID:    realmID,
		Tick:       tick,
		EventType:  eventTypeForChannel(normalizedChannel, messageClassSystem),
		Message:    trimmedMessage,
		Visibility: "public",
		Source:     author,
	}).Error
}

func applyWordlistCensorship(ctx context.Context, database *gorm.DB, message string) (string, int64, int64, error) {
	rules := make([]dal.ChatChannelWordRule, 0)
	if err := database.WithContext(ctx).
		Where("is_active = ?", true).
		Order("id ASC").
		Find(&rules).Error; err != nil {
		return "", 0, 0, err
	}

	redacted := message
	var totalHits int64
	var matchedRules int64
	for _, rule := range rules {
		if strings.TrimSpace(rule.Term) == "" {
			continue
		}
		if strings.ToLower(strings.TrimSpace(rule.MatchMode)) != "contains" {
			continue
		}

		next, hits := censorContains(redacted, rule.Term)
		if hits > 0 {
			matchedRules++
			totalHits += int64(hits)
			redacted = next
		}
	}

	return redacted, totalHits, matchedRules, nil
}

func censorContains(message string, term string) (string, int) {
	trimmedTerm := strings.TrimSpace(term)
	if trimmedTerm == "" {
		return message, 0
	}

	pattern := "(?i)" + regexp.QuoteMeta(trimmedTerm)
	re, err := regexp.Compile(pattern)
	if err != nil {
		return message, 0
	}

	hits := 0
	redacted := re.ReplaceAllStringFunc(message, func(match string) string {
		hits++
		return strings.Repeat("*", len(match))
	})

	return redacted, hits
}
