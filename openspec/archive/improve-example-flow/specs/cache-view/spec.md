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
