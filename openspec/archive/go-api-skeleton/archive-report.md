# Archive Report: go-api-skeleton

## Change Summary

**What was done**: Decommissioned the Laravel scaffolding and stood up the Go service that owns
the API: `cmd/api` (HTTP, config, wiring, signals, graceful shutdown), `cmd/worker`, a closed
`internal/` surface, probes, CORS and the Dockerfile.
**Why**: the Laravel side held only scaffolding and a RabbitMQ/Redis demo; the diagram names Go as
the sole owner of the API and of writes, with the mobile app as first consumer.
**How**: `net/http` only — no framework — with dependencies behind interfaces so the suite needs
no PostgreSQL, Redis or RabbitMQ.

## Delivered

- `api/cmd/{api,worker}` and `api/internal/{config,httpapi,db,probe}` at the time; the package set
  has since grown to 30 packages around the same shape (`bootstrap` arrived later, with the first
  administrator command).
- Configuration read from the process environment only, with a required variable failing startup
  by name and no silent fallback.
- `docker-compose.yml` and the image, and the removal of the PHP tooling from the tree.

## Verification

The suite has run without external services since this change and still does: `go test ./...`
green with PostgreSQL, Redis, RabbitMQ and Soketi stopped. The binary builds with `go build ./...`
and `go vet ./...` clean.
