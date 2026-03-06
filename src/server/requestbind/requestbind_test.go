package requestbind

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

type samplePayload struct {
	Name string `json:"name"`
}

func TestJSON(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr bool
	}{
		{name: "valid payload", body: `{"name":"lived"}`},
		{name: "unknown field", body: `{"name":"lived","extra":true}`, wantErr: true},
		{name: "trailing payload", body: `{"name":"lived"}{"name":"dup"}`, wantErr: true},
		{name: "malformed payload", body: `{"name":`, wantErr: true},
	}

	e := echo.New()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/", strings.NewReader(test.body))
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			var payload samplePayload
			err := JSON(c, &payload, "invalid payload")
			if test.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if payload.Name != "lived" {
				t.Fatalf("expected parsed name lived, got %q", payload.Name)
			}
		})
	}
}