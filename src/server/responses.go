package server

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/labstack/echo/v4"
)

const (
	statusSuccess = "success"
	statusError   = "error"
)

type apiResponse struct {
	Status    string `json:"status"`
	Message   string `json:"message"`
	RequestID string `json:"requestId,omitempty"`
	Data      any    `json:"data,omitempty"`
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

func makeHTTPErrorHandler(logger *slog.Logger) echo.HTTPErrorHandler {
	return func(err error, c echo.Context) {
		if c.Response().Committed {
			return
		}

		statusCode := http.StatusInternalServerError
		message := "An unexpected error interrupted the story."

		var httpError *echo.HTTPError
		if errors.As(err, &httpError) {
			if httpError.Code > 0 {
				statusCode = httpError.Code
			}

			switch typed := httpError.Message.(type) {
			case string:
				if typed != "" {
					message = typed
				}
			default:
				if typed != nil {
					message = "Request could not be completed."
				}
			}
		}

		requestID := c.Response().Header().Get(echo.HeaderXRequestID)
		if requestID == "" {
			requestID = c.Request().Header.Get(echo.HeaderXRequestID)
		}

		logger.Error(
			"http_error",
			"status", statusCode,
			"request_id", requestID,
			"method", c.Request().Method,
			"path", c.Path(),
			"error", err,
		)

		_ = c.JSON(statusCode, apiResponse{
			Status:    statusError,
			Message:   message,
			RequestID: requestID,
		})
	}
}
