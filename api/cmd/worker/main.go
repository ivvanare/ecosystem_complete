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

	"github.com/redis/go-redis/v9"

	"github.com/ivvanare/ecosystem_complete/api/internal/config"
	"github.com/ivvanare/ecosystem_complete/api/internal/db"
	"github.com/ivvanare/ecosystem_complete/api/internal/invitations"
	"github.com/ivvanare/ecosystem_complete/api/internal/mailer"
	"github.com/ivvanare/ecosystem_complete/api/internal/trending"
	"github.com/ivvanare/ecosystem_complete/api/internal/worker"
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

	schedulerDone := startScheduler(ctx, logger)

	for {
		select {
		case <-ctx.Done():
			logger.Info("shutdown requested, draining")
			shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
			handleDelivery(shutdownCtx, deliveries, sender, logger)
			if schedulerDone != nil {
				select {
				case <-schedulerDone:
				case <-shutdownCtx.Done():
					logger.Warn("scheduler did not stop before the shutdown deadline")
				}
			}
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

// startScheduler wires the periodic-job scheduler from the environment. A missing
// or invalid WORKER_SCHEDULER_INTERVAL disables it with a warning: the worker's
// mail duty must survive a bad config, so this never fails startup. The returned
// channel closes when the scheduler has stopped; nil means no scheduler is running.
func startScheduler(ctx context.Context, logger *slog.Logger) <-chan struct{} {
	interval, err := config.SchedulerInterval(os.Getenv)
	switch {
	case err != nil:
		logger.Warn("scheduler disabled", slog.String("reason", err.Error()))
		return nil
	case interval == 0:
		logger.Info("scheduler disabled: WORKER_SCHEDULER_INTERVAL is not set")
		return nil
	}

	scheduler, err := worker.NewScheduler(interval, logger)
	if err != nil {
		logger.Warn("scheduler disabled", slog.String("reason", err.Error()))
		return nil
	}
	name, job := worker.Heartbeat(logger)
	scheduler.Register(name, job)
	registerTrending(ctx, scheduler, logger)

	done := make(chan struct{})
	go func() {
		defer close(done)
		scheduler.Run(ctx)
	}()
	return done
}

// registerTrending wires the trending producer onto the scheduler.
//
// DATABASE_URL and REDIS_ADDR are required by the API but not by the mail path, so a worker
// without them keeps delivering mail and simply does not rank anything: a missing producer
// degrades the read path to rating order (S28), which is a state, not an outage. Every
// failure here is logged and the scheduler keeps its other jobs.
func registerTrending(ctx context.Context, scheduler *worker.Scheduler, logger *slog.Logger) {
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	redisAddr := strings.TrimSpace(os.Getenv("REDIS_ADDR"))
	if databaseURL == "" || redisAddr == "" {
		logger.Info("trending producer not registered: DATABASE_URL or REDIS_ADDR is unset")
		return
	}

	pool, err := db.Connect(ctx, databaseURL, 3, logger)
	if err != nil {
		logger.Error("trending producer disabled: database unavailable", slog.String("error", err.Error()))
		return
	}

	redisClient := redis.NewClient(&redis.Options{Addr: redisAddr})
	batch := trending.NewBatch(
		trending.NewPostgresLoader(pool),
		trending.NewWriter(redisClient, logger),
		logger,
	)
	scheduler.Register(batch.Name(), func(ctx context.Context) error {
		// The pool and the client are closed with the worker, not per tick.
		if err := batch.Run(ctx); err != nil {
			return err
		}
		return nil
	})
	logger.Info("trending producer registered")

	// Invitation expiry is hygiene, not enforcement: the read predicate already refuses an
	// expired invitation. The job keeps the table honest.
	scheduler.Register("invitation-expiry", invitations.NewSweeper(invitations.NewPostgresStore(pool), logger).Run)
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
