// Package http собирает HTTP-сервер приложения: роутинг и таймауты.
// Пока здесь только /healthz; эндпоинт /api/v1/trigger появится
// на шаге интеграции со скрейпером.
package http

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func NewServer(addr string, logger *slog.Logger) *http.Server {
	r := chi.NewRouter()

	// Recoverer: паника в хендлере отдаёт 500 и пишется в лог,
	// а не роняет весь процесс вместе с ботом.
	// NB: middleware.RealIP не используем сознательно — он deprecated как
	// уязвимый к спуфингу X-Forwarded-For (GHSA-3fxj-6jh8-hvhx); если
	// понадобится IP клиента, будем доверять заголовку только от nginx.
	r.Use(middleware.Recoverer)

	r.Get("/healthz", handleHealthz)

	// Сюда на шаге trigger-api добавится группа:
	// r.Route("/api/v1", func(r chi.Router) { ... })

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
