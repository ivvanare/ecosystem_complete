package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/mail"
	"strconv"
	"strings"

	"github.com/ivvanare/ecosystem_complete/api/internal/emailchange"
	"github.com/ivvanare/ecosystem_complete/api/internal/security"
)

// EmailChangeService moves an account to a new address.
type EmailChangeService interface {
	Request(ctx context.Context, userID, newEmail, currentPassword string) error
	Verify(ctx context.Context, userID, code string) error
}

type emailChangeHandlers struct {
	service EmailChangeService
}

type emailChangeRequest struct {
	NewEmail string `json:"new_email"`
	Password string `json:"password"`
}

func (h *emailChangeHandlers) request(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication is required.")
		return
	}

	var body emailChangeRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "The request body is not valid JSON.")
		return
	}

	var fields []fieldError
	if _, err := mail.ParseAddress(strings.TrimSpace(body.NewEmail)); err != nil {
		fields = append(fields, fieldError{Field: "new_email", Code: "invalid"})
	}
	if body.Password == "" {
		fields = append(fields, fieldError{Field: "password", Code: "required"})
	}
	if len(fields) > 0 {
		writeValidationError(w, fields)
		return
	}

	err := h.service.Request(r.Context(), userID, body.NewEmail, body.Password)
	if err != nil {
		var quota *security.TooManyRequestsError
		switch {
		case errors.As(err, &quota):
			retry := int(quota.RetryAfter.Seconds())
			w.Header().Set("Retry-After", strconv.Itoa(retry))
			writeJSON(w, http.StatusTooManyRequests, errorEnvelope{Error: errorBody{
				Code:    "rate_limited",
				Message: "Too many requests. Try again later.",
				Details: map[string]any{"retry_after_seconds": retry},
			}})
			return
		case errors.Is(err, emailchange.ErrSameEmail):
			writeValidationError(w, []fieldError{{Field: "new_email", Code: "unchanged"}})
			return
		case errors.Is(err, emailchange.ErrEmailTaken):
			writeError(w, http.StatusConflict, "email_unavailable", "That email cannot be used.")
			return
		case errors.Is(err, emailchange.ErrInvalidPassword):
			writeError(w, http.StatusBadRequest, "invalid_password", "The current password is incorrect.")
			return
		default:
			writeError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
			return
		}
	}

	w.WriteHeader(http.StatusAccepted)
}

type emailChangeVerifyRequest struct {
	Code string `json:"code"`
}

func (h *emailChangeHandlers) verify(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication is required.")
		return
	}

	var body emailChangeVerifyRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "The request body is not valid JSON.")
		return
	}
	if !codePattern.MatchString(body.Code) {
		writeValidationError(w, []fieldError{{Field: "code", Code: "invalid"}})
		return
	}

	err := h.service.Verify(r.Context(), userID, body.Code)
	switch {
	case errors.Is(err, emailchange.ErrInvalidCode):
		writeError(w, http.StatusBadRequest, "invalid_code", "The code is incorrect or has expired.")
		return
	case errors.Is(err, emailchange.ErrTooManyAttempts):
		writeError(w, http.StatusTooManyRequests, "too_many_attempts", "Too many attempts. Request a new code.")
		return
	case errors.Is(err, emailchange.ErrEmailTaken):
		writeError(w, http.StatusConflict, "email_unavailable", "That email cannot be used.")
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
		return
	}

	// Every session is gone: the address that identified the account just changed.
	w.WriteHeader(http.StatusOK)
}
