# melZone API Core

Laravel 12 application demonstrating Queue (RabbitMQ) + Cache (Redis) communication patterns.

## Project Architecture

### Stack
- **Runtime**: PHP 8.2 (FPM Alpine)
- **Framework**: Laravel 12
- **Database**: PostgreSQL 15 (two databases: `coreapp`, `apiseg`)
- **Message Broker**: RabbitMQ 3 with management plugin
- **Cache**: Redis 7
- **Web Server**: Nginx Alpine (proxies to FPM on port 9000)
- **Frontend**: Vite (dev mode) + Blade templates

### Docker Services

| Service | Container | Port | Description |
|---------|-----------|------|-------------|
| API | melzone_api | internal:9000 | Laravel application (PHP-FPM) |
| Nginx | melzone_nginx | 9000 → 80 | Reverse proxy to FPM |
| PostgreSQL | melzone_db | 5432 | Database server (coreapp + apiseg) |
| RabbitMQ | melzone_mq | 5672, 15672 | Message queue with management UI |
| Redis | melzone_redis | 6379 | Cache server |

### Directory Structure
```
melZone/
├── api-core/          # Main Laravel application
│   ├── app/
│   │   ├── Events/
│   │   │   └── OperacionRealizada.php
│   │   └── Listeners/
│   │       └── EnviarNotificacion.php
│   ├── routes/
│   │   └── web.php
│   └── ...
├── nginx/             # Nginx configuration
├── database/          # PostgreSQL init scripts
└── docker-compose.yml # Multi-service orchestration
```

### Database Configuration
- **coreapp**: Main application database
- **apiseg**: Security/auth database

### Key Architecture Notes
- Queue driver: RabbitMQ via `vladimir-yuldashev/laravel-queue-rabbitmq`
- Redis client: `phpredis` extension (installed via PECL in Dockerfile), NOT Predis
- Session and cache use database driver in production (not Redis directly for sessions)

---

## Queue + Cache Example

This project demonstrates a complete asynchronous processing flow using Laravel's event system with RabbitMQ and Redis.

### Flow Diagram
```
HTTP Request → Event Dispatch → RabbitMQ Queue → Listener → Redis Cache
```

### Files Involved

#### 1. `app/Events/OperacionRealizada.php` - The Event
This event is dispatched when an operation is performed. It contains:
- `amount`: The operation amount (float)
- `storeName`: The store where the operation occurred (string)

The event uses Laravel's `Dispatchable` and `SerializesModels` traits for queue compatibility.

#### 2. `app/Listeners/EnviarNotificacion.php` - The Queue Listener
This listener:
- Implements `ShouldQueue` to be processed asynchronously
- Uses `$connection = "rabbitmq"` to force RabbitMQ as the queue driver
- Processes the event from the RabbitMQ queue
- Stores operation data in Redis using Laravel's Cache facade

Cache operations performed:
- `Cache::put()` - Stores the last operation with 1-hour TTL
- `Cache::increment()` - Increments a counter for total operations per store

#### 3. `routes/web.php` - The Test Routes

**Route 1: `/test-operacion`**
- Dispatches the `OperacionRealizada` event
- Event goes to RabbitMQ queue
- Returns confirmation message

**Route 2: `/check-cache/{store?}`**
- Retrieves cached operation data from Redis
- Shows last operation and total count for a store
- Example: `/check-cache/Tienda Central`

---

## How to Test the Queue + Cache Flow

### Step 1: Start Docker Services
```bash
docker-compose up -d
```

Verify all services are running:
```bash
docker-compose ps
```

### Step 2: Dispatch an Event
Hit the test route to dispatch an event to RabbitMQ:
```
http://localhost:9000/test-operacion
```

Expected response:
```
✅ Evento disparado a RabbitMQ. El listener procesará y guardará en Redis.
```

### Step 3: Verify Queue Processing
Check RabbitMQ management UI:
```
http://localhost:15672
Login: admin / admin_pass
```
- Go to "Queues" tab
- Look for the queue processing the event
- Message should be consumed by the worker

### Step 4: Check Cached Data in Redis
Hit the check-cache route:
```
http://localhost:9000/check-cache/Tienda Central
```

Expected response:
```json
{
  "store": "Tienda Central",
  "last_operation": {
    "amount": 1500.5,
    "store": "Tienda Central",
    "processed_at": "2026-05-01T..."
  },
  "total_operations": 1
}
```

### Step 5: Verify Redis Directly
```bash
docker exec -it melzone_redis redis-cli
> GET last_operation:Tienda Central
> GET operations_count:Tienda Central
```

---

## Developer Commands

All commands should be run inside the `api-core/` directory.

| Command | Purpose |
|---------|---------|
| `composer test` | Run tests (clears config, then `artisan test`) |
| `composer setup` | Full setup: install deps, migrate, npm build |
| `composer dev` | Concurrent: server + queue listener + logs + vite |
| `php artisan test --filter=TestName` | Run a single test |

### Running the Queue Worker
To process jobs from RabbitMQ, you need a queue worker running:
```bash
php artisan queue:work rabbitmq --tries=3
```

---

## Testing

### Framework
- **PHPUnit 11** (via Laravel's `artisan test`)

### Test Configuration
Tests use `phpunit.xml` configuration with:
- **Database**: SQLite in-memory (`DB_CONNECTION=sqlite`, `DB_DATABASE=:memory:`)
- **Cache**: Array driver (`CACHE_STORE=array`)
- **Sessions**: Array driver (`SESSION_DRIVER=array`)
- **Queue**: Sync driver (`QUEUE_CONNECTION=sync`) - jobs run immediately in tests
- **Broadcast**: Null driver (`BROADCAST_CONNECTION=null`)

### Test Suites
- `tests/Unit/` - Unit tests
- `tests/Feature/` - Feature tests

### Running Tests
```bash
# All tests
composer test

# Single test
php artisan test --filter=TestName

# With coverage (if configured)
php artisan test --coverage
```

---

## Code Style

- **Laravel Pint** for linting (configured in `composer.json`)
- Uses default Pint preset (no custom rules)

Run linter:
```bash
./vendor/bin/pint
```

---

## Environment Configuration

### Setup
1. Copy `.env.example` to `.env`:
   ```bash
   cp .env.example .env
   ```

2. Generate application key:
   ```bash
   php artisan key:generate
   ```

### Production-like Configuration (in `.env.example`)
- `QUEUE_CONNECTION=rabbitmq` (not sync)
- `CACHE_STORE=redis` with `REDIS_CLIENT=phpredis`
- `SESSION_DRIVER=database`

---

## Gotchas

- Windows paths in `docker-compose.yml` use forward slashes (`E:/proyects/...`)
- Dockerfile installs `linux-headers` explicitly to fix `sockets` extension build
- Session and cache use database driver in production (not Redis directly for sessions)
- Queue workers need to be running to process RabbitMQ jobs asynchronously

---

## RabbitMQ Management UI

Access the management interface at:
```
http://localhost:15672
```

Credentials:
- Username: `admin`
- Password: `admin_pass`

Use this to:
- Monitor queue health
- See message rates
- Check failed jobs
- Manage queues and exchanges
