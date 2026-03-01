package server

import (
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

func registerMiddleware(e *echo.Echo, logger *slog.Logger, customMiddlewares []echo.MiddlewareFunc) {
	e.Use(middleware.RequestID())
	e.Use(middleware.Recover())
	e.Use(middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		HandleError:      true,
		LogRemoteIP:      true,
		LogHost:          true,
		LogMethod:        true,
		LogURI:           true,
		LogRequestID:     true,
		LogUserAgent:     true,
		LogStatus:        true,
		LogError:         true,
		LogLatency:       true,
		LogContentLength: true,
		LogResponseSize:  true,
		LogValuesFunc: func(c echo.Context, values middleware.RequestLoggerValues) error {
			safeURI := sanitizeLoggedURI(values.URI)
			attrs := []any{
				"request_id", values.RequestID,
				"remote_ip", values.RemoteIP,
				"host", values.Host,
				"method", values.Method,
				"uri", safeURI,
				"user_agent", values.UserAgent,
				"status", values.Status,
				"latency", values.Latency.String(),
				"bytes_in", values.ContentLength,
				"bytes_out", values.ResponseSize,
			}

			if values.Error != nil {
				attrs = append(attrs, "error", values.Error.Error())
			}

			if correlation := traceLogAttrs(c.Request().Context()); len(correlation) > 0 {
				attrs = append(attrs, correlation...)
			}

			switch {
			case values.Status >= http.StatusInternalServerError || values.Error != nil:
				logger.Error("http_request", attrs...)
			case values.Status >= http.StatusBadRequest:
				logger.Warn("http_request", attrs...)
			default:
				logger.Info("http_request", attrs...)
			}

			return nil
		},
	}))

	for _, middlewareFunc := range customMiddlewares {
		e.Use(middlewareFunc)
	}
}

func sanitizeLoggedURI(uri string) string {
	parsed, err := url.ParseRequestURI(uri)
	if err != nil || parsed == nil {
		return uri
	}

	if parsed.RawQuery == "" {
		return uri
	}

	query := parsed.Query()
	for key := range query {
		normalized := strings.ToLower(strings.TrimSpace(key))
		switch normalized {
		case "accesstoken", "access_token", "refreshtoken", "refresh_token", "token", "authorization":
			query.Set(key, "<redacted>")
		}
	}

	parsed.RawQuery = query.Encode()
	return parsed.String()
}
