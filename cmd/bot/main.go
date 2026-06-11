package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

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
	// Страховка на ранние выходы по ошибке; штатное закрытие — явное,
	// в конце run (повторный Close безопасен: pgxpool использует sync.Once).
	defer pool.Close()
	logger.Info("connected to postgres")

	repos := repository.New(pool)
	authSvc := service.NewAuth(repos, repos.Users, logger)
	subsSvc := service.NewSubscriptions(repos.Shows, repos.Subscriptions, logger)

	tgBot, err := telegram.New(ctx, cfg.BotToken, cfg.TelegramWebhookSecret, authSvc, subsSvc, logger)
	if err != nil {
		return err
	}

	// Сценарии 2 и 3 ТЗ: веерная рассылка и обработка блокировок
	notifier := service.NewNotifier(
		repos.Shows, repos.Subscriptions, repos.Users,
		telegram.NewSender(tgBot), logger)

	// Цикл обработки апдейтов останавливается отменой того же ctx,
	// что и весь процесс. В webhook-режиме апдейты приходят через
	// наш HTTP-сервер, в polling бот опрашивает Telegram сам (ТЗ §5.2).
	var tgWebhook http.HandlerFunc
	botDone := make(chan struct{})
	switch cfg.BotMode {
	case config.BotModeWebhook:
		if err := telegram.SetupWebhook(ctx, tgBot, cfg.WebhookURL, cfg.TelegramWebhookSecret); err != nil {
			return err
		}
		logger.Info("telegram webhook registered", "url", cfg.WebhookURL)
		tgWebhook = tgBot.WebhookHandler()
		go func() {
			defer close(botDone)
			tgBot.StartWebhook(ctx)
		}()
	default: // polling
		// Защита от хвоста webhook-режима: активный вебхук
		// конфликтует с getUpdates (Telegram отвечает 409)
		if err := telegram.RemoveWebhook(ctx, tgBot); err != nil {
			logger.Warn("remove stale webhook", "error", err)
		}
		go func() {
			defer close(botDone)
			logger.Info("telegram bot polling started")
			tgBot.Start(ctx)
		}()
	}

	srv := httpdelivery.NewServer(cfg.HTTPAddr, logger, pool, notifier, cfg.ChangedetectionAuthToken, tgWebhook)

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

	// Порядок из ТЗ §5.4: закрыть пул БД, затем удалить вебхук из Telegram
	pool.Close()
	logger.Info("database pool closed")

	if cfg.BotMode == config.BotModeWebhook {
		delCtx, delCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer delCancel()
		if err := telegram.RemoveWebhook(delCtx, tgBot); err != nil {
			logger.Error("delete telegram webhook", "error", err)
		} else {
			logger.Info("telegram webhook deleted")
		}
	}

	if firstErr != nil {
		return firstErr
	}
	logger.Info("server stopped cleanly")
	return nil
}
