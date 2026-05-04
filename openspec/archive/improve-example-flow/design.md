# Design: Improve Example Flow Visualization

## Technical Approach

Add an interactive Blade dashboard at `/dashboard` that demonstrates the RabbitMQ → Redis flow with tutorial stages, trigger button, and real-time status. Enhance existing routes for POST support and timestamp tracking. Use vanilla JS for interactivity (no framework). Health checks use try/catch on queue/cache connections since RabbitMQ HTTP API isn't available in the Laravel RabbitMQ package.

## Architecture Decisions

| Option | Tradeoff | Decision |
|--------|----------|----------|
| JS framework (Alpine/Vue) for dashboard | More reactive but adds complexity | **Vanilla JS** — keep it simple, match existing stack |
| RabbitMQ HTTP API for health | Accurate but requires extra credentials | **AMQP connection attempt** — use `Queue::connection()` try/catch |
| Store last 5 ops in Redis list | Persistent but needs migration | **Read from Laravel log** — simpler, already has data |
| WebSocket for real-time updates | Instant but complex | **Polling on refresh** — KISS, user clicks button to trigger |

### Decision: Queue Health Check Strategy

**Choice**: Try to open AMQP connection via `PhpAmqpLib\Connection\AMQPStreamConnection`
**Alternatives considered**: HTTP API call to RabbitMQ management (port 15672), queue:work process check
**Rationale**: Management plugin may not be enabled. AMQP connection attempt is the most direct check and matches how Laravel connects.

### Decision: Event Timestamp Tracking

**Choice**: Add `dispatched_at` property to `OperacionRealizada` event class
**Alternatives considered**: Rely on `broadcastWith()` timestamp only, use Laravel's built-in event timing
**Rationale**: Property is serialized to queue, survives async processing. `broadcastWith()` only affects broadcasting, not queue payload.

## Data Flow

```
User clicks "Enviar Notificación"
         │
         ▼
    POST /test-operacion
    (amount, store params)
         │
         ▼
    OperacionRealizada event
    (amount, storeName, dispatched_at)
         │
         ▼
    RabbitMQ Queue: operacion.realizada
    (via ShouldBroadcast + ShouldQueue)
         │
         ▼
    EnviarNotificacion listener
    (processed_at added here)
         │
         ▼
    Redis Cache: last_operation:{store}
    (amount, store, processed_at, TTL 1h)
         │
         ▼
    Dashboard reads cache + log
    (shows last 5 operations)
```

## File Changes

| File | Action | Description |
|------|--------|-------------|
| `routes/web.php` | Modify | Add `/dashboard` route (GET), enhance `/test-operacion` for POST + JSON response |
| `resources/views/dashboard.blade.php` | Create | Interactive Blade dashboard with tutorial stages, button, and status panels |
| `app/Events/OperacionRealizada.php` | Modify | Add `dispatched_at` public property, set in constructor |
| `app/Listeners/EnviarNotificacion.php` | Modify | Ensure `processed_at` is logged consistently (already present) |
| `tests/Feature/DashboardTest.php` | Create | Test dashboard route, POST to /test-operacion, JSON structure |

## Interfaces / Contracts

### Event Payload (OperacionRealizada)

```php
class OperacionRealizada implements ShouldBroadcast
{
    public $amount;
    public $storeName;
    public $dispatched_at;  // NEW: ISO 8601 timestamp

    public function __construct($amount, $storeName)
    {
        $this->amount = $amount;
        $this->storeName = $storeName;
        $this->dispatched_at = now()->toISOString();  // NEW
    }

    public function broadcastWith(): array
    {
        return [
            'monto' => $this->amount,
            'tienda' => $this->storeName,
            'timestamp' => $this->dispatched_at,  // Use property
        ];
    }
}
```

### POST /test-operacion Response JSON

```json
{
    "status": "dispatched",
    "queue": "operacion.realizada",
    "event_data": {
        "amount": 1500.5,
        "store": "Tienda Central",
        "dispatched_at": "2026-05-04T20:30:00.000000Z"
    }
}
```

### Cache Structure (last_operation:{store})

```php
[
    'amount' => 1500.5,
    'store' => 'Tienda Central',
    'processed_at' => '2026-05-04T20:30:05.000000Z',  // Added by listener
    'dispatched_at' => '2026-05-04T20:30:00.000000Z'   // From event
]
```

## Testing Strategy

| Layer | What to Test | Approach |
|-------|-------------|----------|
| Unit | Event carries `dispatched_at` | Create event, assert property is set |
| Feature | GET `/dashboard` returns 200 | `->get('/dashboard')->assertStatus(200)` |
| Feature | POST `/test-operacion` returns JSON | `->postJson('/test-operacion', [...])->assertJsonStructure(...)` |
| Feature | GET `/test-operacion` backward compat | `->get('/test-operacion')->assertSee('Evento disparado')` |

**Note**: Tests use `QUEUE_CONNECTION=sync` and `CACHE_STORE=array` (phpunit.xml), so RabbitMQ/Redis connectivity won't be tested. Test JSON structure and response codes only.

## Queue Health Check Implementation

```php
// In dashboard route handler
$queueStatus = 'unknown';
try {
    $connection = new \PhpAmqpLib\Connection\AMQPStreamConnection(
        config('queue.connections.rabbitmq.hosts.0.host'),
        config('queue.connections.rabbitmq.hosts.0.port'),
        config('queue.connections.rabbitmq.hosts.0.user'),
        config('queue.connections.rabbitmq.hosts.0.password'),
        config('queue.connections.rabbitmq.hosts.0.vhost')
    );
    $queueStatus = $connection->isConnected() ? 'online' : 'offline';
    $connection->close();
} catch (\Exception $e) {
    $queueStatus = 'offline';
}
```

## Error Handling

- **RabbitMQ offline**: Dashboard shows "Queue: Offline" badge, button remains clickable (request will fail gracefully with Laravel's queue error handling)
- **Redis offline**: Dashboard shows "Cache: No disponible", falls back to showing data from Laravel log file
- **Both offline**: Tutorial still visible, button triggers flow but errors shown via Laravel's exception handler

## Tutorial Content (5 Stages)

1. **HTTP Request**: Route `POST /test-operacion`, params `amount` (100-9999) + `store` ("Tienda Central" | "Sucursal Norte")
2. **Event Dispatch**: `OperacionRealizada` event, props `amount` + `storeName` + `dispatched_at`, broadcast channel `dashboard-stats`
3. **RabbitMQ Queue**: Queue `operacion.realizada`, exchange `amq.direct`, routing key from event class
4. **Listener Processing**: `EnviarNotificacion` listener, async via RabbitMQ, stores in Redis + logs to `laravel.log`
5. **Redis Cache**: Key `last_operation:{storeName}`, TTL 1 hour, stores `amount` + `store` + `processed_at` + `dispatched_at`

## Migration / Rollout

No migration required. This is a demo app with no production data. Changes are additive (new route, new view) or backward-compatible (POST support added alongside GET).

## Open Questions

- [ ] Should the dashboard auto-refresh after clicking "Enviar Notificación"? (Recommend: simple page reload via JS)
- [ ] Should we display the RabbitMQ queue name dynamically from config or hardcode `operacion.realizada`? (Recommend: hardcode for tutorial clarity)
