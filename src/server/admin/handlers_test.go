package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/asciifaceman/lived/pkg/dal"
	"github.com/labstack/echo/v4"
)

func TestHasRole(t *testing.T) {
	if !hasRole([]string{"player", "admin"}, "admin") {
		t.Fatal("expected admin role to be detected")
	}
	if hasRole([]string{"player", "moderator"}, "admin") {
		t.Fatal("expected missing admin role to return false")
	}
	if hasRole(nil, "admin") {
		t.Fatal("expected nil roles to return false")
	}
}

func TestValidateRealmAction(t *testing.T) {
	tests := []struct {
		name    string
		req     realmActionRequest
		wantErr bool
	}{
		{
			name: "reset defaults",
			req: realmActionRequest{
				Action:     actionMarketResetDefaults,
				ReasonCode: "economy_repair",
			},
		},
		{
			name: "set price",
			req: realmActionRequest{
				Action:     actionMarketSetPrice,
				ReasonCode: "manual_tuning",
				ItemKey:    "scrap",
				Price:      12,
			},
		},
		{
			name: "missing reason",
			req: realmActionRequest{
				Action: actionMarketResetDefaults,
			},
			wantErr: true,
		},
		{
			name: "set price missing item",
			req: realmActionRequest{
				Action:     actionMarketSetPrice,
				ReasonCode: "manual_tuning",
				Price:      9,
			},
			wantErr: true,
		},
		{
			name: "unsupported action",
			req: realmActionRequest{
				Action:     "realm_pause",
				ReasonCode: "maintenance",
			},
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := validateRealmAction(test.req)
			if test.wantErr {
				if err == nil {
					t.Fatal("expected validation error")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected validation error: %v", err)
			}
		})
	}
}

func TestValidateReasonAndNote(t *testing.T) {
	if _, _, err := validateReasonAndNote("", ""); err == nil {
		t.Fatal("expected error when reasonCode is empty")
	}

	reasonCode, note, err := validateReasonAndNote("OPS_FIX", "  maintenance note ")
	if err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
	if reasonCode != "ops_fix" {
		t.Fatalf("expected normalized reasonCode ops_fix, got %q", reasonCode)
	}
	if note != "maintenance note" {
		t.Fatalf("expected trimmed note, got %q", note)
	}
}

func TestValidateRoleModeration(t *testing.T) {
	tests := []struct {
		name    string
		req     moderationRoleRequest
		wantErr bool
	}{
		{
			name: "grant valid",
			req: moderationRoleRequest{
				RoleKey:    "moderator",
				Action:     "grant",
				ReasonCode: "staffing",
			},
		},
		{
			name: "revoke valid",
			req: moderationRoleRequest{
				RoleKey:    "moderator",
				Action:     "revoke",
				ReasonCode: "policy_violation",
			},
		},
		{
			name: "missing role key",
			req: moderationRoleRequest{
				Action:     "grant",
				ReasonCode: "staffing",
			},
			wantErr: true,
		},
		{
			name: "invalid action",
			req: moderationRoleRequest{
				RoleKey:    "moderator",
				Action:     "set",
				ReasonCode: "staffing",
			},
			wantErr: true,
		},
		{
			name: "invalid role chars",
			req: moderationRoleRequest{
				RoleKey:    "mod!",
				Action:     "grant",
				ReasonCode: "staffing",
			},
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := validateRoleModeration(test.req)
			if test.wantErr {
				if err == nil {
					t.Fatal("expected validation error")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected validation error: %v", err)
			}
		})
	}
}

func TestParseAuditLimit(t *testing.T) {
	if got, err := parseAuditLimit(""); err != nil || got != defaultAuditLimit {
		t.Fatalf("expected default limit %d, got %d, err=%v", defaultAuditLimit, got, err)
	}

	if got, err := parseAuditLimit("999"); err != nil || got != maxAuditLimit {
		t.Fatalf("expected capped limit %d, got %d, err=%v", maxAuditLimit, got, err)
	}

	if _, err := parseAuditLimit("0"); err == nil {
		t.Fatal("expected error for zero limit")
	}
}

