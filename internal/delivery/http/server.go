// Package http собирает HTTP-сервер приложения: роутинг и таймауты.
// Эндпоинт /api/v1/trigger появится на шаге интеграции со скрейпером.
package http

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// Pinger — то, что умеет проверить соединение с хранилищем (pgxpool.Pool).
// Интерфейс держит пакет независимым от драйвера БД.
type Pinger interface {
	Ping(ctx context.Context) error
}

// NewServer собирает HTTP-сервер. tgWebhook — обработчик апдейтов Telegram
// (nil в polling-режиме, тогда маршрут не монтируется); подлинность апдейтов
// он проверяет сам по X-Telegram-Bot-Api-Secret-Token.
func NewServer(addr string, logger *slog.Logger, db Pinger, trigger TriggerService, scraperSecret string, tgWebhook http.HandlerFunc) *http.Server {
	r := chi.NewRouter()

	// Recoverer: паника в хендлере отдаёт 500 и пишется в лог,
	// а не роняет весь процесс вместе с ботом.
	// NB: middleware.RealIP не используем сознательно — он deprecated как
	// уязвимый к спуфингу X-Forwarded-For (GHSA-3fxj-6jh8-hvhx); если
	// понадобится IP клиента, будем доверять заголовку только от nginx.
	r.Use(middleware.Recoverer)

	// liveness: процесс жив
	r.Get("/healthz", handleHealthz)
	// readiness: процесс жив И база отвечает
	r.Get("/readyz", handleReadyz(logger, db))

	// Вебхук скрейпера: вся группа /api/v1 закрыта заголовком-секретом
	r.Route("/api/v1", func(api chi.Router) {
		api.Use(requireScraperAuth(logger, scraperSecret))
		api.Post("/trigger", handleTrigger(logger, trigger))
	})

	// Вебхук Telegram (BOT_MODE=webhook): nginx проксирует сюда
	// запросы с https://<домен>/telegram/webhook
	if tgWebhook != nil {
		r.Post("/telegram/webhook", tgWebhook)
	}

	return &http.Server{
		Addr:    addr,
		Handler: r,
		// Защита от медленных клиентов, держащих соединения открытыми
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}
}

func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func handleReadyz(logger *slog.Logger, db Pinger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		if err := db.Ping(ctx); err != nil {
			logger.Error("readiness probe failed", "error", err)
			http.Error(w, "database unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready"))
	}
}
