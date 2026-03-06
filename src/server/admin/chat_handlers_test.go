package admin

import "testing"

func TestNormalizeChatChannelKey(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr bool
	}{
		{name: "trim and lower", raw: " Trade-1 ", want: "trade-1"},
		{name: "underscore", raw: "help_room", want: "help_room"},
		{name: "empty", raw: "", wantErr: true},
		{name: "invalid char", raw: "general!", wantErr: true},
		{name: "too long", raw: "abcdefghijklmnopqrstuvwxyz1234567", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := normalizeChatChannelKey(test.raw)
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
				t.Fatalf("expected %q, got %q", test.want, got)
			}
		})
	}
}

func TestParseRealmIDWithDefault(t *testing.T) {
	if got := parseRealmIDWithDefault(""); got != 1 {
		t.Fatalf("expected default realm 1 for empty input, got %d", got)
	}

	if got := parseRealmIDWithDefault("   "); got != 1 {
		t.Fatalf("expected default realm 1 for whitespace input, got %d", got)
	}

	if got := parseRealmIDWithDefault("0"); got != 1 {
		t.Fatalf("expected default realm 1 for zero input, got %d", got)
	}

	if got := parseRealmIDWithDefault("abc"); got != 1 {
		t.Fatalf("expected default realm 1 for invalid input, got %d", got)
	}

	if got := parseRealmIDWithDefault("42"); got != 42 {
		t.Fatalf("expected parsed realm 42, got %d", got)
	}
}

func TestParseRuleIDPathParam(t *testing.T) {
	if got, err := parseRuleIDPathParam("5"); err != nil || got != 5 {
		t.Fatalf("expected parsed ruleId 5,nil got %d,%v", got, err)
	}

	if _, err := parseRuleIDPathParam("0"); err == nil {
		t.Fatal("expected error for zero ruleId")
	}

	if _, err := parseRuleIDPathParam("abc"); err == nil {
		t.Fatal("expected error for invalid ruleId")
	}
}

func TestParseChatBindingScope(t *testing.T) {
	if got, err := parseChatBindingScope(""); err != nil || got != "realm" {
		t.Fatalf("expected default realm scope, got %q err=%v", got, err)
	}

	if got, err := parseChatBindingScope("realm"); err != nil || got != "realm" {
		t.Fatalf("expected realm scope, got %q err=%v", got, err)
	}

	if _, err := parseChatBindingScope("global"); err == nil {
		t.Fatal("expected error for non-realm binding scope")
	}
}

func TestParseChatPolicyScope(t *testing.T) {
	if got, err := parseChatPolicyScope(""); err != nil || got != "global" {
		t.Fatalf("expected default global scope, got %q err=%v", got, err)
	}

	if got, err := parseChatPolicyScope("global"); err != nil || got != "global" {
		t.Fatalf("expected global scope, got %q err=%v", got, err)
	}

	if _, err := parseChatPolicyScope("realm"); err == nil {
		t.Fatal("expected error for non-global policy scope")
	}
}

func TestChatBindingScopeKey(t *testing.T) {
	if got := chatBindingScopeKey("realm", 7); got != "realm:7" {
		t.Fatalf("expected realm:7, got %q", got)
	}

	if got := chatBindingScopeKey("realm", 0); got != "realm:1" {
		t.Fatalf("expected realm:1 for zero realm input, got %q", got)
	}

	if got := chatBindingScopeKey("global", 99); got != "global" {
		t.Fatalf("expected passthrough for non-realm scope, got %q", got)
	}
}
