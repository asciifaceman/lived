package ratelimit

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
)

type IdentifierFunc func(c echo.Context) string

type counterEntry struct {
	count   int
	resetAt time.Time
}

type FixedWindowLimiter struct {
	window         time.Duration
	identify       IdentifierFunc
	cleanupEvery   uint64
	requestCounter uint64
	entries        map[string]counterEntry
	mu             sync.Mutex
	now            func() time.Time
}

func NewFixedWindowLimiter(window time.Duration, identify IdentifierFunc) *FixedWindowLimiter {
	if window <= 0 {
		window = time.Minute
	}
	if identify == nil {
		identify = ClientIPIdentifier
	}

	return &FixedWindowLimiter{
		window:       window,
		identify:     identify,
		cleanupEvery: 128,
		entries:      make(map[string]counterEntry),
		now:          time.Now,
	}
}

func ClientIPIdentifier(c echo.Context) string {
	identifier := strings.TrimSpace(c.RealIP())
	if identifier == "" {
		return "unknown"
	}
	return identifier
}

func AccountOrIPIdentifier(accountIDFromContext func(context.Context) (uint, bool)) IdentifierFunc {
	return func(c echo.Context) string {
		if accountIDFromContext != nil {
			if accountID, ok := accountIDFromContext(c.Request().Context()); ok && accountID > 0 {
				return "account:" + strconv.FormatUint(uint64(accountID), 10)
			}
		}

		return "ip:" + ClientIPIdentifier(c)
	}
}

func (l *FixedWindowLimiter) Middleware(scope string, maxRequests int) echo.MiddlewareFunc {
	if maxRequests <= 0 {
		return func(next echo.HandlerFunc) echo.HandlerFunc {
			return func(c echo.Context) error {
				return next(c)
			}
		}
	}

	normalizedScope := strings.TrimSpace(scope)
	if normalizedScope == "" {
		normalizedScope = "default"
	}

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			identifier := strings.TrimSpace(l.identify(c))
			if identifier == "" {
				identifier = "unknown"
			}

			allowed, retryAfter := l.allow(normalizedScope+":"+identifier, maxRequests)
			if !allowed {
				retryAfterSeconds := int(retryAfter.Seconds())
				if retryAfterSeconds < 1 {
					retryAfterSeconds = 1
				}
				c.Response().Header().Set("Retry-After", strconv.Itoa(retryAfterSeconds))
				return echo.NewHTTPError(http.StatusTooManyRequests, "rate limit exceeded")
			}

			return next(c)
		}
	}
}

func (l *FixedWindowLimiter) allow(key string, maxRequests int) (bool, time.Duration) {
	now := l.now().UTC()

	l.mu.Lock()
	defer l.mu.Unlock()

	l.requestCounter++
	if l.requestCounter%l.cleanupEvery == 0 {
		l.pruneExpiredLocked(now)
	}

	entry, exists := l.entries[key]
	if !exists || !now.Before(entry.resetAt) {
		l.entries[key] = counterEntry{count: 1, resetAt: now.Add(l.window)}
		return true, 0
	}

	if entry.count >= maxRequests {
		return false, entry.resetAt.Sub(now)
	}

	entry.count++
	l.entries[key] = entry
	return true, 0
}

func (l *FixedWindowLimiter) pruneExpiredLocked(now time.Time) {
	for key, entry := range l.entries {
		if !now.Before(entry.resetAt) {
			delete(l.entries, key)
		}
	}
}
