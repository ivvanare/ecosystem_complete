# Specs for improve-example-flow

## Domain: interactive-dashboard

# Interactive Dashboard Specification

## Purpose

Interactive web dashboard that demonstrates the RabbitMQ + Redis flow with tutorial-style visualization and trigger button.

## Requirements

### Requirement: Dashboard Access

The system SHALL provide a `/dashboard` route returning a Blade view with interactive tutorial.

#### Scenario: User visits dashboard

- GIVEN the application is running
- WHEN the user navigates to `/dashboard`
- THEN the dashboard view loads with tutorial stages and "Enviar Notificación" button

### Requirement: Enviar Notificación Button

The system SHALL display a button that sends a POST request to `/test-operacion` with random amount and store name.

#### Scenario: User clicks "Enviar Notificación" button

- GIVEN the dashboard is loaded
- WHEN the user clicks "Enviar Notificación"
- THEN a POST request is sent to `/test-operacion` with random `amount` (100-9999) and `store` ("Tienda Central" or "Sucursal Norte")
- AND the dashboard refreshes showing the dispatched event status

#### Scenario: RabbitMQ is offline

- GIVEN RabbitMQ is not running
- WHEN the user clicks "Enviar Notificación"
- THEN the dashboard shows "Queue: Offline" warning
- AND the button shows a disabled state with explanation

### Requirement: Last Operations Display

The system SHALL display the last 5 operations retrieved from Redis cache with timestamps.

#### Scenario: User views last 5 operations

- GIVEN Redis contains cached operations under keys `last_operation:{store}`
- WHEN the user loads the dashboard
- THEN the last 5 operations are displayed with `store`, `amount`, `processed_at`
- AND operations are sorted by `processed_at` descending

#### Scenario: Redis is offline

- GIVEN Redis connection fails
- WHEN the user loads the dashboard
- THEN a fallback message "Cache: No disponible" is shown
- AND the operations section displays cached data from Laravel log as fallback

### Requirement: Queue Health Check

The system SHALL display RabbitMQ connection status on the dashboard.

#### Scenario: Queue health visible

- GIVEN the dashboard loads
- THEN the queue status shows "Online" or "Offline"
- AND the queue name `operacion.realizada` is displayed

---

## Domain: flow-tutorial

# Flow Tutorial Specification

## Purpose

Educational visualization showing the 5-stage flow of data through the system with technical details at each stage.

## Requirements

### Requirement: Stage-by-Stage Tutorial

The system SHALL display 5 tutorial stages explaining the RabbitMQ + Redis flow with tech details.

#### Scenario: Dashboard shows stage-by-stage tutorial

- GIVEN the dashboard is loaded
- WHEN the user views the tutorial section
- THEN 5 stages are displayed:
  - **Paso 1 - HTTP Request**: Route `/test-operacion`, method POST, params `amount` and `store`
  - **Paso 2 - Event Dispatch**: Event `OperacionRealizada`, properties `amount` + `storeName`, broadcast channel `dashboard-stats`
  - **Paso 3 - RabbitMQ Queue**: Queue name `operacion.realizada`, exchange `amq.direct`, routing key `operacion.realizada`
  - **Paso 4 - Listener Processing**: Listener `EnviarNotificacion`, handles async via RabbitMQ, logs to Laravel log
  - **Paso 5 - Redis Cache**: Cache key `last_operation:{storeName}`, TTL 1 hour, stores `amount`, `store`, `processed_at`

### Requirement: Flow Diagram

The system SHALL render a visual arrow diagram showing the data flow.

#### Scenario: Flow diagram visible

- GIVEN the dashboard is loaded
- WHEN the user views the tutorial
- THEN a diagram is shown: `HTTP → Event → Queue → Listener → Cache`
- AND each stage is clickable to expand tech details

### Requirement: Timestamp Tracking Display

The system SHALL show `dispatched_at` and `processed_at` timestamps at relevant stages.

#### Scenario: Timestamps displayed

- GIVEN an event was dispatched and processed
- WHEN viewing the tutorial or operations list
- THEN `dispatched_at` (from event) and `processed_at` (from cache) are both visible

---

## Domain: event-dispatch (Delta)

# Delta for Event Dispatch

## ADDED Requirements

### Requirement: POST Support for Test Operation

The system SHALL accept POST requests on `/test-operacion` and return JSON with event metadata.

#### Scenario: POST to /test-operacion returns JSON

- GIVEN the route `/test-operacion` receives a POST request
- WHEN the request contains optional `amount` and `store` parameters
- THEN the system dispatches `OperacionRealizada` event
- AND returns JSON with `status: "dispatched"`, `queue: "operacion.realizada"`, `event_data: {amount, store, dispatched_at}`

#### Scenario: GET to /test-operacion (backward compatibility)

- GIVEN the route `/test-operacion` receives a GET request
- WHEN no parameters are provided
- THEN the system behaves as before (dispatches with default values and returns text response)

### Requirement: Dispatched At Timestamp

The system SHALL include `dispatched_at` timestamp in the `OperacionRealizada` event.

#### Scenario: Event carries dispatched_at

- GIVEN the `OperacionRealizada` event is constructed
- WHEN the event is serialized for the queue
- THEN `dispatched_at` is included as an ISO 8601 timestamp

---

## Domain: cache-view (Delta)

# Delta for Cache View

## ADDED Requirements

### Requirement: Enhanced Cache Response Format

The system SHALL return clearer JSON structure from `/check-cache/{store}` with timestamps.

#### Scenario: GET /check-cache/{store} shows timestamps

- GIVEN Redis contains data for `{store}`
- WHEN the user requests `/check-cache/{store}`
- THEN the response includes `last_operation` with `amount`, `store`, `processed_at`
- AND `total_operations` count
- AND `last_updated` timestamp from cache TTL

### Requirement: Multi-Store Cache Summary

The system SHALL provide a summary view when no store is specified.

#### Scenario: GET /check-cache shows multi-store summary

- GIVEN multiple stores have cached data
- WHEN the user requests `/check-cache` without a store parameter
- THEN the response lists all stores with their last operation summary
- AND each entry shows `store`, `last_amount`, `last_processed_at`
