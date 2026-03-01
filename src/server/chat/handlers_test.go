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
