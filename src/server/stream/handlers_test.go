package stream

import (
	"net/http/httptest"
	"testing"

	"github.com/asciifaceman/lived/pkg/config"
)

func TestParseStreamResumeCursor(t *testing.T) {
	tests := []struct {
		name      string
		raw       string
		exists    bool
		valid     bool
		realmID   uint
		tick      int64
		reason    string
	}{
		{name: "empty", raw: "", exists: false},
		{name: "invalid format", raw: "abc", exists: true, valid: false, reason: "invalid_format"},
		{name: "invalid realm", raw: "0:12", exists: true, valid: false, reason: "invalid_realm"},
		{name: "invalid tick", raw: "1:-2", exists: true, valid: false, reason: "invalid_tick"},
		{name: "valid", raw: "2:15", exists: true, valid: true, realmID: 2, tick: 15},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cursor := parseStreamResumeCursor(test.raw)
			if !test.exists {
				if cursor != nil {
					t.Fatalf("expected nil cursor")
				}
				return
			}
			if cursor == nil {
				t.Fatalf("expected cursor")
			}
			if cursor.Valid != test.valid {
				t.Fatalf("expected valid=%v, got %v", test.valid, cursor.Valid)
			}
			if cursor.Reason != test.reason {
				t.Fatalf("expected reason=%q, got %q", test.reason, cursor.Reason)
			}
			if cursor.RealmID != test.realmID {
				t.Fatalf("expected realm=%d, got %d", test.realmID, cursor.RealmID)
			}
			if cursor.Tick != test.tick {
				t.Fatalf("expected tick=%d, got %d", test.tick, cursor.Tick)
			}
		})
	}
}

func TestBuildOriginChecker(t *testing.T) {
	cfg := config.Config{
		FrontendDevProxyURL: "http://localhost:5173",
		StreamAllowedOrigins: []string{"https://lived.example.com"},
	}
	checker := buildOriginChecker(cfg)

	req := httptest.NewRequest("GET", "http://localhost:8080/v1/stream/world", nil)
	req.Host = "localhost:8080"
	req.Header.Set("Origin", "http://localhost:8080")
	if !checker(req) {
		t.Fatal("expected same-host origin to be accepted")
	}

	req2 := httptest.NewRequest("GET", "http://localhost:8080/v1/stream/world", nil)
	req2.Host = "localhost:8080"
	req2.Header.Set("Origin", "http://localhost:5173")
	if !checker(req2) {
		t.Fatal("expected configured frontend origin to be accepted")
	}

	req3 := httptest.NewRequest("GET", "http://localhost:8080/v1/stream/world", nil)
	req3.Host = "localhost:8080"
	req3.Header.Set("Origin", "https://evil.example")
	if checker(req3) {
		t.Fatal("expected unknown origin to be rejected")
	}
}

func TestBuildResumeStatusFallbackAndCursorMode(t *testing.T) {
	invalid := &streamResumeCursor{Raw: "abc", Valid: false, Reason: "invalid_format"}
	status := buildResumeStatus(invalid, 1, 100, "1:100")
	if status.Mode != "snapshot_fallback" {
		t.Fatalf("expected fallback mode, got %q", status.Mode)
	}
	if status.Reason != "invalid_format" {
		t.Fatalf("expected invalid format reason, got %q", status.Reason)
	}

	contiguous := &streamResumeCursor{Raw: "1:99", Valid: true, RealmID: 1, Tick: 99}
	status = buildResumeStatus(contiguous, 1, 100, "1:100")
	if status.Mode != "cursor" {
		t.Fatalf("expected cursor mode, got %q", status.Mode)
	}

	gap := &streamResumeCursor{Raw: "1:50", Valid: true, RealmID: 1, Tick: 50}
	status = buildResumeStatus(gap, 1, 100, "1:100")
	if status.Mode != "snapshot_fallback" {
		t.Fatalf("expected fallback mode for gap, got %q", status.Mode)
	}
	if status.GapTicks != 50 {
		t.Fatalf("expected gap ticks 50, got %d", status.GapTicks)
	}
}
