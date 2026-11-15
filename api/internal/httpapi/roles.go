package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/ivvanare/ecosystem_complete/api/internal/identity"
)

// RoleAdmin is the authoring surface of RBAC. It is separate from AdminReader because authoring a
// role is a different capability from assigning one: the seeds give role.manage to SUPERADMIN
// alone, and ADMIN "no cambia roles ni permisos".
type RoleAdmin interface {
	CreateRole(ctx context.Context, actorID string, in identity.RoleInput) (identity.Role, error)
	Role(ctx context.Context, roleID string) (identity.Role, error)
	UpdateRole(ctx context.Context, actorID, roleID string, in identity.RoleUpdate) (identity.Role, error)
	RetireRole(ctx context.Context, actorID, roleID string) error
	ReplaceRolePermissions(ctx context.Context, actorID, roleID string, codes []string) ([]string, error)
}

// roleHandlers serves the role-authoring routes.
type roleHandlers struct {
	service RoleAdmin
	logger  *slog.Logger
}

type roleRequest struct {
	Name        string   `json:"name"`
	Description *string  `json:"description"`
	Permissions []string `json:"permissions"`
}

// rolePatchRequest carries an explicit null for description, which a **string cannot: encoding/json
// turns both an omitted field and a null into a nil outer pointer, so "clear it deliberately" would
// be dead code. RawMessage keeps the three states apart by hand.
type rolePatchRequest struct {
	Name        *string         `json:"name"`
	Description json.RawMessage `json:"description"`
	Status      *string         `json:"status"`
}

type rolePermissionsRequest struct {
	// A pointer, because the contract declares `codes` required: a missing key and a null both
	// decode to a nil slice, and treating that as "the empty set" wipes a role's permissions.
	Codes *[]string `json:"codes"`
}

func roleShape(role identity.Role) roleResponse {
	permissions := role.Permissions
	if permissions == nil {
		permissions = []string{}
	}
	return roleResponse{
		ID: role.ID, Name: role.Name, Description: role.Description,
		Status: role.Status, Permissions: permissions,
	}
}

// createRole serves POST /v1/admin/roles.
func (h *roleHandlers) createRole(w http.ResponseWriter, r *http.Request) {
	actorID, ok := userIDFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication is required.")
		return
	}
	var body roleRequest
	if !decodeJSON(w, r, 8<<10, &body) {
		return
	}
	role, err := h.service.CreateRole(r.Context(), actorID, identity.RoleInput{
		Name: body.Name, Description: body.Description, Permissions: body.Permissions,
	})
	if err != nil {
		h.writeRoleError(w, err, "creating a role")
		return
	}
	writeJSON(w, http.StatusCreated, roleShape(role))
}

// readRole serves GET /v1/admin/roles/{coRole}.
func (h *roleHandlers) readRole(w http.ResponseWriter, r *http.Request) {
	roleID, ok := roleParam(w, r)
	if !ok {
		return
	}
	role, err := h.service.Role(r.Context(), roleID)
	if err != nil {
		h.writeRoleError(w, err, "reading a role")
		return
	}
	writeJSON(w, http.StatusOK, roleShape(role))
}

// updateRole serves PATCH /v1/admin/roles/{coRole}.
func (h *roleHandlers) updateRole(w http.ResponseWriter, r *http.Request) {
	actorID, ok := userIDFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication is required.")
		return
	}
	roleID, ok := roleParam(w, r)
	if !ok {
		return
	}
	var body rolePatchRequest
	if !decodeJSON(w, r, 8<<10, &body) {
		return
	}
	description, ok := described(body.Description)
	if !ok {
		writeValidationError(w, []fieldError{{Field: "description", Code: "invalid"}})
		return
	}
	role, err := h.service.UpdateRole(r.Context(), actorID, roleID, identity.RoleUpdate{
		Name: body.Name, Description: description, Status: body.Status,
	})
	if err != nil {
		h.writeRoleError(w, err, "updating a role")
		return
	}
	writeJSON(w, http.StatusOK, roleShape(role))
}

