package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/mail"
	"regexp"
	"strconv"
	"strings"

	"github.com/ivvanare/ecosystem_complete/api/internal/login"
	"github.com/ivvanare/ecosystem_complete/api/internal/session"
	"github.com/ivvanare/ecosystem_complete/api/internal/signup"
)

// SignupRequester starts a registration. The handler owns transport-level validation; the
// service owns the anti-enumeration rules.
type SignupRequester interface {
	Request(ctx context.Context, email, name, password string) error
}

// MinPasswordLength is the contract's floor (C12). Twelve characters, no composition
// rules: length beats symbol soup, and the policy is easy to explain to a user.
const MinPasswordLength = 12

// SignupVerifier finishes a registration and opens the first session.
type SignupVerifier interface {
	Verify(ctx context.Context, email, code string, meta session.Meta) (session.Tokens, error)
}

// LoginAuthenticator exchanges credentials for a session.
type LoginAuthenticator interface {
	Login(ctx context.Context, email, password string, meta session.Meta) (session.Tokens, error)
}

// SessionRotator rotates and closes sessions.
type SessionRotator interface {
	Refresh(ctx context.Context, presented string, meta session.Meta) (session.Tokens, error)
	Logout(ctx context.Context, presented string) error
}

type authHandlers struct {
	signup   SignupRequester
	verifier SignupVerifier
	login    LoginAuthenticator
	rotator  SessionRotator
	logger   *slog.Logger
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

var codePattern = regexp.MustCompile(`^[0-9]{6}$`)

type verifyRequest struct {
	Email string `json:"email"`
	Code  string `json:"code"`
}

type tokenPairResponse struct {
	AccessToken      string `json:"access_token"`
	TokenType        string `json:"token_type"`
	ExpiresIn        int    `json:"expires_in"`
	RefreshToken     string `json:"refresh_token"`
	RefreshExpiresIn int    `json:"refresh_expires_in"`
}

// verifySignup consumes the emailed code. Every failure that depends on the code itself
// answers the same 400: distinguishing "no such registration" from "wrong code" would
// confirm which addresses are mid-registration.
func (h *authHandlers) verifySignup(w http.ResponseWriter, r *http.Request) {
	var body verifyRequest
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
	if len(fields) > 0 {
		writeValidationError(w, fields)
		return
	}

	// The contract's verify request has no device label; the user agent is what we know.
	meta := session.Meta{
		UserAgent: r.UserAgent(),
		IP:        clientIP(r),
	}

	tokens, err := h.verifier.Verify(r.Context(), body.Email, body.Code, meta)
	switch {
	case errors.Is(err, signup.ErrInvalidCode):
		writeError(w, http.StatusBadRequest, "invalid_code", "The code is incorrect or has expired.")
		return
	case errors.Is(err, signup.ErrTooManyAttempts):
		writeError(w, http.StatusTooManyRequests, "too_many_attempts", "Too many attempts. Request a new code.")
		return
	case err != nil:
		h.logger.Error("signup verification failed", slog.String("error", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
		return
	}

	writeJSON(w, http.StatusCreated, tokenPairResponse{
		AccessToken:      tokens.AccessToken,
		TokenType:        "Bearer",
		ExpiresIn:        tokens.ExpiresIn,
		RefreshToken:     tokens.RefreshToken,
		RefreshExpiresIn: tokens.RefreshExpiresIn,
	})
}

// clientIP is the peer address. There is no proxy in front of the API, so RemoteAddr is
// the client; when an edge appears it must be trusted explicitly, never blindly.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Device   string `json:"device"`
}

// login answers 401 for an unknown address, a wrong password and an inactive account
// alike, and 423 only when the account is genuinely locked.
func (h *authHandlers) loginUser(w http.ResponseWriter, r *http.Request) {
	var body loginRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "The request body is not valid JSON.")
		return
	}

	var fields []fieldError
	if _, err := mail.ParseAddress(strings.TrimSpace(body.Email)); err != nil {
		fields = append(fields, fieldError{Field: "email", Code: "invalid"})
	}
	if body.Password == "" {
		fields = append(fields, fieldError{Field: "password", Code: "required"})
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

	tokens, err := h.login.Login(r.Context(), body.Email, body.Password, meta)
	if err != nil {
		var locked *login.LockedError
		switch {
		case errors.As(err, &locked):
			retry := int(locked.RetryAfter.Seconds())
			w.Header().Set("Retry-After", strconv.Itoa(retry))
			writeJSON(w, http.StatusLocked, errorEnvelope{Error: errorBody{
				Code:    "account_locked",
				Message: "Too many failed attempts. Try again later.",
				Details: map[string]any{"retry_after_seconds": retry},
			}})
			return
		case errors.Is(err, login.ErrInvalidCredentials):
			writeError(w, http.StatusUnauthorized, "invalid_credentials", "Email or password is incorrect.")
			return
		default:
			h.logger.Error("login failed", slog.String("error", err.Error()))
			writeError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
			return
		}
	}

	writeJSON(w, http.StatusOK, tokenPairResponse{
		AccessToken:      tokens.AccessToken,
		TokenType:        "Bearer",
		ExpiresIn:        tokens.ExpiresIn,
		RefreshToken:     tokens.RefreshToken,
		RefreshExpiresIn: tokens.RefreshExpiresIn,
	})
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// refreshSession rotates the refresh token. Reuse is reported with its own code because the
// client must stop retrying and log in again: the family is already dead.
func (h *authHandlers) refreshSession(w http.ResponseWriter, r *http.Request) {
	var body refreshRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "The request body is not valid JSON.")
		return
	}

	meta := session.Meta{UserAgent: r.UserAgent(), IP: clientIP(r)}

	tokens, err := h.rotator.Refresh(r.Context(), strings.TrimSpace(body.RefreshToken), meta)
	if err != nil {
		switch {
		case errors.Is(err, session.ErrRefreshReused):
			writeError(w, http.StatusUnauthorized, "refresh_reused", "This session was revoked because the token was reused.")
			return
		case errors.Is(err, session.ErrInvalidRefresh):
			writeError(w, http.StatusUnauthorized, "refresh_invalid", "The session is no longer valid. Log in again.")
			return
		default:
			h.logger.Error("refresh failed", slog.String("error", err.Error()))
			writeError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
			return
		}
	}

	writeJSON(w, http.StatusOK, tokenPairResponse{
		AccessToken:      tokens.AccessToken,
		TokenType:        "Bearer",
		ExpiresIn:        tokens.ExpiresIn,
		RefreshToken:     tokens.RefreshToken,
		RefreshExpiresIn: tokens.RefreshExpiresIn,
	})
}

// logoutSession revokes the presented family and answers 204 even when the token is unknown:
// the desired state is reached either way, and an error would confirm whether it existed.
func (h *authHandlers) logoutSession(w http.ResponseWriter, r *http.Request) {
	var body refreshRequest
	if r.Body != nil {
		_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&body)
	}

	if err := h.rotator.Logout(r.Context(), strings.TrimSpace(body.RefreshToken)); err != nil {
		h.logger.Error("logout failed", slog.String("error", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
