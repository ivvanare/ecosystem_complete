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
