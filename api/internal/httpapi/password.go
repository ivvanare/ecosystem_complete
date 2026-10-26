package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/mail"
	"strings"

	"github.com/ivvanare/ecosystem_complete/api/internal/password"
)

// PasswordService is the recovery and change surface.
type PasswordService interface {
	RequestReset(ctx context.Context, email string) error
	Reset(ctx context.Context, email, code, newPassword string) error
	Change(ctx context.Context, userID, currentPassword, newPassword string) error
}

type passwordHandlers struct {
	service PasswordService
}

type forgotRequest struct {
	Email string `json:"email"`
}

// forgotPassword always answers 202 for a well-formed address, whether or not the account
// exists. Anything else turns this endpoint into an account oracle.
func (h *passwordHandlers) forgotPassword(w http.ResponseWriter, r *http.Request) {
	var body forgotRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "The request body is not valid JSON.")
		return
	}
	if _, err := mail.ParseAddress(strings.TrimSpace(body.Email)); err != nil {
		writeValidationError(w, []fieldError{{Field: "email", Code: "invalid"}})
		return
	}

	if err := h.service.RequestReset(r.Context(), body.Email); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
		return
	}

	w.WriteHeader(http.StatusAccepted)
}

type resetRequest struct {
	Email       string `json:"email"`
	Code        string `json:"code"`
	NewPassword string `json:"new_password"`
}

func (h *passwordHandlers) resetPassword(w http.ResponseWriter, r *http.Request) {
	var body resetRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "The request body is not valid JSON.")
		return
	}

	var fields []fieldError
	if _, err := mail.ParseAddress(strings.TrimSpace(body.Email)); err != nil {
		fields = append(fields, fieldError{Field: "email", Code: "invalid"})
	}
	if !codePattern.MatchString(body.Code) {
		fields = append(fields, fieldError{Field: "code", Code: "invalid"})
	}
	if len(body.NewPassword) < MinPasswordLength {
		fields = append(fields, fieldError{Field: "new_password", Code: "too_short"})
	}
	if len(fields) > 0 {
		writeValidationError(w, fields)
		return
	}

	err := h.service.Reset(r.Context(), body.Email, body.Code, body.NewPassword)
	switch {
	case errors.Is(err, password.ErrInvalidCode):
		writeError(w, http.StatusBadRequest, "invalid_code", "The code is incorrect or has expired.")
		return
	case errors.Is(err, password.ErrTooManyAttempts):
		writeError(w, http.StatusTooManyRequests, "too_many_attempts", "Too many attempts. Request a new code.")
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
		return
	}

	// Every session is gone now: the client must log in again with the new password.
	w.WriteHeader(http.StatusOK)
}

type changeRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

func (h *passwordHandlers) changePassword(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication is required.")
		return
	}

	var body changeRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "The request body is not valid JSON.")
		return
	}

	var fields []fieldError
	if body.CurrentPassword == "" {
		fields = append(fields, fieldError{Field: "current_password", Code: "required"})
	}
	if len(body.NewPassword) < MinPasswordLength {
		fields = append(fields, fieldError{Field: "new_password", Code: "too_short"})
	}
	if len(fields) > 0 {
		writeValidationError(w, fields)
		return
	}

	err := h.service.Change(r.Context(), userID, body.CurrentPassword, body.NewPassword)
	switch {
	case errors.Is(err, password.ErrInvalidCurrentPassword):
		writeError(w, http.StatusBadRequest, "invalid_password", "The current password is incorrect.")
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
		return
	}

	w.WriteHeader(http.StatusOK)
}
