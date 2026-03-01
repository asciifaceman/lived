package mmo

import "testing"

func TestParseRealmID_Default(t *testing.T) {
	realmID, err := parseRealmID("")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if realmID != 1 {
		t.Fatalf("expected default realm 1, got %d", realmID)
	}
}

func TestParseRealmID_Valid(t *testing.T) {
	realmID, err := parseRealmID("42")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if realmID != 42 {
		t.Fatalf("expected realm 42, got %d", realmID)
	}
}

func TestParseRealmID_Invalid(t *testing.T) {
	if _, err := parseRealmID("0"); err == nil {
		t.Fatal("expected error for zero realm")
	}

	if _, err := parseRealmID("abc"); err == nil {
		t.Fatal("expected error for non-numeric realm")
	}
}

func TestParseOptionalPositiveUint_Empty(t *testing.T) {
	value, err := parseOptionalPositiveUint("", "characterId")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if value != 0 {
		t.Fatalf("expected zero value for empty input, got %d", value)
	}
}

func TestParseOptionalPositiveUint_Valid(t *testing.T) {
	value, err := parseOptionalPositiveUint("17", "characterId")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if value != 17 {
		t.Fatalf("expected parsed value 17, got %d", value)
	}
}

func TestParseOptionalPositiveUint_Invalid(t *testing.T) {
	if _, err := parseOptionalPositiveUint("0", "characterId"); err == nil {
		t.Fatal("expected error for zero value")
	}

	if _, err := parseOptionalPositiveUint("abc", "characterId"); err == nil {
		t.Fatal("expected error for non-numeric value")
	}
}
