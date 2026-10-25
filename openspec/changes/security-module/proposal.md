# Proposal: Security Module

## Intent

Implement the identity, session, RBAC and audit surface that `security-api-contract`
already fixed. The contract is the spec; this change is the code behind it.

## Scope

### In Scope
- CORS middleware with credentials and an auth-free preflight (slice 0 of the contract).
- Seed data: the permission catalogue and the base roles (SUPERADMIN, ADMIN, EDITOR).
- Then, in order: signup with code, login with lockout, refresh rotation with family
  revocation, password reset/change, RBAC middleware and `/v1/me`, email change, audit
  writes, social login.

### Out of Scope
- Domain endpoints (`core`: places, menus, reviews, routes).
- Rate limiting placement (edge or Go middleware) — still open in C12.
- Mail transport choice: the worker will publish to RabbitMQ, but the SMTP provider is
  not chosen yet.

## Capabilities

### New Capabilities
- `security-module`: authentication, session lifecycle, authorization and audit.

### Modified Capabilities
- `http-surface`: CORS becomes part of the server contract for the SPA panel.
