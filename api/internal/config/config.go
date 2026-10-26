// Package config loads the service configuration from the process environment.
//
// The lookup function is injected so callers (and tests) control where values come
// from; nothing in this package reads os.Getenv directly.
package config

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"
)

// Defaults applied when an optional variable is unset.
const (
	DefaultHTTPAddr          = ":8080"
	DefaultDBInitDir         = "/app/database/init"
	DefaultShutdownTimeout   = 10 * time.Second
	DefaultMailQueue         = "melia.mail"
	DefaultSMTPAddr          = "mailpit:1025"
	DefaultMailFrom          = "no-reply@melia.test"
	DefaultAccessTokenTTL    = 15 * time.Minute
	DefaultRefreshTokenTTL   = 720 * time.Hour
	DefaultLoginMaxAttempts  = 5
	DefaultLoginLockDuration = 15 * time.Minute
	// MinJWTSecretLength keeps HS256 out of reach of a guessable key.
	MinJWTSecretLength = 32
)

// Config is the fully resolved runtime configuration.
type Config struct {
	HTTPAddr          string
	DatabaseURL       string
	RedisAddr         string
	RabbitMQURL       string
	DBInitDir         string
	CORSOrigins       []string
	MailQueue         string
	SMTPAddr          string
	MailFrom          string
	JWTSecret         string
	AccessTokenTTL    time.Duration
	RefreshTokenTTL   time.Duration
	LoginMaxAttempts  int
	LoginLockDuration time.Duration
	AutoMigrate       bool
	LogLevel          slog.Level
	ShutdownTimeout   time.Duration
}

// Load reads and validates the configuration. It returns an error naming the offending
// variable instead of falling back to a default for anything that matters.
func Load(getenv func(string) string) (Config, error) {
	cfg := Config{
		HTTPAddr:          valueOr(getenv, "HTTP_ADDR", DefaultHTTPAddr),
		DatabaseURL:       lookup(getenv, "DATABASE_URL"),
		RedisAddr:         lookup(getenv, "REDIS_ADDR"),
		RabbitMQURL:       lookup(getenv, "RABBITMQ_URL"),
		DBInitDir:         valueOr(getenv, "DB_INIT_DIR", DefaultDBInitDir),
		CORSOrigins:       splitList(lookup(getenv, "CORS_ALLOWED_ORIGINS")),
		MailQueue:         valueOr(getenv, "MAIL_QUEUE", DefaultMailQueue),
		SMTPAddr:          valueOr(getenv, "SMTP_ADDR", DefaultSMTPAddr),
		MailFrom:          valueOr(getenv, "MAIL_FROM", DefaultMailFrom),
		JWTSecret:         lookup(getenv, "JWT_SECRET"),
		AccessTokenTTL:    DefaultAccessTokenTTL,
		RefreshTokenTTL:   DefaultRefreshTokenTTL,
		LoginMaxAttempts:  DefaultLoginMaxAttempts,
		LoginLockDuration: DefaultLoginLockDuration,
		AutoMigrate:       true,
		LogLevel:          slog.LevelInfo,
		ShutdownTimeout:   DefaultShutdownTimeout,
	}

	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("missing required environment variable: DATABASE_URL")
	}
	if cfg.RedisAddr == "" {
		return Config{}, fmt.Errorf("missing required environment variable: REDIS_ADDR")
	}

	if cfg.JWTSecret == "" {
		return Config{}, fmt.Errorf("missing required environment variable: JWT_SECRET")
	}
	if len(cfg.JWTSecret) < MinJWTSecretLength {
		return Config{}, fmt.Errorf("invalid JWT_SECRET: must be at least %d characters", MinJWTSecretLength)
	}

	for key, target := range map[string]*time.Duration{
		"ACCESS_TOKEN_TTL":    &cfg.AccessTokenTTL,
		"REFRESH_TOKEN_TTL":   &cfg.RefreshTokenTTL,
		"LOGIN_LOCK_DURATION": &cfg.LoginLockDuration,
	} {
		raw := lookup(getenv, key)
		if raw == "" {
			continue
		}
		parsed, err := time.ParseDuration(raw)
		if err != nil || parsed <= 0 {
			return Config{}, fmt.Errorf("invalid %s %q: must be a positive duration such as 15m", key, raw)
		}
		*target = parsed
	}

	if raw := lookup(getenv, "LOGIN_MAX_ATTEMPTS"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			return Config{}, fmt.Errorf("invalid LOGIN_MAX_ATTEMPTS %q: must be a positive integer", raw)
		}
		cfg.LoginMaxAttempts = parsed
	}

	if raw := lookup(getenv, "DB_AUTO_MIGRATE"); raw != "" {
		parsed, err := parseBool(raw)
		if err != nil {
			return Config{}, fmt.Errorf("invalid DB_AUTO_MIGRATE %q: must be true or false", raw)
		}
		cfg.AutoMigrate = parsed
	}

	if raw := lookup(getenv, "LOG_LEVEL"); raw != "" {
		level, err := parseLogLevel(raw)
		if err != nil {
			return Config{}, err
		}
		cfg.LogLevel = level
	}

	return cfg, nil
}

// splitList parses a comma-separated variable, dropping blanks.
func splitList(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			values = append(values, part)
		}
	}
	return values
}

func lookup(getenv func(string) string, key string) string {
	return strings.TrimSpace(getenv(key))
}

func valueOr(getenv func(string) string, key, fallback string) string {
	if value := lookup(getenv, key); value != "" {
		return value
	}
	return fallback
}

func parseBool(raw string) (bool, error) {
	switch strings.ToLower(raw) {
	case "true", "1":
		return true, nil
	case "false", "0":
		return false, nil
	default:
		return false, fmt.Errorf("unparseable boolean %q", raw)
	}
}

func parseLogLevel(raw string) (slog.Level, error) {
	switch strings.ToLower(raw) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, fmt.Errorf("invalid LOG_LEVEL %q: must be one of debug, info, warn, error", raw)
	}
}
