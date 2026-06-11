package config

import (
	"strings"
	"testing"
	"time"
)

// setRequiredDBEnv задаёт обязательные переменные подключения к БД,
// без которых Load() осознанно падает.
func setRequiredDBEnv(t *testing.T) {
	t.Helper()
	t.Setenv("POSTGRES_USER", "bot")
	t.Setenv("POSTGRES_PASSWORD", "secret")
	t.Setenv("POSTGRES_DB", "botdb")
}

func TestLoadDefaults(t *testing.T) {
	setRequiredDBEnv(t)

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
	setRequiredDBEnv(t)
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
	setRequiredDBEnv(t)
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
	setRequiredDBEnv(t)
	t.Setenv("POSTGRES_PASSWORD", "") // пустое значение = не задано

	if _, err := Load(); err == nil {
		t.Fatal("Load() без POSTGRES_PASSWORD должен возвращать ошибку")
	}
}

func TestLoadInvalidShutdownTimeout(t *testing.T) {
	setRequiredDBEnv(t)
	t.Setenv("SHUTDOWN_TIMEOUT", "не-длительность")

	if _, err := Load(); err == nil {
		t.Fatal("Load() с кривым SHUTDOWN_TIMEOUT должен возвращать ошибку")
	}
}
