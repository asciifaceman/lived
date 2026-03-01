package stream

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/asciifaceman/lived/pkg/config"
	"github.com/asciifaceman/lived/pkg/dal"
	"github.com/asciifaceman/lived/src/gameplay"
	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

type worldStreamEvent struct {
	Type        string              `json:"type"`
	At          string              `json:"at"`
	Tick        int64               `json:"tick"`
	Day         int64               `json:"day"`
	MinuteOfDay int64               `json:"minuteOfDay"`
	Clock       string              `json:"clock"`
	DayPart     string              `json:"dayPart"`
	MarketOpen  bool                `json:"marketOpen"`
	MarketState string              `json:"marketState"`
	Player      *playerStreamStatus `json:"player,omitempty"`
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

func RegisterRoutes(group *echo.Group, database *gorm.DB, cfg config.Config) {
	group.GET("/world", makeWorldStreamHandler(database, cfg))
}

func makeWorldStreamHandler(database *gorm.DB, cfg config.Config) echo.HandlerFunc {
	return func(c echo.Context) error {
		conn, err := upgrader.Upgrade(c.Response(), c.Request(), nil)
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

		tickEvery := cfg.TickInterval
		if tickEvery <= 0 {
			tickEvery = time.Second
		}

		ticker := time.NewTicker(tickEvery)
		defer ticker.Stop()

		if writeErr := writeWorldSnapshot(ctx, conn, database); writeErr != nil {
			return nil
		}

		for {
			select {
			case <-ctx.Done():
				return nil
			case <-readDone:
				return nil
			case <-ticker.C:
				if writeErr := writeWorldSnapshot(ctx, conn, database); writeErr != nil {
					return nil
				}
			}
		}
	}
}

func writeWorldSnapshot(ctx context.Context, conn *websocket.Conn, database *gorm.DB) error {
	tick, err := gameplay.CurrentWorldTick(ctx, database)
	if err != nil {
		return err
	}

	market, err := gameplay.GetMarketStatus(ctx, database, tick)
	if err != nil {
		return err
	}

	event := worldStreamEvent{
		Type:        "world_snapshot",
		At:          time.Now().UTC().Format(time.RFC3339),
		Tick:        tick,
		Day:         tick / (24 * 60),
		MinuteOfDay: market.MinuteOfDay,
		Clock:       clockLabel(market.MinuteOfDay),
		DayPart:     dayPartLabel(market.MinuteOfDay),
		MarketOpen:  market.IsOpen,
		MarketState: market.SessionState,
	}

	primaryPlayer, err := loadPrimaryPlayer(ctx, database)
	if err != nil {
		return err
	}
	if primaryPlayer != nil {
		snapshot, err := gameplay.LoadWorldSnapshot(ctx, database, primaryPlayer.ID)
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

	_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return conn.WriteJSON(event)
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
