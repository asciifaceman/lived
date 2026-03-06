package ratelimit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

func TestFixedWindowLimiter_AllowWithinAndBeyondWindow(t *testing.T) {
	baseNow := time.Date(2026, 2, 28, 12, 0, 0, 0, time.UTC)
	limiter := NewFixedWindowLimiter(time.Minute, nil)
	limiter.now = func() time.Time { return baseNow }

	allowed, retryAfter := limiter.allow("scope:user", 2)
	if !allowed {
		t.Fatalf("expected first request to be allowed")
	}
	if retryAfter != 0 {
		t.Fatalf("expected zero retry after for allowed request, got %v", retryAfter)
	}

	allowed, _ = limiter.allow("scope:user", 2)
	if !allowed {
		t.Fatalf("expected second request to be allowed")
	}

	allowed, retryAfter = limiter.allow("scope:user", 2)
	if allowed {
		t.Fatalf("expected third request in window to be denied")
	}
	if retryAfter <= 0 {
		t.Fatalf("expected positive retry after, got %v", retryAfter)
	}

	baseNow = baseNow.Add(61 * time.Second)
	allowed, _ = limiter.allow("scope:user", 2)
	if !allowed {
		t.Fatalf("expected request after window reset to be allowed")
	}
}

func TestFixedWindowLimiter_MiddlewareSetsRetryAfterAndScopesIndependently(t *testing.T) {
	baseNow := time.Date(2026, 2, 28, 12, 0, 0, 0, time.UTC)
	limiter := NewFixedWindowLimiter(time.Minute, func(c echo.Context) string {
		return c.RealIP()
	})
	limiter.now = func() time.Time { return baseNow }

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.RemoteAddr = "203.0.113.1:1234"
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	next := func(c echo.Context) error {
		return c.NoContent(http.StatusNoContent)
	}

	mwA := limiter.Middleware("scope_a", 1)
	if err := mwA(next)(c); err != nil {
		t.Fatalf("expected first scope_a call to pass: %v", err)
	}

	if err := mwA(next)(c); err == nil {
		t.Fatalf("expected second scope_a call to fail")
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Fatalf("expected Retry-After header to be set")
	}

	mwB := limiter.Middleware("scope_b", 1)
	if err := mwB(next)(c); err != nil {
		t.Fatalf("expected first scope_b call to pass independently: %v", err)
	}
}

func TestAccountOrIPIdentifier_UsesAccountWhenAvailable(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.9:9999"
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	identifier := AccountOrIPIdentifier(func(ctx context.Context) (uint, bool) {
		return 42, true
	})

	if got := identifier(c); got != "account:42" {
		t.Fatalf("expected account-based identifier, got %q", got)
	}
}

func TestAccountOrIPIdentifier_FallsBackToIP(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.10:1234"
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	identifier := AccountOrIPIdentifier(func(ctx context.Context) (uint, bool) {
		return 0, false
	})

	if got := identifier(c); got != "ip:203.0.113.10" {
		t.Fatalf("expected ip fallback identifier, got %q", got)
	}
}
