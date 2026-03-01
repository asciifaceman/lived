package chat

import "testing"

func TestNormalizeChannel(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr bool
	}{
		{name: "default", raw: "", want: "global"},
		{name: "trim and lower", raw: " Trade-1 ", want: "trade-1"},
		{name: "underscore", raw: "help_room", want: "help_room"},
		{name: "invalid char", raw: "general!", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := normalizeChannel(test.raw)
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

func TestParseRealmID(t *testing.T) {
	realmID, err := parseRealmID("")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if realmID != 1 {
		t.Fatalf("expected default realm 1, got %d", realmID)
	}

	if _, err := parseRealmID("0"); err == nil {
		t.Fatal("expected error for zero realm")
	}
}

func TestParseOptionalPositiveUint_Empty(t *testing.T) {
	value, err := parseOptionalPositiveUint("", "characterId")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if value != 0 {
		t.Fatalf("expected zero for empty input, got %d", value)
	}
}

func TestParseOptionalPositiveUint_Valid(t *testing.T) {
	value, err := parseOptionalPositiveUint("9", "characterId")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if value != 9 {
		t.Fatalf("expected parsed value 9, got %d", value)
	}
}

func TestParseOptionalPositiveUint_Invalid(t *testing.T) {
	if _, err := parseOptionalPositiveUint("0", "characterId"); err == nil {
		t.Fatal("expected error for zero value")
	}
	if _, err := parseOptionalPositiveUint("xyz", "characterId"); err == nil {
		t.Fatal("expected error for non-numeric value")
	}
}

func TestEventTypeForChannel(t *testing.T) {
	channel := "global"

	if got := eventTypeForChannel(channel, messageClassPlayer); got != eventPrefixPlayer+channel {
		t.Fatalf("expected player event type %q, got %q", eventPrefixPlayer+channel, got)
	}
	if got := eventTypeForChannel(channel, messageClassModerator); got != eventPrefixMod+channel {
		t.Fatalf("expected moderator event type %q, got %q", eventPrefixMod+channel, got)
	}
	if got := eventTypeForChannel(channel, messageClassAdmin); got != eventPrefixAdmin+channel {
		t.Fatalf("expected admin event type %q, got %q", eventPrefixAdmin+channel, got)
	}
	if got := eventTypeForChannel(channel, messageClassSystem); got != eventPrefixSystem+channel {
		t.Fatalf("expected system event type %q, got %q", eventPrefixSystem+channel, got)
	}
	if got := eventTypeForChannel(channel, "unknown"); got != eventPrefixPlayer+channel {
		t.Fatalf("expected fallback player event type %q, got %q", eventPrefixPlayer+channel, got)
	}
}

func TestMessageClassFromEventType(t *testing.T) {
	channel := "global"

	if got := messageClassFromEventType(eventPrefixPlayer+channel, channel); got != messageClassPlayer {
		t.Fatalf("expected player class, got %q", got)
	}
	if got := messageClassFromEventType(eventPrefixMod+channel, channel); got != messageClassModerator {
		t.Fatalf("expected moderator class, got %q", got)
	}
	if got := messageClassFromEventType(eventPrefixAdmin+channel, channel); got != messageClassAdmin {
		t.Fatalf("expected admin class, got %q", got)
	}
	if got := messageClassFromEventType(eventPrefixSystem+channel, channel); got != messageClassSystem {
		t.Fatalf("expected system class, got %q", got)
	}
	if got := messageClassFromEventType("some_other_event", channel); got != messageClassPlayer {
		t.Fatalf("expected fallback player class, got %q", got)
	}
}

func TestEventTypesForChannel(t *testing.T) {
	channel := "trade"
	got := eventTypesForChannel(channel)

	if len(got) != 4 {
		t.Fatalf("expected 4 event types, got %d", len(got))
	}

	want := []string{
		eventPrefixPlayer + channel,
		eventPrefixMod + channel,
		eventPrefixAdmin + channel,
		eventPrefixSystem + channel,
	}

	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("unexpected event type at index %d: expected %q got %q", i, want[i], got[i])
		}
	}
}

func TestHasRole(t *testing.T) {
	roles := []string{" player ", " Moderator", "ADMIN "}

	if !hasRole(roles, "player") {
		t.Fatal("expected player role to match")
	}
	if !hasRole(roles, "moderator") {
		t.Fatal("expected moderator role to match")
	}
	if !hasRole(roles, "admin") {
		t.Fatal("expected admin role to match")
	}
	if hasRole(roles, "missing") {
		t.Fatal("expected missing role not to match")
	}
}

func TestAuthorRoleFromMessageClass(t *testing.T) {
	if got := authorRoleFromMessageClass(messageClassPlayer); got != "player" {
		t.Fatalf("expected player role, got %q", got)
	}
	if got := authorRoleFromMessageClass(messageClassModerator); got != "moderator" {
		t.Fatalf("expected moderator role, got %q", got)
	}
	if got := authorRoleFromMessageClass(messageClassAdmin); got != "admin" {
		t.Fatalf("expected admin role, got %q", got)
	}
	if got := authorRoleFromMessageClass(messageClassSystem); got != "system" {
		t.Fatalf("expected system role, got %q", got)
	}
	if got := authorRoleFromMessageClass("unknown"); got != "player" {
		t.Fatalf("expected fallback player role, got %q", got)
	}
}

func TestAuthorBadgesFromMessageClass(t *testing.T) {
	if got := authorBadgesFromMessageClass(messageClassPlayer); len(got) != 0 {
		t.Fatalf("expected no badges for player, got %v", got)
	}

	if got := authorBadgesFromMessageClass(messageClassModerator); len(got) != 1 || got[0] != "moderator" {
		t.Fatalf("expected moderator badge, got %v", got)
	}

	if got := authorBadgesFromMessageClass(messageClassAdmin); len(got) != 1 || got[0] != "admin" {
		t.Fatalf("expected admin badge, got %v", got)
	}

	if got := authorBadgesFromMessageClass(messageClassSystem); len(got) != 1 || got[0] != "system" {
		t.Fatalf("expected system badge, got %v", got)
	}
}

func TestCensorContains(t *testing.T) {
	message := "This is BAD and bad again"
	redacted, hits := censorContains(message, "bad")

	if hits != 2 {
		t.Fatalf("expected 2 hits, got %d", hits)
	}
	if redacted != "This is *** and *** again" {
		t.Fatalf("unexpected redacted message: %q", redacted)
	}
}

func TestCensorContains_EmptyTerm(t *testing.T) {
	message := "hello world"
	redacted, hits := censorContains(message, "   ")

	if hits != 0 {
		t.Fatalf("expected 0 hits, got %d", hits)
	}
	if redacted != message {
		t.Fatalf("expected unchanged message, got %q", redacted)
	}
}
