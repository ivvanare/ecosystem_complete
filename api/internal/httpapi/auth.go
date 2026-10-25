package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/mail"
	"strings"
)

// SignupRequester starts a registration. The handler owns transport-level validation; the
// service owns the anti-enumeration rules.
type SignupRequester interface {
	Request(ctx context.Context, email, name, password string) error
}

// MinPasswordLength is the contract's floor (C12). Twelve characters, no composition
// rules: length beats symbol soup, and the policy is easy to explain to a user.
const MinPasswordLength = 12

type authHandlers struct {
	signup SignupRequester
	logger *slog.Logger
}

type signupRequest struct {
	Email    string `json:"email"`
	Name     string `json:"name"`
	Password string `json:"password"`
}

// startSignup always answers 202 for a well-formed request, whether or not the address is
// already registered. Any difference in the response would turn this endpoint into an
// account oracle.
func (h *authHandlers) startSignup(w http.ResponseWriter, r *http.Request) {
	var body signupRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "The request body is not valid JSON.")
		return
	}

	if fields := validateSignup(body); len(fields) > 0 {
		writeValidationError(w, fields)
		return
	}

	if err := h.signup.Request(r.Context(), body.Email, body.Name, body.Password); err != nil {
		h.logger.Error("signup request failed", slog.String("error", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
		return
	}

	w.WriteHeader(http.StatusAccepted)
}

type fieldError struct {
	Field string `json:"field"`
	Code  string `json:"code"`
}

func validateSignup(body signupRequest) []fieldError {
	var fields []fieldError

	email := strings.TrimSpace(body.Email)
	if _, err := mail.ParseAddress(email); err != nil || !strings.Contains(email, "@") {
		fields = append(fields, fieldError{Field: "email", Code: "invalid"})
	}

	name := strings.TrimSpace(body.Name)
	if name == "" {
		fields = append(fields, fieldError{Field: "name", Code: "required"})
	} else if len(name) > 120 {
		fields = append(fields, fieldError{Field: "name", Code: "too_long"})
	}

	if len(body.Password) < MinPasswordLength {
		fields = append(fields, fieldError{Field: "password", Code: "too_short"})
	}

	return fields
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorEnvelope{Error: errorBody{Code: code, Message: message}})
}

func writeValidationError(w http.ResponseWriter, fields []fieldError) {
	writeJSON(w, http.StatusUnprocessableEntity, errorEnvelope{
		Error: errorBody{
			Code:    "validation_failed",
			Message: "The request has invalid fields.",
			Details: map[string]any{"fields": fields},
		},
	})
}
