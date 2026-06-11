// Package config читает конфигурацию приложения из переменных окружения.
package config

import (
	"fmt"
	"net/url"
	"os"
	"time"
)

type Config struct {
	// HTTPAddr — адрес HTTP-сервера внутри контейнера, например ":8000".
	HTTPAddr string
	// ShutdownTimeout — сколько ждать завершения активных запросов
	// и текущей пачки рассылки при graceful shutdown.
	ShutdownTimeout time.Duration
	// DatabaseDSN — строка подключения к PostgreSQL, собирается
	// из тех же POSTGRES_*-переменных, что использует сам контейнер БД.
	DatabaseDSN string
	// BotToken — токен Telegram-бота от BotFather. Обязателен:
	// без него приложение не имеет смысла, падаем сразу.
	BotToken string
	// BotMode — polling (локальная разработка) или webhook (сервер).
	BotMode string
}

const (
	BotModePolling = "polling"
	BotModeWebhook = "webhook"
)

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

	dsn, err := databaseDSN()
	if err != nil {
		return Config{}, err
	}
	cfg.DatabaseDSN = dsn

	cfg.BotToken = os.Getenv("BOT_TOKEN")
	if cfg.BotToken == "" {
		return Config{}, fmt.Errorf("required env BOT_TOKEN is not set")
	}

	cfg.BotMode = getEnv("BOT_MODE", BotModePolling)
	if cfg.BotMode != BotModePolling && cfg.BotMode != BotModeWebhook {
		return Config{}, fmt.Errorf("BOT_MODE must be %q or %q, got %q",
			BotModePolling, BotModeWebhook, cfg.BotMode)
	}

	return cfg, nil
}

// DatabaseDSNFromEnv отдаёт только DSN БД — для утилит вроде invitegen,
// которым не нужен токен бота и прочая конфигурация.
func DatabaseDSNFromEnv() (string, error) {
	return databaseDSN()
}

func databaseDSN() (string, error) {
	user := os.Getenv("POSTGRES_USER")
	password := os.Getenv("POSTGRES_PASSWORD")
	dbname := os.Getenv("POSTGRES_DB")
	for name, v := range map[string]string{
		"POSTGRES_USER":     user,
		"POSTGRES_PASSWORD": password,
		"POSTGRES_DB":       dbname,
	} {
		if v == "" {
			return "", fmt.Errorf("required env %s is not set", name)
		}
	}

	u := url.URL{
		Scheme: "postgres",
		// url.UserPassword экранирует спецсимволы в пароле
		User: url.UserPassword(user, password),
		Host: getEnv("POSTGRES_HOST", "postgres_db") + ":" + getEnv("POSTGRES_PORT", "5432"),
		Path: "/" + dbname,
		// Внутри изолированной docker-сети TLS к БД не используется
		RawQuery: "sslmode=disable",
	}
	return u.String(), nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
