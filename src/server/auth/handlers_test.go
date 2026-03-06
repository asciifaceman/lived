package auth

import (
	"net/http/httptest"
	"testing"
)

func TestExtractAccessToken(t *testing.T) {
	req := httptest.NewRequest("GET", "http://localhost", nil)
	req.Header.Set("Authorization", "Bearer header-token")
	if got := extractAccessToken(req, "query-token", false); got != "header-token" {
		t.Fatalf("expected header token, got %q", got)
	}

	req = httptest.NewRequest("GET", "http://localhost", nil)
	req.Header.Set("Sec-WebSocket-Protocol", "lived.v1, bearer.ws-token")
	if got := extractAccessToken(req, "query-token", false); got != "ws-token" {
		t.Fatalf("expected websocket protocol token, got %q", got)
	}

	req = httptest.NewRequest("GET", "http://localhost", nil)
	if got := extractAccessToken(req, "query-token", false); got != "" {
		t.Fatalf("expected no token when query fallback disabled, got %q", got)
	}
	if got := extractAccessToken(req, "query-token", true); got != "query-token" {
		t.Fatalf("expected query token fallback, got %q", got)
	}
}

func TestParseWebSocketProtocolBearer(t *testing.T) {
	if got := parseWebSocketProtocolBearer("lived.v1, bearer.token-123"); got != "token-123" {
		t.Fatalf("expected token-123, got %q", got)
	}
	if got := parseWebSocketProtocolBearer("lived.v1"); got != "" {
		t.Fatalf("expected empty token, got %q", got)
	}
}
