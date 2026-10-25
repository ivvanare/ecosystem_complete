// Package probe implements the readiness probes for the external dependencies.
package probe

import (
	"context"
	"fmt"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/redis/go-redis/v9"

	"github.com/jackc/pgx/v5/pgxpool"
)

// RabbitMQ dial timeout: a probe must fail fast, not hang the readiness endpoint.
const defaultDialTimeout = 3 * time.Second

// Postgres pings the connection pool. Required: the API cannot serve without it.
type Postgres struct {
	Pool *pgxpool.Pool
}

// Name identifies the dependency in the readiness report.
func (p Postgres) Name() string { return "postgres" }

// Required reports that the API cannot serve without PostgreSQL.
func (p Postgres) Required() bool { return true }

// Check pings PostgreSQL.
func (p Postgres) Check(ctx context.Context) error {
	if err := p.Pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping postgres: %w", err)
	}
	return nil
}

// Redis pings the cache. Required: rate limiting and caching sit on the request path.
type Redis struct {
	Client *redis.Client
}

// Name identifies the dependency in the readiness report.
func (r Redis) Name() string { return "redis" }

// Required reports that the API cannot serve without Redis.
func (r Redis) Required() bool { return true }

// Check pings Redis.
func (r Redis) Check(ctx context.Context) error {
	if err := r.Client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("ping redis: %w", err)
	}
	return nil
}

// RabbitMQ verifies the broker is reachable. Optional: nothing on the request path
// publishes yet, so a broker outage degrades the report without failing readiness.
type RabbitMQ struct {
	URL         string
	DialTimeout time.Duration
}

// Name identifies the dependency in the readiness report.
func (r RabbitMQ) Name() string { return "rabbitmq" }

// Required reports that the API can serve without the broker.
func (r RabbitMQ) Required() bool { return false }

// Check opens and immediately closes a broker connection.
func (r RabbitMQ) Check(context.Context) error {
	timeout := r.DialTimeout
	if timeout <= 0 {
		timeout = defaultDialTimeout
	}

	conn, err := amqp.DialConfig(r.URL, amqp.Config{Dial: amqp.DefaultDial(timeout)})
	if err != nil {
		return fmt.Errorf("dial rabbitmq: %w", err)
	}
	return conn.Close()
}
