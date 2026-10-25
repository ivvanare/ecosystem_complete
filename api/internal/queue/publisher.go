// Package queue publishes messages to RabbitMQ. It is the first real use of the broker:
// until now the API only pinged it for readiness.
package queue

import (
	"context"
	"fmt"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// Publisher sends one message to a durable queue.
type Publisher interface {
	Publish(ctx context.Context, payload []byte) error
}

// AMQP publishes to a single durable queue, connecting lazily so a broker restart does
// not leave the process with a dead handle forever.
type AMQP struct {
	URL   string
	Queue string

	mu   sync.Mutex
	conn *amqp.Connection
	ch   *amqp.Channel
}

// NewAMQP returns a publisher for the given broker URL and queue name.
func NewAMQP(url, queueName string) *AMQP {
	return &AMQP{URL: url, Queue: queueName}
}

// Publish declares the queue if needed and publishes a persistent message.
func (a *AMQP) Publish(ctx context.Context, payload []byte) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	ch, err := a.channel()
	if err != nil {
		return err
	}

	if _, err := ch.QueueDeclare(a.Queue, true, false, false, false, nil); err != nil {
		a.reset()
		return fmt.Errorf("queue: declaring %s: %w", a.Queue, err)
	}

	err = ch.PublishWithContext(ctx, "", a.Queue, false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Timestamp:    time.Now(),
		Body:         payload,
	})
	if err != nil {
		a.reset()
		return fmt.Errorf("queue: publishing to %s: %w", a.Queue, err)
	}

	return nil
}

// Close releases the broker connection.
func (a *AMQP) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.reset()
	return nil
}

func (a *AMQP) channel() (*amqp.Channel, error) {
	if a.ch != nil && !a.conn.IsClosed() {
		return a.ch, nil
	}

	conn, err := amqp.Dial(a.URL)
	if err != nil {
		return nil, fmt.Errorf("queue: dialing broker: %w", err)
	}
	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("queue: opening channel: %w", err)
	}

	a.conn, a.ch = conn, ch
	return ch, nil
}

func (a *AMQP) reset() {
	if a.ch != nil {
		_ = a.ch.Close()
	}
	if a.conn != nil {
		_ = a.conn.Close()
	}
	a.ch, a.conn = nil, nil
}
