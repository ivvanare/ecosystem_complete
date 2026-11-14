# Proposal: Security API Contract

## Intent

The security module (`tb_user`, sessions, RBAC, audit) is the next block of work, and the
admin panel under `gui/` is being built in another session at the same time. Without a
written contract both sides invent the same things differently: the shape of an error, how
the token travels, what the panel may assume about permissions.

This change writes the contract and nothing else. It is the cheapest artifact that keeps
two parallel tracks compatible.

## Scope

### In Scope
- `api/openapi.yaml` (OpenAPI 3.1): health, auth, identity and admin RBAC read/write
  surface — 19 operations, 11 schemas, one error envelope.
- `spec.md`: the decisions a schema cannot express (token transport, rotation, anti
  enumeration, lockout, audit rules) as numbered requirements.
- Aligning the one existing endpoint that contradicts the contract: the 404 handler now
  returns the envelope instead of a bare string.

### Out of Scope
- Implementing any endpoint. This change produces no handler, no migration, no seed.
- Endpoints for `core` (places, menus, reviews, routes). The panel will need them, but
  they belong to their own contract.
- The choice of how the panel authenticates in production (bearer now; httpOnly cookie if
  the backoffice ends up server-rendered). Recorded as open, not decided.
- Rate limiting placement: edge or Go middleware. Recorded as open.

## Capabilities

### New Capabilities
- `security-api-contract`: the request/response contract for identity, sessions, RBAC and
  the audit read endpoint, consumed by the mobile app and the admin panel.

### Modified Capabilities
- `error-contract`: every error, including 404, uses `{"error":{"code","message"}}`.
