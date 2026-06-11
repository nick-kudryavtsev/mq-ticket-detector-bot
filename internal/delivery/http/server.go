// Package http собирает HTTP-сервер приложения: роутинг и таймауты.
// Пока здесь только /healthz; эндпоинт /api/v1/trigger появится
// на шаге интеграции со скрейпером.
package http

import (
	"log/slog"
	"net/http"
	"time"
)

func NewServer(addr string, logger *slog.Logger) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handleHealthz)

	return &http.Server{
		Addr:    addr,
		Handler: mux,
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
