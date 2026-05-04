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
