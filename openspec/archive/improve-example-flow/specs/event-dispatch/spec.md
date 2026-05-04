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
