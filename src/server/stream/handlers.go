package stream

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/asciifaceman/lived/pkg/config"
	"github.com/asciifaceman/lived/pkg/dal"
	"github.com/asciifaceman/lived/src/gameplay"
	serverAuth "github.com/asciifaceman/lived/src/server/auth"
	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

type worldStreamEvent struct {
	Type        string              `json:"type"`
	EventID     string              `json:"eventId"`
	At          string              `json:"at"`
	Tick        int64               `json:"tick"`
	Day         int64               `json:"day"`
	MinuteOfDay int64               `json:"minuteOfDay"`
	Clock       string              `json:"clock"`
	DayPart     string              `json:"dayPart"`
	MarketOpen  bool                `json:"marketOpen"`
	MarketState string              `json:"marketState"`
	Resume      *streamResumeStatus `json:"resume,omitempty"`
	Player      *playerStreamStatus `json:"player,omitempty"`
}

type streamResumeStatus struct {
	RequestedLastEventID string `json:"requestedLastEventId,omitempty"`
	ResolvedEventID      string `json:"resolvedEventId,omitempty"`
	Mode                 string `json:"mode"`
	Reason               string `json:"reason,omitempty"`
	GapTicks             int64  `json:"gapTicks,omitempty"`
}

type streamResumeCursor struct {
	Raw     string
	RealmID uint
	Tick    int64
	Valid   bool
	Reason  string
}

type playerStreamStatus struct {
	Name            string  `json:"name"`
	Coins           int64   `json:"coins"`
	AscensionCount  int64   `json:"ascensionCount"`
	WealthBonusPct  float64 `json:"wealthBonusPct"`
	QueuedOrActive  int     `json:"queuedOrActiveBehaviors"`
	AscensionReady  bool    `json:"ascensionReady"`
	AscensionReason string  `json:"ascensionReason"`
}

type streamViewer struct {
	PlayerID uint
	RealmID  uint
	Name     string
}

func RegisterRoutes(group *echo.Group, database *gorm.DB, cfg config.Config) {
	var limiter *streamConnectionLimiter
	if cfg.MMOAuthEnabled {
		limiter = newStreamConnectionLimiter(cfg.StreamMaxConnsPerAccount, cfg.StreamMaxConnsPerSession)
	}

	handler := makeWorldStreamHandler(database, cfg, limiter)
	if cfg.MMOAuthEnabled {
		group.GET("/world", handler, serverAuth.RequireAuthForStream(database, cfg))
		return
	}
	group.GET("/world", handler)
}

func makeWorldStreamHandler(database *gorm.DB, cfg config.Config, limiter *streamConnectionLimiter) echo.HandlerFunc {
	return func(c echo.Context) error {
		viewer := (*streamViewer)(nil)
		accountID := uint(0)
		sessionID := uint(0)
		if cfg.MMOAuthEnabled {
			actor, ok := serverAuth.ActorFromContext(c.Request().Context())
			if !ok {
				return echo.NewHTTPError(http.StatusUnauthorized, "missing actor context")
			}
			accountID = actor.AccountID
			sessionID = actor.SessionID
			if limiter != nil {
				if acquired := limiter.tryAcquire(accountID, sessionID); !acquired {
					return echo.NewHTTPError(http.StatusTooManyRequests, "stream connection limit reached for this account/session")
				}
				defer limiter.release(accountID, sessionID)
			}

			resolvedViewer, err := resolveStreamViewer(c.Request().Context(), c.QueryParam("characterId"), database)
			if err != nil {
				var httpErr *echo.HTTPError
				if errors.As(err, &httpErr) {
					return httpErr
				}
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return echo.NewHTTPError(http.StatusBadRequest, "complete onboarding first")
				}
				return echo.NewHTTPError(http.StatusInternalServerError, "failed to resolve stream character")
			}
			viewer = resolvedViewer
		}

		requestUpgrader := upgrader
		requestUpgrader.CheckOrigin = buildOriginChecker(cfg)
		requestUpgrader.Subprotocols = []string{"lived.v1"}
		conn, err := requestUpgrader.Upgrade(c.Response(), c.Request(), nil)
		if err != nil {
			return err
		}
		defer conn.Close()

		ctx, cancel := context.WithCancel(c.Request().Context())
		defer cancel()

		readDone := make(chan struct{})
		go func() {
			defer close(readDone)
			for {
				if _, _, readErr := conn.ReadMessage(); readErr != nil {
					cancel()
					return
				}
			}
		}()

		resumeCursor := parseStreamResumeCursor(c.QueryParam("lastEventId"))

		tickEvery := cfg.TickInterval
		if tickEvery <= 0 {
			tickEvery = time.Second
		}

		ticker := time.NewTicker(tickEvery)
		defer ticker.Stop()

		if writeErr := writeWorldSnapshot(ctx, conn, database, viewer, resumeCursor); writeErr != nil {
			return nil
		}
		resumeCursor = nil

		for {
			select {
			case <-ctx.Done():
				return nil
			case <-readDone:
				return nil
			case <-ticker.C:
				if writeErr := writeWorldSnapshot(ctx, conn, database, viewer, nil); writeErr != nil {
					return nil
				}
			}
		}
	}
}

