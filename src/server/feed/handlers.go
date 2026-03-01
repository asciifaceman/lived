package feed

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/asciifaceman/lived/pkg/config"
	"github.com/asciifaceman/lived/pkg/dal"
	"github.com/asciifaceman/lived/src/gameplay"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

const statusSuccess = "success"

type apiResponse struct {
	Status    string `json:"status"`
	Message   string `json:"message"`
	RequestID string `json:"requestId,omitempty"`
	Data      any    `json:"data,omitempty"`
}

type feedEvent struct {
	ID          uint   `json:"id"`
	Tick        int64  `json:"tick"`
	Day         int64  `json:"day"`
	MinuteOfDay int64  `json:"minuteOfDay"`
	Clock       string `json:"clock"`
	EventType   string `json:"eventType"`
	Source      string `json:"source"`
	Message     string `json:"message"`
	ReferenceID uint   `json:"referenceId"`
}

func RegisterRoutes(group *echo.Group, database *gorm.DB, _ config.Config) {
	group.GET("/public", makePublicFeedHandler(database))
}

func makePublicFeedHandler(database *gorm.DB) echo.HandlerFunc {
	return func(c echo.Context) error {
		realmID, err := parseRealmID(c.QueryParam("realmId"))
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}

		limit := 50
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

		rows := make([]dal.WorldEvent, 0)
		if err := database.WithContext(c.Request().Context()).
			Where("realm_id = ? AND visibility = ?", realmID, "public").
			Order("tick DESC, id DESC").
			Limit(limit).
			Find(&rows).Error; err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load public feed")
		}

		events := make([]feedEvent, 0, len(rows))
		for _, row := range rows {
			minuteOfDay := positiveMinuteOfDay(row.Tick)
			events = append(events, feedEvent{
				ID:          row.ID,
				Tick:        row.Tick,
				Day:         row.Tick / (24 * 60),
				MinuteOfDay: minuteOfDay,
				Clock:       clockLabel(minuteOfDay),
				EventType:   row.EventType,
				Source:      row.Source,
				Message:     row.Message,
				ReferenceID: row.ReferenceID,
			})
		}

		currentTick, err := gameplay.CurrentWorldTickForRealm(c.Request().Context(), database, realmID)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load world tick")
		}

		return respondSuccess(c, http.StatusOK, "Public feed loaded.", map[string]any{
			"realmId":     realmID,
			"limit":       limit,
			"currentTick": currentTick,
			"events":      events,
		})
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

func respondSuccess(c echo.Context, code int, message string, data any) error {
	requestID := c.Response().Header().Get(echo.HeaderXRequestID)
	return c.JSON(code, apiResponse{
		Status:    statusSuccess,
		Message:   message,
		RequestID: requestID,
		Data:      data,
	})
}
