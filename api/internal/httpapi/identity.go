package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/ivvanare/ecosystem_complete/api/internal/identity"
)

// IdentityReader serves the caller's profile and the RBAC catalogues.
type IdentityReader interface {
	Me(ctx context.Context, userID string) (identity.Me, error)
	Roles(ctx context.Context) ([]identity.Role, error)
	Permissions(ctx context.Context) ([]identity.Permission, error)
}

// AdminPermission is what opens the backoffice. A user reaches it because a role grants this
// code, never because the record is "of type admin".
const AdminPermission = "admin.access"

type meResponse struct {
	ID              string     `json:"id"`
	Name            *string    `json:"name"`
	Email           *string    `json:"email"`
	Status          string     `json:"status"`
	EmailVerifiedAt *time.Time `json:"email_verified_at"`
	Roles           []string   `json:"roles"`
	Permissions     []string   `json:"permissions"`
	CreatedAt       time.Time  `json:"created_at"`
}

type roleResponse struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description *string  `json:"description"`
	Status      string   `json:"status"`
	Permissions []string `json:"permissions"`
}

type permissionResponse struct {
	ID     string `json:"id"`
	Code   string `json:"code"`
	Module string `json:"module"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

type identityHandlers struct {
	reader IdentityReader
}

// me returns the profile together with the effective roles and permissions. The panel decides
// what to show from `permissions`; it must not infer capability from a role name.
func (h *identityHandlers) me(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication is required.")
		return
	}

	me, err := h.reader.Me(r.Context(), userID)
	if err != nil {
		if errors.Is(err, identity.ErrNotFound) {
			// The token is valid but the account is gone: 401, not 404.
			writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication is required.")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
		return
	}

	writeJSON(w, http.StatusOK, meResponse{
		ID:              me.ID,
		Name:            me.Name,
		Email:           me.Email,
		Status:          me.Status,
		EmailVerifiedAt: me.EmailVerifiedAt,
		Roles:           me.Roles,
		Permissions:     me.Permissions,
		CreatedAt:       me.CreatedAt,
	})
}

func (h *identityHandlers) roles(w http.ResponseWriter, r *http.Request) {
	roles, err := h.reader.Roles(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
		return
	}

	data := make([]roleResponse, 0, len(roles))
	for _, role := range roles {
		data = append(data, roleResponse{
			ID:          role.ID,
			Name:        role.Name,
			Description: role.Description,
			Status:      role.Status,
			Permissions: role.Permissions,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{"data": data})
}

func (h *identityHandlers) permissions(w http.ResponseWriter, r *http.Request) {
	permissions, err := h.reader.Permissions(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
		return
	}

	data := make([]permissionResponse, 0, len(permissions))
	for _, permission := range permissions {
		data = append(data, permissionResponse{
			ID:     permission.ID,
			Code:   permission.Code,
			Module: permission.Module,
			Name:   permission.Name,
			Status: permission.Status,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{"data": data})
}