// retireRole serves DELETE /v1/admin/roles/{coRole}.
func (h *roleHandlers) retireRole(w http.ResponseWriter, r *http.Request) {
	actorID, ok := userIDFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication is required.")
		return
	}
	roleID, ok := roleParam(w, r)
	if !ok {
		return
	}
	if err := h.service.RetireRole(r.Context(), actorID, roleID); err != nil {
		h.writeRoleError(w, err, "retiring a role")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// replacePermissions serves PUT /v1/admin/roles/{coRole}/permissions: the whole composition.
func (h *roleHandlers) replacePermissions(w http.ResponseWriter, r *http.Request) {
	actorID, ok := userIDFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication is required.")
		return
	}
	roleID, ok := roleParam(w, r)
	if !ok {
		return
	}
	var body rolePermissionsRequest
	if !decodeJSON(w, r, 8<<10, &body) {
		return
	}
	if body.Codes == nil {
		writeValidationError(w, []fieldError{{Field: "codes", Code: "required"}})
		return
	}
	if _, err := h.service.ReplaceRolePermissions(r.Context(), actorID, roleID, *body.Codes); err != nil {
		h.writeRoleError(w, err, "replacing a role's permissions")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// described turns the three states of the description field into the domain's shape: absent means
// "leave it alone" (nil), null means "clear it" (a pointer to a nil string), and a string means set.
func described(raw json.RawMessage) (**string, bool) {
	if len(raw) == 0 {
		return nil, true
	}
	if string(raw) == "null" {
		var cleared *string
		return &cleared, true
	}
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return nil, false
	}
	value := &text
	return &value, true
}

// roleParam validates the path id the same way every other admin route does.
func roleParam(w http.ResponseWriter, r *http.Request) (string, bool) {
	roleID := r.PathValue("coRole")
	if !uuidPattern.MatchString(roleID) {
		writeValidationError(w, []fieldError{{Field: "coRole", Code: "invalid"}})
		return "", false
	}
	return roleID, true
}

// writeRoleError maps the domain outcomes onto the documented responses. The two 409s are distinct
// on purpose: "it is in use" tells the operator to move people, "it is protected" tells them the
// system will not allow it at all.
func (h *roleHandlers) writeRoleError(w http.ResponseWriter, err error, action string) {
	var inUse *identity.RoleInUseError
	var unknown *identity.UnknownPermissionError
	switch {
	case errors.Is(err, identity.ErrRoleNotFound):
		writeError(w, http.StatusNotFound, "not_found", "Role not found.")
	case errors.Is(err, identity.ErrRoleProtected):
		writeJSON(w, http.StatusConflict, errorEnvelope{Error: errorBody{
			Code:    "role_protected",
			Message: "The system role cannot be changed this way.",
		}})
	case errors.As(err, &inUse):
		writeJSON(w, http.StatusConflict, errorEnvelope{Error: errorBody{
			Code:    "role_in_use",
			Message: "The role still has users; move them before retiring it.",
			Details: map[string]any{"users": inUse.Users},
		}})
	case errors.Is(err, identity.ErrRoleNameTaken):
		writeValidationError(w, []fieldError{{Field: "name", Code: "duplicate"}})
	case errors.Is(err, identity.ErrRoleNameRequired):
		writeValidationError(w, []fieldError{{Field: "name", Code: "required"}})
	case errors.Is(err, identity.ErrRoleNameTooLong):
		writeValidationError(w, []fieldError{{Field: "name", Code: "too_long"}})
	case errors.Is(err, identity.ErrRoleManageRequired):
		// Assigning a role that carries role.manage requires holding it: otherwise the separation
		// between admin.access and role.manage is one request away from being bypassed.
		writeError(w, http.StatusForbidden, "permission_denied", "Granting role.manage requires role.manage.")
	case errors.Is(err, identity.ErrInvalidRoleStatus):
		writeValidationError(w, []fieldError{{Field: "status", Code: "invalid"}})
	case errors.As(err, &unknown):
		writeValidationError(w, []fieldError{{Field: "permissions", Code: "unknown"}})
	case errors.Is(err, identity.ErrInvalidRolePermissions):
		writeValidationError(w, []fieldError{{Field: "permissions", Code: "invalid"}})
	default:
		h.logger.Error(action, slog.String("error", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
	}
}
