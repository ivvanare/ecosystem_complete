package httpapi

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/ivvanare/ecosystem_complete/api/internal/session"
)

// Cookie and header names. They are part of the contract with the SPA.
const (
	refreshCookieName = "refresh_token"
	csrfCookieName    = "csrf_token"
	csrfHeaderName    = "X-CSRF-Token"
	// Scoping the cookies to the auth paths keeps them off every other request.
	sessionCookiePath = "/v1/auth"
)

// errCSRF means the request carried a session cookie without a matching double-submit
// token. A cross-site caller can send the cookie but cannot read it to echo the header.
var errCSRF = errors.New("httpapi: csrf token missing or mismatched")

// cookieSettings decides how a token pair reaches the client.
//
// Two clients, two transports: the mobile app receives the refresh token in the body and
// stores it in secure storage, while the Angular panel receives it as an httpOnly cookie
// that JavaScript cannot read. A browser is recognised by an Origin that the CORS allowlist
// already trusts — the same list that decides whether the browser can call at all.
type cookieSettings struct {
	allowedOrigins []string
	secure         bool
}

func (c cookieSettings) isBrowser(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	return origin != "" && slices.Contains(c.allowedOrigins, origin)
}

// setSession writes both cookies: the httpOnly refresh token and the readable CSRF token the
// SPA echoes back in a header.
func (c cookieSettings) setSession(w http.ResponseWriter, tokens session.Tokens) error {
	csrf, err := newCSRFToken()
	if err != nil {
		return err
	}

	http.SetCookie(w, &http.Cookie{
		Name:     refreshCookieName,
		Value:    tokens.RefreshToken,
		Path:     sessionCookiePath,
		MaxAge:   tokens.RefreshExpiresIn,
		HttpOnly: true,
		Secure:   c.secure,
		SameSite: http.SameSiteLaxMode,
	})
	http.SetCookie(w, &http.Cookie{
		Name:   csrfCookieName,
		Value:  csrf,
		Path:   sessionCookiePath,
		MaxAge: tokens.RefreshExpiresIn,
		// Readable on purpose: the double-submit pattern needs JavaScript to read it.
		HttpOnly: false,
		Secure:   c.secure,
		SameSite: http.SameSiteLaxMode,
	})

	return nil
}

// clearSession expires both cookies.
func (c cookieSettings) clearSession(w http.ResponseWriter) {
	for _, name := range []string{refreshCookieName, csrfCookieName} {
		http.SetCookie(w, &http.Cookie{
			Name:     name,
			Value:    "",
			Path:     sessionCookiePath,
			MaxAge:   -1,
			HttpOnly: name == refreshCookieName,
			Secure:   c.secure,
			SameSite: http.SameSiteLaxMode,
		})
	}
}

// presented returns the refresh token from the body or from the cookie.
//
// When the cookie is the source, the double-submit token is required: a cookie is attached
// by the browser automatically, so without this a cross-site form post could rotate or end a
// session the user never touched.
func (c cookieSettings) presented(r *http.Request, bodyToken string) (token string, fromCookie bool, err error) {
	if token = strings.TrimSpace(bodyToken); token != "" {
		return token, false, nil
	}

	cookie, cookieErr := r.Cookie(refreshCookieName)
	if cookieErr != nil || cookie.Value == "" {
		return "", false, nil
	}

	csrfCookie, csrfErr := r.Cookie(csrfCookieName)
	if csrfErr != nil || csrfCookie.Value == "" {
		return "", true, errCSRF
	}
	header := r.Header.Get(csrfHeaderName)
	if subtle.ConstantTimeCompare([]byte(header), []byte(csrfCookie.Value)) != 1 {
		return "", true, errCSRF
	}

	return cookie.Value, true, nil
}

func newCSRFToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
