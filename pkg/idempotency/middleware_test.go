package idempotency

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

func TestStoreMiddleware_ReplaysSuccessfulResponse(t *testing.T) {
	e := echo.New()
	store := NewStore(10*time.Minute, ClientIPScope)
	middleware := store.Middleware()

	handlerCalls := 0
	handler := middleware(func(c echo.Context) error {
		handlerCalls++
		return c.JSON(http.StatusCreated, map[string]any{"ok": true, "calls": handlerCalls})
	})

	req1 := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(`{"x":1}`))
	req1.Header.Set("Content-Type", echo.MIMEApplicationJSON)
	req1.Header.Set("Idempotency-Key", "abc-123")
	req1.RemoteAddr = "203.0.113.20:1111"
	rec1 := httptest.NewRecorder()
	c1 := e.NewContext(req1, rec1)
	c1.SetPath("/test")

	if err := handler(c1); err != nil {
		t.Fatalf("first request failed: %v", err)
	}
	if got := rec1.Header().Get("Idempotency-Status"); got != "stored" {
		t.Fatalf("expected first response Idempotency-Status stored, got %q", got)
	}

	req2 := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(`{"x":1}`))
	req2.Header.Set("Content-Type", echo.MIMEApplicationJSON)
	req2.Header.Set("Idempotency-Key", "abc-123")
	req2.RemoteAddr = "203.0.113.20:1111"
	rec2 := httptest.NewRecorder()
	c2 := e.NewContext(req2, rec2)
	c2.SetPath("/test")

	if err := handler(c2); err != nil {
		t.Fatalf("second request failed: %v", err)
	}

	if handlerCalls != 1 {
		t.Fatalf("expected handler called once, got %d", handlerCalls)
	}
	if rec2.Code != http.StatusCreated {
		t.Fatalf("expected replay status %d, got %d", http.StatusCreated, rec2.Code)
	}
	if got := rec2.Header().Get("Idempotency-Replayed"); got != "true" {
		t.Fatalf("expected Idempotency-Replayed header true, got %q", got)
	}
	if got := rec2.Header().Get("Idempotency-Status"); got != "replayed" {
		t.Fatalf("expected replay response Idempotency-Status replayed, got %q", got)
	}
}

func TestStoreMiddleware_RejectsDifferentPayloadForSameKey(t *testing.T) {
	e := echo.New()
	store := NewStore(10*time.Minute, ClientIPScope)
	middleware := store.Middleware()

	handler := middleware(func(c echo.Context) error {
		return c.JSON(http.StatusCreated, map[string]any{"ok": true})
	})

	req1 := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(`{"x":1}`))
	req1.Header.Set("Idempotency-Key", "same-key")
	req1.RemoteAddr = "203.0.113.20:1111"
	rec1 := httptest.NewRecorder()
	c1 := e.NewContext(req1, rec1)
	c1.SetPath("/test")
	if err := handler(c1); err != nil {
		t.Fatalf("first request failed: %v", err)
	}

	req2 := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(`{"x":2}`))
	req2.Header.Set("Idempotency-Key", "same-key")
	req2.RemoteAddr = "203.0.113.20:1111"
	rec2 := httptest.NewRecorder()
	c2 := e.NewContext(req2, rec2)
	c2.SetPath("/test")
	err := handler(c2)
	if err == nil {
		t.Fatalf("expected conflict error for different payload")
	}

	httpErr, ok := err.(*echo.HTTPError)
	if !ok {
		t.Fatalf("expected HTTPError, got %T", err)
	}
	if httpErr.Code != http.StatusConflict {
		t.Fatalf("expected conflict status, got %d", httpErr.Code)
	}
}

func TestAccountOrIPScope_UsesAccountThenFallsBackIP(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	req.RemoteAddr = "203.0.113.30:1111"
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	scope := AccountOrIPScope(func(ctx context.Context) (uint, bool) {
		return 77, true
	})
	if got := scope(c); got != "account:77" {
		t.Fatalf("expected account scope, got %q", got)
	}

	scope = AccountOrIPScope(func(ctx context.Context) (uint, bool) {
		return 0, false
	})
	if got := scope(c); got != "ip:203.0.113.30" {
		t.Fatalf("expected ip fallback scope, got %q", got)
	}
}

func TestStoreMiddleware_ExpiresRecordAfterTTL(t *testing.T) {
	e := echo.New()
	base := time.Date(2026, 2, 28, 12, 0, 0, 0, time.UTC)
	store := NewStore(time.Minute, ClientIPScope)
	store.now = func() time.Time { return base }
	middleware := store.Middleware()

	handlerCalls := 0
	handler := middleware(func(c echo.Context) error {
		handlerCalls++
		return c.JSON(http.StatusCreated, map[string]any{"calls": handlerCalls})
	})

	run := func() {
		req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(`{"x":1}`))
		req.Header.Set("Idempotency-Key", "exp-key")
		req.RemoteAddr = "203.0.113.40:1111"
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/test")
		if err := handler(c); err != nil {
			t.Fatalf("request failed: %v", err)
		}
		var payload map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatalf("invalid response payload: %v", err)
		}
	}

	run()
	run()
	if handlerCalls != 1 {
		t.Fatalf("expected replay within ttl, calls=%d", handlerCalls)
	}

	base = base.Add(61 * time.Second)
	run()
	if handlerCalls != 2 {
		t.Fatalf("expected new execution after ttl expiry, calls=%d", handlerCalls)
	}
}
