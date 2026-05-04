# Proposal: Improve Example Flow Visualization

## Intent

Make the RabbitMQ + Redis flow OBVIOUS and educational. Currently, the event → queue → listener → cache flow works but isn't visually demonstrated. Users can't **see** the data moving through the system without checking RabbitMQ UI and Redis separately.

## Scope

### In Scope
- Add a simple dashboard route (`/dashboard`) showing real-time flow status
- Enhance `/test-operacion` to return JSON with event details + RabbitMQ queue name
- Enhance `/check-cache/{store}` with clearer visualization of cached data
- Add timestamp tracking at each stage (dispatched_at, processed_at)
- Log to Laravel log (already in listener) + display last 5 operations on dashboard

### Out of Scope
- Authentication or access control for the dashboard
- WebSocket broadcasting to dashboard (keep it simple: polling/refresh)
- Changing RabbitMQ or Redis configuration
- Adding more queue drivers or cache backends

## Capabilities

### New Capabilities
- `flow-dashboard`: Web dashboard displaying the HTTP → RabbitMQ → Redis flow with real-time status

### Modified Capabilities
- `event-dispatch`: Enhance `/test-operacion` route to return structured JSON with event metadata
- `cache-view`: Improve `/check-cache/{store}` response format and add multi-store summary

## Approach

1. **Dashboard route** (`/dashboard`): Blade view showing:
   - Last 5 operations from Redis cache (with timestamps)
   - Queue health check (RabbitMQ connection status)
   - Flow diagram: HTTP → Event → Queue → Listener → Cache

2. **Enhanced dispatch route**: Return JSON with event ID, queue name, and expected cache key

3. **Timestamp tracking**: Add `dispatched_at` to event broadcast, keep `processed_at` in cache

4. **Log aggregation**: Read Laravel log for last 5 operations and display on dashboard

## Affected Areas

| Area | Impact | Description |
|------|--------|-------------|
| `routes/web.php` | Modified | Add `/dashboard` route, enhance existing routes |
| `resources/views/dashboard.blade.php` | New | Simple Blade dashboard template |
| `app/Events/OperacionRealizada.php` | Modified | Add `dispatched_at` timestamp |
| `app/Listeners/EnviarNotificacion.php` | Modified | Keep `processed_at`, ensure consistent logging |

## Risks

| Risk | Likelihood | Mitigation |
|------|------------|------------|
| RabbitMQ not running when dashboard loads | Med | Show "Queue: Offline" gracefully |
| Redis connection fails | Low | Cache dashboard data with fallback message |
| Dashboard adds cognitive overhead | Low | Keep it minimal: one Blade view, no JS framework |

## Rollback Plan

```bash
# Remove dashboard route from web.php
# Delete resources/views/dashboard.blade.php
# Revert event/listener changes (remove dispatched_at)
git checkout -- api-core/routes/web.php api-core/app/Events/OperacionRealizada.php api-core/app/Listeners/EnviarNotificacion.php
rm -f api-core/resources/views/dashboard.blade.php
```

## Dependencies

- Laravel 12 (already installed)
- RabbitMQ must be accessible for queue status check
- Redis must be accessible for cache display

## Success Criteria

- [ ] `/dashboard` route shows last 5 operations from Redis
- [ ] Flow diagram visible on dashboard (HTTP → Event → Queue → Listener → Cache)
- [ ] `/test-operacion` returns JSON with event details
- [ ] `/check-cache/{store}` displays data with clear timestamps
- [ ] Dashboard handles RabbitMQ/Redis offline gracefully
- [ ] All existing tests still pass (`composer test`)
