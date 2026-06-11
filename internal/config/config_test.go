package config

import (
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() с пустым окружением вернул ошибку: %v", err)
	}
	if cfg.HTTPAddr != ":8000" {
		t.Errorf("HTTPAddr = %q, ожидается %q", cfg.HTTPAddr, ":8000")
	}
	if cfg.ShutdownTimeout != 10*time.Second {
		t.Errorf("ShutdownTimeout = %v, ожидается %v", cfg.ShutdownTimeout, 10*time.Second)
	}
}

func TestLoadOverrides(t *testing.T) {
	t.Setenv("HTTP_PORT", "9999")
	t.Setenv("SHUTDOWN_TIMEOUT", "30s")

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
}

func TestLoadInvalidShutdownTimeout(t *testing.T) {
	t.Setenv("SHUTDOWN_TIMEOUT", "не-длительность")

	if _, err := Load(); err == nil {
		t.Fatal("Load() с кривым SHUTDOWN_TIMEOUT должен возвращать ошибку")
	}
}
