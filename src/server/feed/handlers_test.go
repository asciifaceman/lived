package feed

import "testing"

func TestParseRealmID(t *testing.T) {
	realmID, err := parseRealmID("")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if realmID != 1 {
		t.Fatalf("expected default realm 1, got %d", realmID)
	}

	realmID, err = parseRealmID("7")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if realmID != 7 {
		t.Fatalf("expected realm 7, got %d", realmID)
	}

	if _, err := parseRealmID("abc"); err == nil {
		t.Fatal("expected error for invalid realm")
	}
}

func TestPositiveMinuteOfDay(t *testing.T) {
	if got := positiveMinuteOfDay(-1); got != 1439 {
		t.Fatalf("expected 1439, got %d", got)
	}
	if got := positiveMinuteOfDay(61); got != 61 {
		t.Fatalf("expected 61, got %d", got)
	}
}
