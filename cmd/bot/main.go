package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"gitlab.com/kabanza/mq-ticket-detector/internal/config"
	httpdelivery "gitlab.com/kabanza/mq-ticket-detector/internal/delivery/http"
	"gitlab.com/kabanza/mq-ticket-detector/internal/repository"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	if err := run(logger); err != nil {
		logger.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// ctx отменяется по SIGINT/SIGTERM — единая точка входа для graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := repository.NewPool(ctx, cfg.DatabaseDSN)
	if err != nil {
		return err
	}
	// Закрывается ПОСЛЕ остановки HTTP-сервера (defer выполняется позже
	// кода в конце run): к этому моменту запросов к БД уже нет.
	defer func() {
		pool.Close()
		logger.Info("database pool closed")
	}()
	logger.Info("connected to postgres")

	srv := httpdelivery.NewServer(cfg.HTTPAddr, logger, pool)

	serverErr := make(chan error, 1)
	go func() {
		logger.Info("http server listening", "addr", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	select {
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	case err := <-serverErr:
		return err
	}

	// Перестаём принимать новые запросы и ждём завершения активных,
	// но не дольше ShutdownTimeout. Здесь же позже будут закрываться
	// пул PostgreSQL и вебхук Telegram (в обратном порядке инициализации).
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}

	logger.Info("server stopped cleanly")
	return nil
}
