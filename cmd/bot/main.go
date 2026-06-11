package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"gitlab.com/kabanza/mq-ticket-detector/internal/config"
	httpdelivery "gitlab.com/kabanza/mq-ticket-detector/internal/delivery/http"
	"gitlab.com/kabanza/mq-ticket-detector/internal/delivery/telegram"
	"gitlab.com/kabanza/mq-ticket-detector/internal/repository"
	"gitlab.com/kabanza/mq-ticket-detector/internal/service"
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

	repos := repository.New(pool)
	authSvc := service.NewAuth(repos, repos.Users, logger)
	subsSvc := service.NewSubscriptions(repos.Shows, repos.Subscriptions, logger)

	if cfg.BotMode != config.BotModePolling {
		return fmt.Errorf("BOT_MODE=%s пока не реализован (появится на шаге вебхуков)", cfg.BotMode)
	}

	tgBot, err := telegram.New(ctx, cfg.BotToken, authSvc, subsSvc, logger)
	if err != nil {
		return err
	}

	// Сценарии 2 и 3 ТЗ: веерная рассылка и обработка блокировок
	notifier := service.NewNotifier(
		repos.Shows, repos.Subscriptions, repos.Users,
		telegram.NewSender(tgBot), logger)

	// Поллинг останавливается отменой того же ctx, что и весь процесс
	botDone := make(chan struct{})
	go func() {
		defer close(botDone)
		logger.Info("telegram bot polling started")
		tgBot.Start(ctx)
	}()

	srv := httpdelivery.NewServer(cfg.HTTPAddr, logger, pool, notifier, cfg.ChangedetectionAuthToken)

	serverErr := make(chan error, 1)
	go func() {
		logger.Info("http server listening", "addr", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	var firstErr error
	select {
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	case firstErr = <-serverErr:
		logger.Error("http server failed", "error", firstErr)
		stop() // отменяет ctx — бот тоже должен остановиться
	}

	// Порядок остановки (ТЗ §5.4): перестаём принимать запросы и апдейты,
	// досылаем текущую пачку рассылки, и только потом defer закроет пул БД.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil && firstErr == nil {
		firstErr = err
	}

	if err := notifier.Wait(shutdownCtx); err != nil {
		logger.Error("waiting for broadcasts", "error", err)
	} else {
		logger.Info("broadcasts finished")
	}

	<-botDone
	logger.Info("telegram bot stopped")

	if firstErr != nil {
		return firstErr
	}
	logger.Info("server stopped cleanly")
	return nil
}
