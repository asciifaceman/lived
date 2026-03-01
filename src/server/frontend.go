package server

import (
	"errors"
	"io/fs"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/asciifaceman/lived/pkg/config"
	"github.com/labstack/echo/v4"
)

func registerFrontendRoutes(e *echo.Echo, cfg config.Config) {
	if strings.TrimSpace(cfg.FrontendDevProxyURL) != "" {
		registerFrontendDevProxyRoutes(e, cfg.FrontendDevProxyURL)
		return
	}

	if embeddedFS, ok := embeddedFrontendFS(); ok {
		registerEmbeddedFrontendRoutes(e, embeddedFS)
		return
	}

	frontendDir := filepath.Join("web", "dist")
	indexPath := filepath.Join(frontendDir, "index.html")

	if _, err := os.Stat(indexPath); err != nil {
		e.GET("/", func(c echo.Context) error {
			return respondSuccess(c, http.StatusOK, "Frontend is not built yet. Run `npm install && npm run build` in ./web.", map[string]any{"docs": "/swagger/", "api": "/v1"})
		})
		return
	}

	assetsDir := filepath.Join(frontendDir, "assets")
	if _, err := os.Stat(assetsDir); err == nil {
		e.Static("/assets", assetsDir)
	}

	e.GET("/", func(c echo.Context) error {
		return c.File(indexPath)
	})

	e.GET("/*", func(c echo.Context) error {
		path := c.Request().URL.Path
		if strings.HasPrefix(path, "/v1") || strings.HasPrefix(path, "/swagger") || path == "/health" {
			return echo.NewHTTPError(http.StatusNotFound, "not found")
		}

		if strings.HasPrefix(path, "/assets/") {
			return echo.NewHTTPError(http.StatusNotFound, "asset not found")
		}

		if _, err := os.Stat(indexPath); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return echo.NewHTTPError(http.StatusNotFound, "frontend is not built")
			}
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load frontend")
		}
		return c.File(indexPath)
	})
}

func registerEmbeddedFrontendRoutes(e *echo.Echo, frontendFS fs.FS) {
	if _, err := fs.Stat(frontendFS, "index.html"); err != nil {
		e.GET("/", func(c echo.Context) error {
			return respondSuccess(c, http.StatusOK, "Embedded frontend is enabled but assets are missing. Run `cd web && npm run build:embed` before building with -tags embed_frontend.", map[string]any{"docs": "/swagger/", "api": "/v1"})
		})
		return
	}

	e.GET("/", func(c echo.Context) error {
		return serveEmbeddedPath(c, frontendFS, "index.html")
	})

	e.GET("/*", func(c echo.Context) error {
		path := c.Request().URL.Path
		if strings.HasPrefix(path, "/v1") || strings.HasPrefix(path, "/swagger") || path == "/health" {
			return echo.NewHTTPError(http.StatusNotFound, "not found")
		}

		requested := strings.TrimPrefix(path, "/")
		if requested == "" {
			return serveEmbeddedPath(c, frontendFS, "index.html")
		}

		if strings.HasPrefix(requested, "assets/") || strings.Contains(filepath.Base(requested), ".") {
			if _, err := fs.Stat(frontendFS, requested); err != nil {
				if strings.HasPrefix(requested, "assets/") {
					return echo.NewHTTPError(http.StatusNotFound, "asset not found")
				}
				return echo.NewHTTPError(http.StatusNotFound, "not found")
			}
			return serveEmbeddedPath(c, frontendFS, requested)
		}

		return serveEmbeddedPath(c, frontendFS, "index.html")
	})
}

func serveEmbeddedPath(c echo.Context, frontendFS fs.FS, path string) error {
	trimmed := strings.TrimPrefix(path, "/")
	if trimmed == "" {
		trimmed = "index.html"
	}

	server := http.FileServer(http.FS(frontendFS))
	req := c.Request()
	originalPath := req.URL.Path
	originalRawPath := req.URL.RawPath
	req.URL.Path = "/" + trimmed
	req.URL.RawPath = req.URL.Path
	defer func() {
		req.URL.Path = originalPath
		req.URL.RawPath = originalRawPath
	}()

	server.ServeHTTP(c.Response(), req)
	return nil
}

func registerFrontendDevProxyRoutes(e *echo.Echo, target string) {
	targetURL, err := url.Parse(strings.TrimSpace(target))
	if err != nil || targetURL.Scheme == "" || targetURL.Host == "" {
		e.GET("/", func(c echo.Context) error {
			return respondSuccess(c, http.StatusOK, "Invalid LIVED_WEB_DEV_PROXY_URL. Expected format like http://localhost:5173", map[string]any{"docs": "/swagger/", "api": "/v1"})
		})
		return
	}

	proxy := httputil.NewSingleHostReverseProxy(targetURL)

	e.Any("/*", func(c echo.Context) error {
		path := c.Request().URL.Path
		if strings.HasPrefix(path, "/v1") || strings.HasPrefix(path, "/swagger") || path == "/health" {
			return echo.NewHTTPError(http.StatusNotFound, "not found")
		}

		proxy.ServeHTTP(c.Response(), c.Request())
		return nil
	})
}
