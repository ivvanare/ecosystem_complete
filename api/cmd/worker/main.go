// Command worker consumes mail messages from RabbitMQ and delivers them over SMTP.
//
// It is the second half of the signup flow: the API publishes, this process sends. Keeping
// it separate means a slow or failing mail server cannot block an HTTP request.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/ivvanare/ecosystem_complete/api/internal/config"
	"github.com/ivvanare/ecosystem_complete/api/internal/mailer"
)

const (
	defaultPrefetch = 10
	shutdownTimeout = 10 * time.Second
)

func main() {
	if err := run(); err != nil {
		slog.Error("worker stopped", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

func run() error {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: level(os.Getenv("LOG_LEVEL")),
	}))
	slog.SetDefault(logger)

	brokerURL := strings.TrimSpace(os.Getenv("RABBITMQ_URL"))
	if brokerURL == "" {
		return fmt.Errorf("missing required environment variable: RABBITMQ_URL")
	}

	queueName := valueOr("MAIL_QUEUE", config.DefaultMailQueue)
	sender := mailer.New(valueOr("SMTP_ADDR", config.DefaultSMTPAddr), valueOr("MAIL_FROM", config.DefaultMailFrom))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	conn, err := amqp.Dial(brokerURL)
	if err != nil {
		return fmt.Errorf("dialing broker: %w", err)
	}
	defer func() { _ = conn.Close() }()

	channel, err := conn.Channel()
	if err != nil {
		return fmt.Errorf("opening channel: %w", err)
	}
	defer func() { _ = channel.Close() }()

	if _, err := channel.QueueDeclare(queueName, true, false, false, false, nil); err != nil {
		return fmt.Errorf("declaring queue %s: %w", queueName, err)
	}
	if err := channel.Qos(defaultPrefetch, 0, false); err != nil {
		return fmt.Errorf("setting prefetch: %w", err)
	}

	deliveries, err := channel.Consume(queueName, "", false, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("consuming %s: %w", queueName, err)
	}

	logger.Info("worker listening", slog.String("queue", queueName), slog.String("smtp", sender.Addr))

	for {
		select {
		case <-ctx.Done():
			logger.Info("shutdown requested, draining")
			shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
			handleDelivery(shutdownCtx, deliveries, sender, logger)
			cancel()
			logger.Info("worker stopped")
			return nil
		case delivery, ok := <-deliveries:
			if !ok {
				return fmt.Errorf("broker closed the delivery channel")
			}
			handleDelivery(ctx, single(delivery), sender, logger)
		}
	}
}

// single adapts one delivery into a closed channel so the drain path reuses the same code.
func single(delivery amqp.Delivery) <-chan amqp.Delivery {
	channel := make(chan amqp.Delivery, 1)
	channel <- delivery
	close(channel)
	return channel
}

func handleDelivery(ctx context.Context, deliveries <-chan amqp.Delivery, sender *mailer.SMTP, logger *slog.Logger) {
	for delivery := range deliveries {
		var message mailer.Message
		if err := json.Unmarshal(delivery.Body, &message); err != nil {
			logger.Error("dropping unreadable message", slog.String("error", err.Error()))
			_ = delivery.Nack(false, false)
			continue
		}

		if err := sender.Send(ctx, message); err != nil {
			// Not requeued: a message that fails to send usually fails for a reason a
			// retry will not fix (bad address, malformed payload). Requeueing loops.
			logger.Error("mail delivery failed",
				slog.String("error", err.Error()),
				slog.String("subject", message.Subject),
			)
			_ = delivery.Nack(false, false)
			continue
		}

		logger.Info("mail delivered", slog.String("subject", message.Subject))
		_ = delivery.Ack(false)
	}
}

func valueOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func level(raw string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
