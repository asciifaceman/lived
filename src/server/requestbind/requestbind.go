package requestbind

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/labstack/echo/v4"
)

func JSON(c echo.Context, target any, invalidMessage string) error {
	decoder := json.NewDecoder(c.Request().Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(target); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, invalidMessage)
	}

	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return echo.NewHTTPError(http.StatusBadRequest, invalidMessage)
	}

	return nil
}