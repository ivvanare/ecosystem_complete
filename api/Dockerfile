# syntax=docker/dockerfile:1

# Build context is the REPOSITORY ROOT: the runtime image ships database/init so it
# does not depend on a host bind mount (whose permissions can deny the container user).

# --- Build stage: static binary, no toolchain in the final image ---
FROM golang:1.27-alpine AS build

WORKDIR /src

# Dependencies first so the module cache survives source changes.
COPY api/go.mod api/go.sum ./
RUN go mod download

COPY api/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api \
 && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/worker ./cmd/worker \
 && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/bootstrap ./cmd/bootstrap

# --- Runtime stage ---
FROM alpine:3

RUN apk add --no-cache ca-certificates \
    && addgroup -S app \
    && adduser -S -G app app

COPY --from=build /out/api /usr/local/bin/api
COPY --from=build /out/worker /usr/local/bin/worker
COPY --from=build /out/bootstrap /usr/local/bin/bootstrap

# Canonical schema. Copied in, never bind-mounted: a host directory the container user
# cannot read (mode 700, other uid) would otherwise abort startup.
COPY database/init /app/database/init

USER app
EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/api"]
