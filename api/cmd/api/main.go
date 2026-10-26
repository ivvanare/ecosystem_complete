// Command api is the melZone HTTP service: the sole owner of the API and of writes.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/ivvanare/ecosystem_complete/api/internal/audit"
	"github.com/ivvanare/ecosystem_complete/api/internal/config"
	"github.com/ivvanare/ecosystem_complete/api/internal/db"
	"github.com/ivvanare/ecosystem_complete/api/internal/httpapi"
	"github.com/ivvanare/ecosystem_complete/api/internal/login"
	"github.com/ivvanare/ecosystem_complete/api/internal/probe"
	"github.com/ivvanare/ecosystem_complete/api/internal/queue"
	"github.com/ivvanare/ecosystem_complete/api/internal/session"
	"github.com/ivvanare/ecosystem_complete/api/internal/signup"
)

// readHeaderTimeout bounds slow-header connections without capping request bodies.
const readHeaderTimeout = 5 * time.Second

func main() {
	if err := run(); err != nil {
		slog.Error("service stopped", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := db.Connect(ctx, cfg.DatabaseURL, db.DefaultConnectAttempts, logger)
	if err != nil {
		return err
	}
	defer pool.Close()

	if err := bootstrapSchema(ctx, cfg, pool, logger); err != nil {
		return err
	}

	redisClient := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr})
	defer func() { _ = redisClient.Close() }()

	probes := []httpapi.Probe{
		probe.Postgres{Pool: pool},
		probe.Redis{Client: redisClient},
	}
	if cfg.RabbitMQURL != "" {
		probes = append(probes, probe.RabbitMQ{URL: cfg.RabbitMQURL})
	}

	sessionConfig := session.Config{
		Secret:     []byte(cfg.JWTSecret),
		AccessTTL:  cfg.AccessTokenTTL,
		RefreshTTL: cfg.RefreshTokenTTL,
		Issuer:     "melia-api",
	}
	issuer := session.New(sessionConfig, session.NewPostgresRefreshStore(pool))
	rotator := session.NewRotator(session.NewPostgresRotateStore(pool), sessionConfig)

	auditor := audit.NewPostgres(pool, []byte(cfg.JWTSecret))

	// Verification is available even when the broker is down: a user who already holds a
	// code must still be able to finish registering.
	signupVerifier := signup.NewVerifier(signup.NewPostgresVerifier(pool), issuer)
	signupVerifier.Auditor = auditor
	signupVerifier.Logger = logger

	loginService := login.New(login.NewPostgresStore(pool), issuer, auditor, logger)
	loginService.MaxAttempts = cfg.LoginMaxAttempts
	loginService.LockDuration = cfg.LoginLockDuration

	// Starting a registration needs the broker to deliver the code. Without it the route
	// stays unregistered rather than accepting a request it cannot fulfil.
	var signupRequester httpapi.SignupRequester
	if cfg.RabbitMQURL == "" {
		logger.Warn("RABBITMQ_URL is empty: signup is disabled because codes cannot be delivered")
	} else {
		publisher := queue.NewAMQP(cfg.RabbitMQURL, cfg.MailQueue)
		defer func() { _ = publisher.Close() }()
		signupRequester = signup.New(signup.NewPostgresStore(pool), publisher, logger)
	}

	server := &http.Server{
		Addr: cfg.HTTPAddr,
		Handler: httpapi.New(httpapi.Options{
			Probes:             probes,
			Logger:             logger,
			CORSAllowedOrigins: cfg.CORSOrigins,
			Signup:             signupRequester,
			SignupVerifier:     signupVerifier,
			Login:              loginService,
			Rotator:            rotator,
			CookieSecure:       cfg.CookieSecure,
		}),
		ReadHeaderTimeout: readHeaderTimeout,
	}

	serverErr := make(chan error, 1)
	go func() {
		logger.Info("http server listening", slog.String("addr", cfg.HTTPAddr))
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	select {
	case err := <-serverErr:
		return fmt.Errorf("http server: %w", err)
	case <-ctx.Done():
	}

	logger.Info("shutdown requested", slog.Duration("timeout", cfg.ShutdownTimeout))

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}

	logger.Info("shutdown complete")
	return nil
}

// bootstrapSchema applies the canonical schema files from database/init.
func bootstrapSchema(ctx context.Context, cfg config.Config, pool *pgxpool.Pool, logger *slog.Logger) error {
	if !cfg.AutoMigrate {
		logger.Warn("schema bootstrap skipped: DB_AUTO_MIGRATE=false")
		return nil
	}

	runner := &db.Runner{
		Dir:      cfg.DBInitDir,
		Executor: db.PoolExecutor{Pool: pool},
		Ledger:   db.NewPostgresLedger(pool),
		Logger:   logger,
	}

	applied, err := runner.Run(ctx)
	if err != nil {
		return err
	}

	logger.Info("schema bootstrap finished", slog.Int("files_applied", len(applied)))
	return nil
}
