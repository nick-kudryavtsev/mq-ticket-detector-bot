// Package config читает конфигурацию приложения из переменных окружения.
package config

import (
	"fmt"
	"os"
	"time"
)

type Config struct {
	// HTTPAddr — адрес HTTP-сервера внутри контейнера, например ":8000".
	HTTPAddr string
	// ShutdownTimeout — сколько ждать завершения активных запросов
	// и текущей пачки рассылки при graceful shutdown.
	ShutdownTimeout time.Duration
}

func Load() (Config, error) {
	cfg := Config{
		HTTPAddr:        ":" + getEnv("HTTP_PORT", "8000"),
		ShutdownTimeout: 10 * time.Second,
	}

	if raw := os.Getenv("SHUTDOWN_TIMEOUT"); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil {
			return Config{}, fmt.Errorf("parse SHUTDOWN_TIMEOUT: %w", err)
		}
		cfg.ShutdownTimeout = d
	}

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
