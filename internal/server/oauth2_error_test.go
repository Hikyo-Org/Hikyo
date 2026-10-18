package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/admission"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
)

func TestOAuth2StartErrorSafeDetail(t *testing.T) {
	const internal = "synthetic-internal-wrapper"
	for _, tt := range []struct {
		name   string
		err    error
		status int
		detail string
	}{
		{"bare", service.ErrOAuth2Reauth, 409, service.ErrOAuth2Reauth.Error()},
		{"wrapped", fmt.Errorf(internal+": %w", service.ErrOAuth2Reauth), 409, service.ErrOAuth2Reauth.Error()},
		{"nested", fmt.Errorf(internal+": %w", fmt.Errorf("nested: %w", service.ErrOAuth2Reauth)), 409, service.ErrOAuth2Reauth.Error()},
		{"environment", fmt.Errorf(internal+": %w", service.ErrEnvironmentNotForPurpose), 400, "OAuth2 does not accept environment_id"},
		{"unrelated refusal", fmt.Errorf(internal+": %w", domain.ErrUnauthenticated), 401, ""},
		{"overloaded", fmt.Errorf(internal+": %w", admission.ErrOverloaded), 429, ""},
		{"unrelated fault", errors.New(internal), 500, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			if err := oauth2StartError(&API{}, t.Context(), tt.err).VisitOauth2StartResponse(w); err != nil {
				t.Fatal(err)
			}
			response := w.Result()
			defer response.Body.Close()
			if response.StatusCode != tt.status {
				t.Fatalf("status = %d, want %d", response.StatusCode, tt.status)
			}
			if strings.Contains(w.Body.String(), internal) {
				t.Fatalf("internal wrapper leaked: %s", w.Body.String())
			}
			var body apigen.Error
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			detail := ""
			if body.Error.Detail != nil {
				detail = *body.Error.Detail
			}
			if detail != tt.detail {
				t.Fatalf("detail = %q, want %q", detail, tt.detail)
			}
			if tt.status == http.StatusConflict && body.Error.Code != apigen.ErrorCodeConflict {
				t.Fatal("conflict code changed")
			}
		})
	}
}