func TestParseWindowTicks(t *testing.T) {
	if got, err := parseWindowTicks(""); err != nil || got != defaultStatsWindowTicks {
		t.Fatalf("expected default window ticks %d, got %d, err=%v", defaultStatsWindowTicks, got, err)
	}

	if got, err := parseWindowTicks("99999999"); err != nil || got != maxStatsWindowTicks {
		t.Fatalf("expected capped window ticks %d, got %d, err=%v", maxStatsWindowTicks, got, err)
	}

	if _, err := parseWindowTicks("0"); err == nil {
		t.Fatal("expected error for zero window ticks")
	}
}

func TestParseOptionalUintQuery(t *testing.T) {
	if got, err := parseOptionalUintQuery("", "realmId"); err != nil || got != 0 {
		t.Fatalf("expected optional empty to return 0,nil; got %d,%v", got, err)
	}

	if got, err := parseOptionalUintQuery("7", "realmId"); err != nil || got != 7 {
		t.Fatalf("expected parsed value 7,nil; got %d,%v", got, err)
	}

	if _, err := parseOptionalUintQuery("abc", "realmId"); err == nil {
		t.Fatal("expected error for invalid integer")
	}
}

func TestParseOptionalBoolQuery(t *testing.T) {
	if got, err := parseOptionalBoolQuery("", "includeRawJson"); err != nil || got {
		t.Fatalf("expected default false,nil; got %v,%v", got, err)
	}

	if got, err := parseOptionalBoolQuery("true", "includeRawJson"); err != nil || !got {
		t.Fatalf("expected true,nil; got %v,%v", got, err)
	}

	if got, err := parseOptionalBoolQuery("0", "includeRawJson"); err != nil || got {
		t.Fatalf("expected false,nil; got %v,%v", got, err)
	}

	if _, err := parseOptionalBoolQuery("maybe", "includeRawJson"); err == nil {
		t.Fatal("expected error for invalid bool")
	}
}

func TestParseAuditIDPathParam(t *testing.T) {
	if got, err := parseAuditIDPathParam("17"); err != nil || got != 17 {
		t.Fatalf("expected parsed audit id 17,nil; got %d,%v", got, err)
	}

	if _, err := parseAuditIDPathParam("0"); err == nil {
		t.Fatal("expected error for zero audit id")
	}
}

func TestParseAuditFilters(t *testing.T) {
	e := echo.New()
	req := newMockRequest(t, "/v1/admin/audit?realmId=2&actorAccountId=8&actionKey=ACCOUNT_LOCK&beforeId=91&includeRawJson=true&limit=44")
	c := e.NewContext(req, newMockResponseRecorder())

	filters, err := parseAuditFilters(c)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if filters.RealmID != 2 || filters.ActorAccountID != 8 || filters.ActionKey != "account_lock" || filters.BeforeID != 91 || !filters.IncludeRawJSON || filters.Limit != 44 {
		t.Fatalf("unexpected parsed filters: %+v", filters)
	}
}

func TestParseAuditFilters_InvalidBeforeID(t *testing.T) {
	e := echo.New()
	req := newMockRequest(t, "/v1/admin/audit?beforeId=abc")
	c := e.NewContext(req, newMockResponseRecorder())

	if _, err := parseAuditFilters(c); err == nil {
		t.Fatal("expected parse error for invalid beforeId")
	}
}

func TestBuildAuditCSV(t *testing.T) {
	rows := []dal.AdminAuditEvent{
		{
			BaseModel:      dal.BaseModel{ID: 5, CreatedAt: time.Date(2026, 2, 28, 12, 0, 0, 0, time.UTC)},
			RealmID:        1,
			ActorAccountID: 9,
			ActionKey:      "account_lock",
			ReasonCode:     "abuse",
			Note:           "manual action",
			OccurredTick:   1440,
			BeforeJSON:     `{"status":"active"}`,
			AfterJSON:      `{"status":"locked"}`,
		},
	}

	encoded, err := buildAuditCSV(rows)
	if err != nil {
		t.Fatalf("unexpected csv build error: %v", err)
	}

	text := string(encoded)
	if !strings.Contains(text, "id,realm_id,actor_account_id,action_key") {
		t.Fatalf("expected csv header, got: %s", text)
	}
	if !strings.Contains(text, "account_lock") {
		t.Fatalf("expected csv to include action key, got: %s", text)
	}
}

func newMockRequest(t *testing.T, target string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	return req
}

func newMockResponseRecorder() *httptest.ResponseRecorder {
	return httptest.NewRecorder()
}
