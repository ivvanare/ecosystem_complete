# melZone Ecosystem

Infrastructure-first Laravel 12 application with Docker-based multi-service architecture.

## Stack

| Component | Technology |
|-----------|-------------|
| Runtime | PHP 8.2 (FPM Alpine) |
| Framework | Laravel 12 |
| Database | PostgreSQL 15 (two databases: `coreapp`, `apiseg`) |
| Message Broker | RabbitMQ 3 with management plugin |
| Cache | Redis 7 |
| Web Server | Nginx Alpine |

## Quick Start

### Prerequisites
- Docker & Docker Compose
- Git

### 1. Clone the repository
```bash
git clone https://github.com/ivvanare/ecosystem_complete.git
cd ecosystem_complete
```

### 2. Start all services
```bash
docker-compose up -d
```

This starts:
- **melzone_api** (PHP-FPM) - internal port 9000
- **melzone_nginx** - port 9000 → 80
- **melzone_db** (PostgreSQL) - port 5432
- **melzone_mq** (RabbitMQ) - ports 5672, 15672 (management UI)
- **melzone_redis** - port 6379

### 3. Setup Laravel
```bash
cd api-core
cp .env.example .env
php artisan key:generate
composer install
php artisan migrate
```

### 4. Test the Queue + Cache example
```bash
# Dispatch event to RabbitMQ
curl http://localhost:9000/test-operacion

# Check cached data in Redis
curl http://localhost:9000/check-cache/Tienda%20Central
```

---

## Repository Structure

```
melZone/
├── api-core/          # Main Laravel 12 application
│   ├── app/
│   │   ├── Events/
│   │   │   └── OperacionRealizada.php
│   │   └── Listeners/
│   │       └── EnviarNotificacion.php
│   ├── routes/
│   │   └── web.php
│   └── README.md      # Detailed Queue+Cache documentation
├── nginx/             # Nginx configuration
│   └── default.conf
├── database/          # PostgreSQL init scripts
│   └── init-db-postgres.sql
├── docker-compose.yml # Multi-service orchestration
├── AGENTS.md         # Guidelines for AI agents
└── README.md         # This file
```

---

## Queue + Cache Example

The project includes a working example of **RabbitMQ → Redis** communication:

```
HTTP Request → Event Dispatch → RabbitMQ Queue → Listener → Redis Cache
```

**Files involved:**
- `api-core/app/Events/OperacionRealizada.php` - The event
- `api-core/app/Listeners/EnviarNotificacion.php` - Queue listener that caches
- `api-core/routes/web.php` - Test routes

For full documentation, see **[api-core/README.md](api-core/README.md)**.

---

## Developer Commands

All commands should be run inside `api-core/`:

| Command | Purpose |
|---------|---------|
| `composer test` | Run tests (uses SQLite in-memory) |
| `composer setup` | Full setup: deps + migrate + build |
| `composer dev` | Dev server + queue listener + logs + vite |

### Running Queue Worker
```bash
php artisan queue:work rabbitmq --tries=3
```

---

## Testing

- **Framework**: PHPUnit 11 (via `artisan test`)
- **Database**: SQLite in-memory (`DB_CONNECTION=sqlite` in `phpunit.xml`)
- **Cache**: Array driver (tests don't touch Redis)
- **Queue**: Sync driver (jobs run immediately in tests)

```bash
# All tests
composer test

# Single test
php artisan test --filter=TestName
```

---

## RabbitMQ Management UI

Access the management interface at:
```
http://localhost:15672
```

**Credentials:**
- Username: `admin`
- Password: `admin_pass`

Use this to monitor queue health, message rates, and failed jobs.

---

## Key Architecture Notes

- **Queue driver**: RabbitMQ via `vladimir-yuldashev/laravel-queue-rabbitmq`
- **Redis client**: `phpredis` extension (installed via PECL in Dockerfile), NOT Predis
- **Two PostgreSQL databases**: `coreapp` (main), `apiseg` (security/auth)
- **Sessions**: Stored in database (not Redis directly)
- **Testing**: Isolated from production services (SQLite, array cache, sync queue)

---

## Environment Configuration

Copy `.env.example` to `.env` and configure:
- `DB_CONNECTION=pgsql` with your PostgreSQL credentials
- `QUEUE_CONNECTION=rabbitmq` for async processing
- `CACHE_STORE=redis` with `REDIS_CLIENT=phpredis`

---

## Links

- **Repository**: https://github.com/ivvanare/ecosystem_complete
- **Laravel Docs**: https://laravel.com/docs
- **RabbitMQ Docs**: https://www.rabbitmq.com/documentation.html
- **Redis Docs**: https://redis.io/docs/
