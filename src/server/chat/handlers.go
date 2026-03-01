package chat

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"unicode"

	"github.com/asciifaceman/lived/pkg/config"
	"github.com/asciifaceman/lived/pkg/dal"
	"github.com/asciifaceman/lived/pkg/idempotency"
	"github.com/asciifaceman/lived/pkg/ratelimit"
	"github.com/asciifaceman/lived/src/gameplay"
	serverAuth "github.com/asciifaceman/lived/src/server/auth"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

const (
	statusSuccess = "success"
	statusActive  = "active"
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
	ID          uint   `json:"id"`
	RealmID     uint   `json:"realmId"`
	Channel     string `json:"channel"`
	Tick        int64  `json:"tick"`
	Day         int64  `json:"day"`
	MinuteOfDay int64  `json:"minuteOfDay"`
	Clock       string `json:"clock"`
	Author      string `json:"author"`
	Message     string `json:"message"`
}

func RegisterRoutes(group *echo.Group, database *gorm.DB, cfg config.Config) {
	group.GET("/channels", makeChannelsHandler())
	group.GET("/messages", makeGetMessagesHandler(database))

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

func makeChannelsHandler() echo.HandlerFunc {
	return func(c echo.Context) error {
		channels := []map[string]any{
			{"key": "global", "name": "Global", "description": "Realm-wide chat channel."},
		}
		return respondSuccess(c, http.StatusOK, "Chat channels loaded.", map[string]any{"channels": channels})
	}
}

func makeGetMessagesHandler(database *gorm.DB) echo.HandlerFunc {
	return func(c echo.Context) error {
		realmID, err := parseRealmID(c.QueryParam("realmId"))
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}

		channel, err := normalizeChannel(c.QueryParam("channel"))
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
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

		eventType := eventTypeForChannel(channel)
		rows := make([]dal.WorldEvent, 0)
		if err := database.WithContext(c.Request().Context()).
			Where("realm_id = ? AND visibility = ? AND event_type = ?", realmID, "public", eventType).
			Order("id DESC").
			Limit(limit).
			Find(&rows).Error; err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load chat messages")
		}

		messages := make([]chatMessage, 0, len(rows))
		for i := len(rows) - 1; i >= 0; i-- {
			row := rows[i]
			minuteOfDay := positiveMinuteOfDay(row.Tick)
			messages = append(messages, chatMessage{
				ID:          row.ID,
				RealmID:     realmID,
				Channel:     channel,
				Tick:        row.Tick,
				Day:         row.Tick / (24 * 60),
				MinuteOfDay: minuteOfDay,
				Clock:       clockLabel(minuteOfDay),
				Author:      row.Source,
				Message:     row.Message,
			})
		}

		return respondSuccess(c, http.StatusOK, "Chat messages loaded.", map[string]any{
			"realmId":  realmID,
			"channel":  channel,
			"limit":    limit,
			"messages": messages,
		})
	}
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
		realmID := uint(1)

		if cfg.MMOAuthEnabled {
			actor, ok := serverAuth.ActorFromContext(c.Request().Context())
			if !ok {
				return echo.NewHTTPError(http.StatusUnauthorized, "missing actor context")
			}

			characterID := uint(0)
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
		} else {
			player, lookupErr := loadPrimaryPlayer(c.Request().Context(), database)
			if lookupErr != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "failed to load primary player")
			}
			if player != nil {
				author = strings.TrimSpace(player.Name)
			}
		}

		if author == "" {
			author = "Player"
		}

		tick, err := gameplay.CurrentWorldTickForRealm(c.Request().Context(), database, realmID)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load world tick")
		}

		entry := dal.WorldEvent{
			RealmID:    realmID,
			Tick:       tick,
			EventType:  eventTypeForChannel(channel),
			Message:    message,
			Visibility: "public",
			Source:     author,
		}
		if err := database.WithContext(c.Request().Context()).Create(&entry).Error; err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to post chat message")
		}

		minuteOfDay := positiveMinuteOfDay(tick)
		return respondSuccess(c, http.StatusCreated, "Chat message posted.", chatMessage{
			ID:          entry.ID,
			RealmID:     realmID,
			Channel:     channel,
			Tick:        tick,
			Day:         tick / (24 * 60),
			MinuteOfDay: minuteOfDay,
			Clock:       clockLabel(minuteOfDay),
			Author:      author,
			Message:     message,
		})
	}
}

func eventTypeForChannel(channel string) string {
	return "chat_message:" + channel
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
	character := &dal.Character{}
	query := database.WithContext(ctx).Where("account_id = ? AND status = ?", accountID, statusActive)
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

func respondSuccess(c echo.Context, code int, message string, data any) error {
	requestID := c.Response().Header().Get(echo.HeaderXRequestID)
	return c.JSON(code, apiResponse{
		Status:    statusSuccess,
		Message:   message,
		RequestID: requestID,
		Data:      data,
	})
}
