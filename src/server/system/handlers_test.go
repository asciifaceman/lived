package system

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/asciifaceman/lived/src/gameplay"
	"github.com/labstack/echo/v4"
)

func TestParseGameDurationMinutes(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    int64
		wantErr bool
	}{
		{name: "empty", raw: "", want: 0},
		{name: "minutes", raw: "90m", want: 90},
		{name: "hours", raw: "12h", want: 720},
		{name: "days", raw: "2d", want: 2880},
		{name: "uppercase trimmed", raw: " 3H ", want: 180},
		{name: "missing unit", raw: "10", wantErr: true},
		{name: "non-positive", raw: "0m", wantErr: true},
		{name: "unknown unit", raw: "5w", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseGameDurationMinutes(test.raw)
			if test.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != test.want {
				t.Fatalf("expected %d, got %d", test.want, got)
			}
		})
	}
}

func TestParseOptionalPositiveUintQuery(t *testing.T) {
	value, err := parseOptionalPositiveUintQuery("", "realmId", 1)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if value != 1 {
		t.Fatalf("expected fallback value 1, got %d", value)
	}

	value, err = parseOptionalPositiveUintQuery("7", "realmId", 1)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if value != 7 {
		t.Fatalf("expected parsed value 7, got %d", value)
	}

	if _, err := parseOptionalPositiveUintQuery("0", "realmId", 1); err == nil {
		t.Fatal("expected error for zero value")
	}
}

func TestParseMarketHistoryQuery(t *testing.T) {
	e := echo.New()

	req := httptest.NewRequest("GET", "/v1/system/market/history", nil)
	c := e.NewContext(req, httptest.NewRecorder())
	query, err := parseMarketHistoryQuery(c, 1)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if query.Limit != 100 {
		t.Fatalf("expected default limit 100, got %d", query.Limit)
	}
	if query.Realm != 1 {
		t.Fatalf("expected default realm 1, got %d", query.Realm)
	}

	req = httptest.NewRequest("GET", "/v1/system/market/history?symbol=wood&limit=999&realmId=3", nil)
	c = e.NewContext(req, httptest.NewRecorder())
	query, err = parseMarketHistoryQuery(c, 1)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if query.Symbol != "wood" {
		t.Fatalf("expected symbol wood, got %q", query.Symbol)
	}
	if query.Limit != 500 {
		t.Fatalf("expected clamped limit 500, got %d", query.Limit)
	}
	if query.Realm != 3 {
		t.Fatalf("expected realm 3, got %d", query.Realm)
	}

	req = httptest.NewRequest("GET", "/v1/system/market/history?limit=0", nil)
	c = e.NewContext(req, httptest.NewRecorder())
	if _, err := parseMarketHistoryQuery(c, 1); err == nil {
		t.Fatal("expected error for invalid limit")
	}
}

func TestParseBehaviorQueueMode(t *testing.T) {
	tests := []struct {
		name            string
		mode            string
		repeatUntil     string
		wantMode        string
		wantRepeatUntil int64
		wantErr         bool
	}{
		{name: "default once", mode: "", repeatUntil: "", wantMode: "once", wantRepeatUntil: 0},
		{name: "repeat mode", mode: "repeat", repeatUntil: "", wantMode: "repeat", wantRepeatUntil: 0},
		{name: "repeat-until", mode: "repeat-until", repeatUntil: "2h", wantMode: "repeat-until", wantRepeatUntil: 120},
		{name: "invalid mode", mode: "loop", repeatUntil: "", wantErr: true},
		{name: "repeat-until missing duration", mode: "repeat-until", repeatUntil: "", wantErr: true},
		{name: "once with repeatUntil", mode: "once", repeatUntil: "30m", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mode, repeatUntil, err := parseBehaviorQueueMode(test.mode, test.repeatUntil)
			if test.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if mode != test.wantMode {
				t.Fatalf("expected mode %q, got %q", test.wantMode, mode)
			}
			if repeatUntil != test.wantRepeatUntil {
				t.Fatalf("expected repeatUntil %d, got %d", test.wantRepeatUntil, repeatUntil)
			}
		})
	}
}

func TestQueueBehaviorErrorStatus(t *testing.T) {
	if got := queueBehaviorErrorStatus(errors.New("bad input")); got != http.StatusBadRequest {
		t.Fatalf("expected bad request status, got %d", got)
	}

	conflictErr := errors.New("wrapped")
	conflictErr = errors.Join(conflictErr, gameplay.ErrBehaviorConflict)
	if got := queueBehaviorErrorStatus(conflictErr); got != http.StatusConflict {
		t.Fatalf("expected conflict status, got %d", got)
	}
}

func TestVersionHandlerIncludesEmbeddedGameDataMetadata(t *testing.T) {
	e := echo.New()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/system/version", nil)
	c := e.NewContext(req, rec)

	handler := makeVersionHandler()
	if err := handler(c); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	payload := map[string]any{}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected data object, got %#v", payload["data"])
	}

	if _, ok := data["api"].(string); !ok {
		t.Fatalf("expected api string, got %#v", data["api"])
	}
	if _, ok := data["backend"].(string); !ok {
		t.Fatalf("expected backend string, got %#v", data["backend"])
	}
	if _, ok := data["frontend"].(string); !ok {
		t.Fatalf("expected frontend string, got %#v", data["frontend"])
	}

	gameData, ok := data["gameData"].(map[string]any)
	if !ok {
		t.Fatalf("expected gameData object, got %#v", data["gameData"])
	}

	manifestVersion, ok := gameData["manifestVersion"].(float64)
	if !ok || manifestVersion < 1 {
		t.Fatalf("expected positive manifestVersion, got %#v", gameData["manifestVersion"])
	}

	filesHash, ok := gameData["filesHash"].(string)
	if !ok || filesHash == "" {
		t.Fatalf("expected non-empty filesHash, got %#v", gameData["filesHash"])
	}
}
