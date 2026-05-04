# Tasks: Improve Example Flow Visualization

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines | 350-450 |
| 400-line budget risk | Medium |
| Chained PRs recommended | Yes |
| Suggested split | PR 1: Rename classes (T01-T04) → PR 2: Dashboard + routes (T05-T08) |
| Delivery strategy | auto-chain |
| Chain strategy | stacked-to-main |

Decision needed before apply: No
Chained PRs recommended: Yes
Chain strategy: stacked-to-main
400-line budget risk: Medium

### Suggested Work Units

| Unit | Goal | Likely PR | Notes |
|------|------|-----------|-------|
| 1 | Rename event + listener to English, update all references | PR 1 | Tests + rename + autoload |
| 2 | Dashboard Blade view + routes + POST support | PR 2 | View, routes, JS interactivity |

## Phase 1: Foundation — Class Renaming (TDD)

- [ ] 1.1 Write failing test: `tests/Unit/OperationPerformedTest.php` — `test_event_has_dispatched_at_property`. Create `OperationPerformed` class (empty) to make test compile, then assert `dispatched_at` is set after construction.
- [ ] 1.2 Rename `app/Events/OperacionRealizada.php` → `app/Events/OperationPerformed.php`. Update class name, namespace, `dispatched_at` property, constructor, `broadcastWith()`. Update `routes/web.php` import + usage.
- [ ] 1.3 Write failing test: `tests/Unit/SendNotificationTest.php` — `test_listener_handles_operation_performed_event`. Create `SendNotification` class (empty) implementing `ShouldQueue`.
- [ ] 1.4 Rename `app/Listeners/EnviarNotificacion.php` → `app/Listeners/SendNotification.php`. Update class name, import `OperationPerformed`, `handle()` signature. Update `routes/web.php` comment. Run `composer dump-autoload`.

## Phase 2: Core Implementation — Routes + Timestamps (TDD)

- [ ] 2.1 Write failing test: `tests/Feature/DashboardTest.php` — `test_dashboard_route_returns_200`. Implement: add `GET /dashboard` route in `routes/web.php` returning `view('dashboard')`.
- [ ] 2.2 Write failing test: `test_post_test_operacion_returns_json`. Implement: enhance `/test-operacion` to accept POST, return JSON with `status`, `queue`, `event_data` (including `dispatched_at`). Keep GET backward compatibility.
- [ ] 2.3 Write failing test: `test_check_cache_returns_enhanced_format`. Implement: update `/check-cache/{store}` to return `last_operation` with `processed_at` + `dispatched_at`, add multi-store summary at `/check-cache`.

## Phase 3: Integration — Dashboard Blade View

- [ ] 3.1 Write failing test: `test_dashboard_shows_tutorial_stages`. Implement: create `resources/views/dashboard.blade.php` with 5-stage tutorial (Paso 1-5), flow diagram, Spanish tutorial text (Rioplatense).
- [ ] 3.2 Write failing test: `test_dashboard_has_trigger_button`. Implement: add "Enviar Notificación" button with vanilla JS POST to `/test-operacion` (random amount/store), page reload after response.
- [ ] 3.3 Write failing test: `test_dashboard_shows_last_operations`. Implement: dashboard reads Redis cache (`last_operation:{store}`) via route closure, displays last 5 ops with timestamps. Fallback to Laravel log if Redis fails.
- [ ] 3.4 Write failing test: `test_dashboard_shows_queue_status`. Implement: queue health check in `/dashboard` route using `PhpAmqpLib\Connection\AMQPStreamConnection` try/catch. Display "Online"/"Offline" + queue name `operacion.realizada`.

## Phase 4: Verification — Full Test Suite

- [ ] 4.1 Run `composer test` — all tests pass (Unit + Feature). Fix any failures.
- [ ] 4.2 Verify backward compatibility: `GET /test-operacion` still returns text response (not JSON).
- [ ] 4.3 Manual smoke test: Docker services up, visit `/dashboard`, click button, verify flow.

## Phase 5: Cleanup

- [ ] 5.1 Update comments in `OperationPerformed.php` and `SendNotification.php` to English (class PHPDoc already updated in 1.2/1.4).
- [ ] 5.2 Remove any `dd()`/`dump()` debug leftovers. Run `composer pint` for style.
