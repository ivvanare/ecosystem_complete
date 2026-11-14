# Archive Report: security-api-contract

## Change Summary

**What was done**: Made the HTTP contract machine-readable and enforced: `api/openapi.yaml` for the
contract, `api/API.md` for the consumer guide, and `api/internal/contract` as a **test** that fails
when code and documentation drift.
**Why**: documentation that is maintained by intention drifts within a week; the gate everybody
already runs is `go test ./...`, so the check lives there.
**How**: the test scrapes registered routes from `internal/httpapi` and asserts they exist in
`openapi.yaml` and are mentioned in `API.md` (and that neither documents a ghost); every emitted
error code must be in the catalogue in `API.md` and asserted by at least one test; every
environment variable read must be in `README.md` and `.env.example`.

## Delivered

- `api/openapi.yaml`, `api/API.md`, `api/README.md`, `api/.env.example`.
- `api/internal/contract`: six tests, one per drift direction, plus the schema shape test.
- The rule that an endpoint change is a three-file change by design, recorded in `AGENTS.md`.

## Verification

`go test ./internal/contract/` green today, six of six. It has caught real omissions in later
slices — a route documented and not registered, and an environment variable read and not
documented (`SOKETI_URL`, in the realtime slice) — which is the evidence that the gate is live and
not decorative.
