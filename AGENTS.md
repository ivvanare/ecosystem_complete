# AGENTS.md — melZone

Compact guidance for OpenCode agents working in this repository.

## Stack

- **Runtime**: PHP 8.2 (FPM Alpine) with Laravel 12
- **Database**: PostgreSQL 15 (two databases: `coreapp`, `apiseg`)
- **Message Broker**: RabbitMQ 3 with management plugin
- **Cache**: Redis 7
- **Web Server**: Nginx Alpine (proxies to FPM on port 9000)
- **Frontend**: Vite (dev mode) + Blade templates

## Repository Structure

```
melZone/
├── api-core/          # Main Laravel application
├── nginx/             # Nginx configuration
├── database/          # PostgreSQL init scripts and data volume
└── docker-compose.yml # Multi-service orchestration
```

This is NOT a monorepo — `api-core` is the only application. All PHP work happens there.

## Developer Commands

Inside `api-core/`:

| Command | Purpose |
|---------|---------|
| `composer test` | Run tests (clears config, then `artisan test`) |
| `composer setup` | Full setup: install deps, migrate, npm build |
| `composer dev` | Concurrent: server + queue listener + logs + vite |

Run single test: `php artisan test --filter=TestName`

## Docker Services

Start everything: `docker-compose up -d`

| Service | Container | Port |
|---------|------------|------|
| API | melzone_api | internal:9000 |
| Nginx | melzone_nginx | 9000 → 80 |
| PostgreSQL | melzone_db | 5432 |
| RabbitMQ | melzone_mq | 5672, 15672 (mgmt) |
| Redis | melzone_redis | 6379 |

RabbitMQ management UI: http://localhost:15672 (admin/admin_pass)

## Testing

- Framework: PHPUnit 11 (via Laravel's `artisan test`)
- Config: `phpunit.xml` uses **SQLite in-memory** for tests (`DB_CONNECTION=sqlite`, `DB_DATABASE=:memory:`)
- Cache set to `array`, sessions to `array`, queues to `sync` during testing
- Test suites: `tests/Unit/` and `tests/Feature/`

## Environment

- Copy `.env.example` to `.env` and run `php artisan key:generate`
- Production-like config in `.env.example`:
  - `QUEUE_CONNECTION=rabbitmq` (not sync)
  - `CACHE_STORE=redis` with `REDIS_CLIENT=phpredis`
  - `SESSION_DRIVER=database`

## Key Architecture Notes

- Queue driver: RabbitMQ via `vladimir-yuldashev/laravel-queue-rabbitmq`
- Redis client: `phpredis` extension (installed via PECL in Dockerfile), NOT Predis
- PostgreSQL init creates two databases: `coreapp` (main) and `apiseg` (security/auth)
- Event example in `routes/web.php`: `OperacionRealizada` dispatches to RabbitMQ
- **Queue + Cache Example**: See `api-core/README.md` for complete documentation
  - Flow: HTTP Request → Event → RabbitMQ Queue → Listener → Redis Cache
  - Test routes: `/test-operacion` (dispatch) and `/check-cache/{store}` (view cached data)

## Code Style

- Laravel Pint for linting (configured in `composer.json`)
- No custom rules detected — uses default Pint preset

## Gotchas

- Windows paths in `docker-compose.yml` use forward slashes (`E:/proyects/...`)
- Dockerfile installs `linux-headers` explicitly to fix `sockets` extension build
- Session and cache use database driver in production (not Redis directly for sessions)
