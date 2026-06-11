// Package service — бизнес-логика приложения. Связывает delivery-слои
// (Telegram, HTTP) с репозиториями, не зная ни про SQL, ни про Telegram API.
package service

import (
	"context"
	"log/slog"
)

// TokenRedeemer атомарно гасит инвайт-токен и создаёт/реактивирует пользователя.
type TokenRedeemer interface {
	RedeemInvite(ctx context.Context, token string, telegramID int64, username string) (ok bool, err error)
}

// UserChecker отвечает, активен ли пользователь.
type UserChecker interface {
	IsActive(ctx context.Context, telegramID int64) (bool, error)
}

// Auth — сценарий 1 ТЗ: приватная регистрация по инвайтам и контроль доступа.
type Auth struct {
	invites TokenRedeemer
	users   UserChecker
	log     *slog.Logger
}

func NewAuth(invites TokenRedeemer, users UserChecker, log *slog.Logger) *Auth {
	return &Auth{invites: invites, users: users, log: log}
}

// RegisterByInvite обрабатывает /start. Пустой токен (пользователь нашёл бота
// напрямую) отклоняется сразу, без запроса к БД — в users никто не пишется.
func (a *Auth) RegisterByInvite(ctx context.Context, telegramID int64, username, token string) (bool, error) {
	if token == "" {
		a.log.Info("start without invite token rejected", "telegram_id", telegramID)
		return false, nil
	}

	ok, err := a.invites.RedeemInvite(ctx, token, telegramID, username)
	if err != nil {
		return false, err
	}
	if !ok {
		a.log.Info("invalid or used invite token rejected", "telegram_id", telegramID)
		return false, nil
	}

	a.log.Info("user registered by invite", "telegram_id", telegramID, "username", username)
	return true, nil
}

// IsAuthorized — проверка доступа к командам бота: пользователь
// существует в users и не деактивирован.
func (a *Auth) IsAuthorized(ctx context.Context, telegramID int64) (bool, error) {
	return a.users.IsActive(ctx, telegramID)
}
