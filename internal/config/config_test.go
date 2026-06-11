package config

import (
	"strings"
	"testing"
	"time"
)

// setRequiredEnv задаёт обязательные переменные,
// без которых Load() осознанно падает.
func setRequiredEnv(t *testing.T) {
	t.Helper()
	t.Setenv("POSTGRES_USER", "bot")
	t.Setenv("POSTGRES_PASSWORD", "secret")
	t.Setenv("POSTGRES_DB", "botdb")
	t.Setenv("BOT_TOKEN", "123:abc")
}

func TestLoadDefaults(t *testing.T) {
	setRequiredEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() вернул ошибку: %v", err)
	}
	if cfg.HTTPAddr != ":8000" {
		t.Errorf("HTTPAddr = %q, ожидается %q", cfg.HTTPAddr, ":8000")
	}
	if cfg.ShutdownTimeout != 10*time.Second {
		t.Errorf("ShutdownTimeout = %v, ожидается %v", cfg.ShutdownTimeout, 10*time.Second)
	}
	want := "postgres://bot:secret@postgres_db:5432/botdb?sslmode=disable"
	if cfg.DatabaseDSN != want {
		t.Errorf("DatabaseDSN = %q, ожидается %q", cfg.DatabaseDSN, want)
	}
}

func TestLoadOverrides(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("HTTP_PORT", "9999")
	t.Setenv("SHUTDOWN_TIMEOUT", "30s")
	t.Setenv("POSTGRES_HOST", "db.example.com")
	t.Setenv("POSTGRES_PORT", "6432")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() вернул ошибку: %v", err)
	}
	if cfg.HTTPAddr != ":9999" {
		t.Errorf("HTTPAddr = %q, ожидается %q", cfg.HTTPAddr, ":9999")
	}
	if cfg.ShutdownTimeout != 30*time.Second {
		t.Errorf("ShutdownTimeout = %v, ожидается %v", cfg.ShutdownTimeout, 30*time.Second)
	}
	if !strings.Contains(cfg.DatabaseDSN, "db.example.com:6432") {
		t.Errorf("DatabaseDSN = %q, ожидается хост db.example.com:6432", cfg.DatabaseDSN)
	}
}

func TestLoadPasswordEscaping(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("POSTGRES_PASSWORD", "p@ss/w:rd")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() вернул ошибку: %v", err)
	}
	if !strings.Contains(cfg.DatabaseDSN, "p%40ss%2Fw%3Ard") {
		t.Errorf("спецсимволы пароля не экранированы: %q", cfg.DatabaseDSN)
	}
}

func TestLoadMissingDBVars(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("POSTGRES_PASSWORD", "") // пустое значение = не задано

	if _, err := Load(); err == nil {
		t.Fatal("Load() без POSTGRES_PASSWORD должен возвращать ошибку")
	}
}

func TestLoadInvalidShutdownTimeout(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("SHUTDOWN_TIMEOUT", "не-длительность")

	if _, err := Load(); err == nil {
		t.Fatal("Load() с кривым SHUTDOWN_TIMEOUT должен возвращать ошибку")
	}
}

func TestLoadMissingBotToken(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("BOT_TOKEN", "")

	if _, err := Load(); err == nil {
		t.Fatal("Load() без BOT_TOKEN должен возвращать ошибку")
	}
}

func TestLoadBotMode(t *testing.T) {
	setRequiredEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() вернул ошибку: %v", err)
	}
	if cfg.BotMode != BotModePolling {
		t.Errorf("BotMode по умолчанию = %q, ожидается %q", cfg.BotMode, BotModePolling)
	}

	t.Setenv("BOT_MODE", "carrier-pigeon")
	if _, err := Load(); err == nil {
		t.Fatal("Load() с неизвестным BOT_MODE должен возвращать ошибку")
	}
}
