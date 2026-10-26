package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/ivvanare/ecosystem_complete/api/internal/session"
	"github.com/ivvanare/ecosystem_complete/api/internal/social"
)

// SocialAuthenticator exchanges a provider ID token for a session.
type SocialAuthenticator interface {
	Authenticate(ctx context.Context, provider, idToken string, meta session.Meta) (session.Tokens, error)
}

type socialHandlers struct {
	service SocialAuthenticator
	cookies cookieSettings
	logger  *slog.Logger
}

type socialRequest struct {
	Provider string `json:"provider"`
	IDToken  string `json:"id_token"`
	Device   string `json:"device"`
}

// socialLogin never returns the provider's token: it is validated and discarded, and the
// client gets this API's own pair.
func (h *socialHandlers) login(w http.ResponseWriter, r *http.Request) {
	var body socialRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "The request body is not valid JSON.")
		return
	}

	provider := strings.ToUpper(strings.TrimSpace(body.Provider))
	var fields []fieldError
	if provider != social.ProviderGoogle && provider != social.ProviderApple {
		fields = append(fields, fieldError{Field: "provider", Code: "invalid"})
	}
	if strings.TrimSpace(body.IDToken) == "" {
		fields = append(fields, fieldError{Field: "id_token", Code: "required"})
	}
	if len(fields) > 0 {
		writeValidationError(w, fields)
		return
	}

	meta := session.Meta{
		Device:    strings.TrimSpace(body.Device),
		UserAgent: r.UserAgent(),
		IP:        clientIP(r),
	}

	tokens, err := h.service.Authenticate(r.Context(), provider, body.IDToken, meta)
	if err != nil {
		switch {
		case errors.Is(err, social.ErrUnknownProvider):
			writeValidationError(w, []fieldError{{Field: "provider", Code: "not_configured"}})
			return
		case errors.Is(err, social.ErrInvalidToken):
			writeError(w, http.StatusUnauthorized, "invalid_token", "The provider token is not valid.")
			return
		default:
			writeError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
			return
		}
	}

	writeTokenPair(w, r, http.StatusOK, tokens, h.cookies, h.logger)
}
