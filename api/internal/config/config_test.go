package config

import (
	"log/slog"
	"strings"
	"testing"
	"time"
)

// env builds a getenv function from a map so tests never touch the process environment.
func env(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func validEnv() map[string]string {
	return map[string]string{
		"DATABASE_URL": "postgres://admin:pass@database:5432/melia?sslmode=disable",
		"REDIS_ADDR":   "redis:6379",
		"JWT_SECRET":   "un-secreto-de-pruebas-suficientemente-largo",
	}
}

func TestLoadAppliesDefaults(t *testing.T) {
	cfg, err := Load(env(validEnv()))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.HTTPAddr != ":8080" {
		t.Errorf("HTTPAddr = %q, want %q", cfg.HTTPAddr, ":8080")
	}
	if cfg.DBInitDir != "/app/database/init" {
		t.Errorf("DBInitDir = %q, want %q", cfg.DBInitDir, "/app/database/init")
	}
	if !cfg.AutoMigrate {
		t.Error("AutoMigrate = false, want true by default")
	}
	if cfg.LogLevel != slog.LevelInfo {
		t.Errorf("LogLevel = %v, want %v", cfg.LogLevel, slog.LevelInfo)
	}
	if cfg.ShutdownTimeout != 10*time.Second {
		t.Errorf("ShutdownTimeout = %v, want 10s", cfg.ShutdownTimeout)
	}
	if cfg.RabbitMQURL != "" {
		t.Errorf("RabbitMQURL = %q, want empty by default", cfg.RabbitMQURL)
	}
}

func TestLoadKeepsExplicitValues(t *testing.T) {
	values := validEnv()
	values["HTTP_ADDR"] = "127.0.0.1:9999"
	values["DB_INIT_DIR"] = "./init"
	values["RABBITMQ_URL"] = "amqp://admin:pass@rabbitmq:5672/"
	values["DB_AUTO_MIGRATE"] = "false"
	values["LOG_LEVEL"] = "debug"

	cfg, err := Load(env(values))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.HTTPAddr != "127.0.0.1:9999" {
		t.Errorf("HTTPAddr = %q", cfg.HTTPAddr)
	}
	if cfg.DBInitDir != "./init" {
		t.Errorf("DBInitDir = %q", cfg.DBInitDir)
	}
	if cfg.RabbitMQURL != "amqp://admin:pass@rabbitmq:5672/" {
		t.Errorf("RabbitMQURL = %q", cfg.RabbitMQURL)
	}
	if cfg.AutoMigrate {
		t.Error("AutoMigrate = true, want false when DB_AUTO_MIGRATE=false")
	}
	if cfg.LogLevel != slog.LevelDebug {
		t.Errorf("LogLevel = %v, want debug", cfg.LogLevel)
	}
}

func TestLoadRejectsMissingRequiredVariables(t *testing.T) {
	for _, missing := range []string{"DATABASE_URL", "REDIS_ADDR"} {
		values := validEnv()
		delete(values, missing)

		_, err := Load(env(values))
		if err == nil {
			t.Fatalf("Load succeeded without %s, want error", missing)
		}
		if !strings.Contains(err.Error(), missing) {
			t.Errorf("error %q does not name %s", err, missing)
		}
	}
}

func TestLoadRejectsUnparseableValues(t *testing.T) {
	cases := map[string]string{
		"DB_AUTO_MIGRATE": "banana",
		"LOG_LEVEL":       "verbose",
	}

	for key, value := range cases {
		values := validEnv()
		values[key] = value

		_, err := Load(env(values))
		if err == nil {
			t.Fatalf("Load accepted %s=%q, want error", key, value)
		}
		if !strings.Contains(err.Error(), key) {
			t.Errorf("error %q does not name %s", err, key)
		}
	}
}

func TestLoadTrimsSurroundingWhitespace(t *testing.T) {
	values := validEnv()
	values["REDIS_ADDR"] = "  redis:6379\n"

	cfg, err := Load(env(values))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.RedisAddr != "redis:6379" {
		t.Errorf("RedisAddr = %q, want trimmed value", cfg.RedisAddr)
	}
}

func TestLoadParsesCORSOrigins(t *testing.T) {
	values := validEnv()
	values["CORS_ALLOWED_ORIGINS"] = " https://admin.melia.test , https://melia.test ,, "

	cfg, err := Load(env(values))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if len(cfg.CORSOrigins) != 2 {
		t.Fatalf("CORSOrigins = %v, want 2 entries", cfg.CORSOrigins)
	}
	if cfg.CORSOrigins[0] != "https://admin.melia.test" || cfg.CORSOrigins[1] != "https://melia.test" {
		t.Errorf("CORSOrigins = %v, want trimmed values in order", cfg.CORSOrigins)
	}
}

func TestLoadHasNoCORSOriginsByDefault(t *testing.T) {
	cfg, err := Load(env(validEnv()))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if len(cfg.CORSOrigins) != 0 {
		t.Errorf("CORSOrigins = %v, want none: a server must not invent an allowed origin", cfg.CORSOrigins)
	}
}

func TestLoadRequiresAStrongJWTSecret(t *testing.T) {
	values := validEnv()
	delete(values, "JWT_SECRET")

	if _, err := Load(env(values)); err == nil || !strings.Contains(err.Error(), "JWT_SECRET") {
		t.Fatalf("error = %v, want it to name JWT_SECRET", err)
	}

	values["JWT_SECRET"] = "corto"
	if _, err := Load(env(values)); err == nil || !strings.Contains(err.Error(), "JWT_SECRET") {
		t.Fatalf("error = %v, want a length complaint naming JWT_SECRET", err)
	}
}

func TestLoadParsesTokenTTLs(t *testing.T) {
	values := validEnv()
	values["ACCESS_TOKEN_TTL"] = "5m"
	values["REFRESH_TOKEN_TTL"] = "48h"

	cfg, err := Load(env(values))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.AccessTokenTTL != 5*time.Minute {
		t.Errorf("AccessTokenTTL = %v, want 5m", cfg.AccessTokenTTL)
	}
	if cfg.RefreshTokenTTL != 48*time.Hour {
		t.Errorf("RefreshTokenTTL = %v, want 48h", cfg.RefreshTokenTTL)
	}

	values["ACCESS_TOKEN_TTL"] = "cinco minutos"
	if _, err := Load(env(values)); err == nil || !strings.Contains(err.Error(), "ACCESS_TOKEN_TTL") {
		t.Fatalf("error = %v, want it to name ACCESS_TOKEN_TTL", err)
	}
}

func TestLoadDefaultsTokenTTLs(t *testing.T) {
	cfg, err := Load(env(validEnv()))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.AccessTokenTTL != 15*time.Minute {
		t.Errorf("AccessTokenTTL = %v, want 15m", cfg.AccessTokenTTL)
	}
	if cfg.RefreshTokenTTL != 720*time.Hour {
		t.Errorf("RefreshTokenTTL = %v, want 720h", cfg.RefreshTokenTTL)
	}
}
