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
	t.Setenv("CHANGEDETECTION_AUTH_TOKEN", "scraper-secret")
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

func TestLoadMissingScraperToken(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("CHANGEDETECTION_AUTH_TOKEN", "")

	if _, err := Load(); err == nil {
		t.Fatal("Load() без CHANGEDETECTION_AUTH_TOKEN должен возвращать ошибку")
	}
}

func TestLoadWebhookModeRequirements(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("BOT_MODE", "webhook")

	// без WEBHOOK_URL и секрета — ошибка
	if _, err := Load(); err == nil {
		t.Fatal("webhook-режим без WEBHOOK_URL должен возвращать ошибку")
	}

	t.Setenv("WEBHOOK_URL", "http://insecure.example.com/hook")
	t.Setenv("TELEGRAM_WEBHOOK_SECRET", "s3cret")
	if _, err := Load(); err == nil {
		t.Fatal("WEBHOOK_URL без https должен возвращать ошибку")
	}

	t.Setenv("WEBHOOK_URL", "https://bot.example.com/telegram/webhook")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("валидный webhook-конфиг: %v", err)
	}
	if cfg.WebhookURL != "https://bot.example.com/telegram/webhook" || cfg.TelegramWebhookSecret != "s3cret" {
		t.Fatalf("конфиг вебхука не прочитан: %+v", cfg)
	}

	// в polling-режиме эти переменные не обязательны
	t.Setenv("BOT_MODE", "polling")
	t.Setenv("WEBHOOK_URL", "")
	t.Setenv("TELEGRAM_WEBHOOK_SECRET", "")
	if _, err := Load(); err != nil {
		t.Fatalf("polling без webhook-переменных должен работать: %v", err)
	}
}

func TestLoadNotifyCooldown(t *testing.T) {
	setRequiredEnv(t)

	cfg, err := Load()
	if err != nil || cfg.NotifyCooldown != 0 {
		t.Fatalf("дефолтный cooldown: %v, err=%v, ожидается 0 (выключен)", cfg.NotifyCooldown, err)
	}

	t.Setenv("NOTIFY_COOLDOWN", "30m")
	if cfg, err = Load(); err != nil || cfg.NotifyCooldown != 30*time.Minute {
		t.Fatalf("NOTIFY_COOLDOWN=30m: %v, err=%v", cfg.NotifyCooldown, err)
	}

	t.Setenv("NOTIFY_COOLDOWN", "0")
	if cfg, err = Load(); err != nil || cfg.NotifyCooldown != 0 {
		t.Fatalf("NOTIFY_COOLDOWN=0: %v, err=%v", cfg.NotifyCooldown, err)
	}

	t.Setenv("NOTIFY_COOLDOWN", "-5m")
	if _, err = Load(); err == nil {
		t.Fatal("отрицательный NOTIFY_COOLDOWN должен возвращать ошибку")
	}
}
