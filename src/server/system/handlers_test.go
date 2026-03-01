package system

import "testing"

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