func writeWorldSnapshot(ctx context.Context, conn *websocket.Conn, database *gorm.DB, viewer *streamViewer, resumeCursor *streamResumeCursor) error {
	realmID := uint(1)
	if viewer != nil && viewer.RealmID != 0 {
		realmID = viewer.RealmID
	}

	tick, err := gameplay.CurrentWorldTickForRealm(ctx, database, realmID)
	if err != nil {
		return err
	}

	market, err := gameplay.GetMarketStatus(ctx, database, tick, realmID)
	if err != nil {
		return err
	}

	event := worldStreamEvent{
		Type:        "world_snapshot",
		EventID:     composeStreamEventID(realmID, tick),
		At:          time.Now().UTC().Format(time.RFC3339),
		Tick:        tick,
		Day:         tick / (24 * 60),
		MinuteOfDay: market.MinuteOfDay,
		Clock:       clockLabel(market.MinuteOfDay),
		DayPart:     dayPartLabel(market.MinuteOfDay),
		MarketOpen:  market.IsOpen,
		MarketState: market.SessionState,
	}

	if resumeCursor != nil && strings.TrimSpace(resumeCursor.Raw) != "" {
		event.Resume = buildResumeStatus(resumeCursor, realmID, tick, event.EventID)
	}

	if viewer != nil {
		snapshot, err := gameplay.LoadWorldSnapshot(ctx, database, viewer.PlayerID, realmID)
		if err != nil {
			return err
		}

		queuedOrActive := 0
		for _, behavior := range snapshot.Behaviors {
			if behavior.ActorType != gameplay.ActorPlayer || behavior.ActorID != viewer.PlayerID {
				continue
			}
			if behavior.State == "queued" || behavior.State == "active" {
				queuedOrActive++
			}
		}

		event.Player = &playerStreamStatus{
			Name:            viewer.Name,
			Coins:           snapshot.Inventory["coins"],
			AscensionCount:  snapshot.AscensionCount,
			WealthBonusPct:  snapshot.WealthBonusPct,
			QueuedOrActive:  queuedOrActive,
			AscensionReady:  snapshot.Ascension.Available,
			AscensionReason: snapshot.Ascension.Reason,
		}
	} else {
		primaryPlayer, err := loadPrimaryPlayer(ctx, database)
		if err != nil {
			return err
		}
		if primaryPlayer != nil {
			snapshot, err := gameplay.LoadWorldSnapshot(ctx, database, primaryPlayer.ID, realmID)
			if err != nil {
				return err
			}

			queuedOrActive := 0
			for _, behavior := range snapshot.Behaviors {
				if behavior.ActorType != gameplay.ActorPlayer || behavior.ActorID != primaryPlayer.ID {
					continue
				}
				if behavior.State == "queued" || behavior.State == "active" {
					queuedOrActive++
				}
			}

			event.Player = &playerStreamStatus{
				Name:            primaryPlayer.Name,
				Coins:           snapshot.Inventory["coins"],
				AscensionCount:  snapshot.AscensionCount,
				WealthBonusPct:  snapshot.WealthBonusPct,
				QueuedOrActive:  queuedOrActive,
				AscensionReady:  snapshot.Ascension.Available,
				AscensionReason: snapshot.Ascension.Reason,
			}
		}
	}

	_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return conn.WriteJSON(event)
}

func buildResumeStatus(cursor *streamResumeCursor, currentRealmID uint, currentTick int64, resolvedEventID string) *streamResumeStatus {
	status := &streamResumeStatus{
		RequestedLastEventID: strings.TrimSpace(cursor.Raw),
		ResolvedEventID:      resolvedEventID,
		Mode:                 "snapshot_fallback",
	}

	if !cursor.Valid {
		status.Reason = cursor.Reason
		return status
	}
	if cursor.RealmID != currentRealmID {
		status.Reason = "realm_mismatch"
		return status
	}

	gap := currentTick - cursor.Tick
	if gap <= 1 {
		status.Mode = "cursor"
		status.Reason = "cursor_contiguous"
		return status
	}

	status.GapTicks = gap
	status.Reason = "cursor_gap_snapshot"
	return status
}

func composeStreamEventID(realmID uint, tick int64) string {
	return fmt.Sprintf("%d:%d", realmID, tick)
}

func parseStreamResumeCursor(raw string) *streamResumeCursor {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	parts := strings.Split(raw, ":")
	if len(parts) != 2 {
		return &streamResumeCursor{Raw: raw, Valid: false, Reason: "invalid_format"}
	}

	realm, realmErr := strconv.ParseUint(strings.TrimSpace(parts[0]), 10, 64)
	tick, tickErr := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
	if realmErr != nil || realm == 0 {
		return &streamResumeCursor{Raw: raw, Valid: false, Reason: "invalid_realm"}
	}
	if tickErr != nil || tick < 0 {
		return &streamResumeCursor{Raw: raw, Valid: false, Reason: "invalid_tick"}
	}

	return &streamResumeCursor{Raw: raw, RealmID: uint(realm), Tick: tick, Valid: true}
}

func buildOriginChecker(cfg config.Config) func(r *http.Request) bool {
	allowed := map[string]struct{}{}
	for _, origin := range cfg.StreamAllowedOrigins {
		normalized := normalizeOrigin(origin)
		if normalized == "" {
			continue
		}
		allowed[normalized] = struct{}{}
	}

	frontendOrigin := normalizeOrigin(cfg.FrontendDevProxyURL)
	if frontendOrigin != "" {
		allowed[frontendOrigin] = struct{}{}
	}

	return func(r *http.Request) bool {
		origin := strings.TrimSpace(r.Header.Get("Origin"))
		if origin == "" {
			return true
		}

		normalizedOrigin := normalizeOrigin(origin)
		if normalizedOrigin == "" {
			return false
		}

		reqHost := strings.TrimSpace(r.Host)
		if reqHost != "" {
			if normalizedOrigin == "http://"+reqHost || normalizedOrigin == "https://"+reqHost {
				return true
			}
		}

		_, ok := allowed[normalizedOrigin]
		return ok
	}
}

func normalizeOrigin(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return ""
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return ""
	}
	return parsed.Scheme + "://" + parsed.Host
}

func resolveStreamViewer(ctx context.Context, rawCharacterID string, database *gorm.DB) (*streamViewer, error) {
	const defaultRealmID uint = 1

	actor, ok := serverAuth.ActorFromContext(ctx)
	if !ok {
		return nil, echo.NewHTTPError(http.StatusUnauthorized, "missing actor context")
	}

	query := database.WithContext(ctx).
		Where("account_id = ? AND status = ?", actor.AccountID, "active")

	if strings.TrimSpace(rawCharacterID) != "" {
		parsed, err := strconv.ParseUint(strings.TrimSpace(rawCharacterID), 10, 64)
		if err != nil || parsed == 0 {
			return nil, echo.NewHTTPError(http.StatusBadRequest, "characterId must be a positive integer")
		}
		query = query.Where("id = ?", uint(parsed))
	} else {
		query = query.Where("realm_id = ?", defaultRealmID)
	}

	character := dal.Character{}
	result := query.Order("is_primary DESC, id ASC").Limit(1).Find(&character)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, gorm.ErrRecordNotFound
	}

	return &streamViewer{PlayerID: character.PlayerID, RealmID: character.RealmID, Name: character.Name}, nil
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

func clockLabel(minuteOfDay int64) string {
	hours := minuteOfDay / 60
	minutes := minuteOfDay % 60
	return fmt.Sprintf("%02d:%02d", hours, minutes)
}

func dayPartLabel(minuteOfDay int64) string {
	switch {
	case minuteOfDay < 5*60:
		return "Night"
	case minuteOfDay < 8*60:
		return "Dawn"
	case minuteOfDay < 12*60:
		return "Morning"
	case minuteOfDay < 17*60:
		return "Afternoon"
	case minuteOfDay < 20*60:
		return "Evening"
	default:
		return "Late Night"
	}
}

func IsWebSocketUpgrade(c echo.Context) bool {
	upgrade := strings.ToLower(c.Request().Header.Get("Upgrade"))
	connection := strings.ToLower(c.Request().Header.Get("Connection"))
	return upgrade == "websocket" && strings.Contains(connection, "upgrade")
}
