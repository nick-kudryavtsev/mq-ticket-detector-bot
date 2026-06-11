// Package telegram — delivery-слой Telegram-бота: маршрутизация апдейтов,
// тексты ответов и контроль доступа. Бизнес-логика живёт в service.
package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"gitlab.com/kabanza/mq-ticket-detector/internal/service"
)

const (
	// Текст из ТЗ, сценарий 1: пользователь без валидного приглашения.
	msgAccessDenied = "Доступ ограничен. Этот бот работает только по пригласительным ссылкам."
	msgWelcome      = "✅ Приглашение принято — доступ открыт!\n\nКоманда /shows покажет список шоу для подписки."
	msgHelp         = "Доступные команды:\n/shows — список шоу и управление подписками (появится в следующем обновлении)."
	msgInternalErr  = "Произошла внутренняя ошибка. Попробуйте позже."
)

type handler struct {
	auth *service.Auth
	log  *slog.Logger
}

// New собирает бота: /start открыт для всех (это и есть вход по приглашению),
// всё остальное — только для авторизованных через requireAuth.
func New(token string, auth *service.Auth, log *slog.Logger) (*bot.Bot, error) {
	h := &handler{auth: auth, log: log}

	b, err := bot.New(token, bot.WithDefaultHandler(h.requireAuth(h.handleHelp)))
	if err != nil {
		return nil, fmt.Errorf("init telegram bot: %w", err)
	}
	b.RegisterHandler(bot.HandlerTypeMessageText, "/start", bot.MatchTypePrefix, h.handleStart)

	return b, nil
}

// handleStart — сценарий 1 ТЗ: deep linking. Текст сообщения имеет вид
// «/start» либо «/start <secret_token>».
func (h *handler) handleStart(ctx context.Context, b *bot.Bot, update *models.Update) {
	msg := update.Message
	if msg == nil || msg.From == nil {
		return
	}

	registered, err := h.auth.RegisterByInvite(ctx, msg.From.ID, msg.From.Username, startPayload(msg.Text))
	switch {
	case err != nil:
		h.log.Error("register by invite", "telegram_id", msg.From.ID, "error", err)
		h.reply(ctx, b, msg.Chat.ID, msgInternalErr)
	case registered:
		h.reply(ctx, b, msg.Chat.ID, msgWelcome)
	default:
		h.reply(ctx, b, msg.Chat.ID, msgAccessDenied)
	}
}

func (h *handler) handleHelp(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update.Message == nil {
		return
	}
	h.reply(ctx, b, update.Message.Chat.ID, msgHelp)
}

// requireAuth пропускает дальше только пользователей из таблицы users
// с is_active = true. Всем остальным — отказ из ТЗ.
func (h *handler) requireAuth(next bot.HandlerFunc) bot.HandlerFunc {
	return func(ctx context.Context, b *bot.Bot, update *models.Update) {
		msg := update.Message
		if msg == nil || msg.From == nil {
			return
		}

		authorized, err := h.auth.IsAuthorized(ctx, msg.From.ID)
		if err != nil {
			h.log.Error("authorization check", "telegram_id", msg.From.ID, "error", err)
			h.reply(ctx, b, msg.Chat.ID, msgInternalErr)
			return
		}
		if !authorized {
			h.reply(ctx, b, msg.Chat.ID, msgAccessDenied)
			return
		}
		next(ctx, b, update)
	}
}

func (h *handler) reply(ctx context.Context, b *bot.Bot, chatID int64, text string) {
	if _, err := b.SendMessage(ctx, &bot.SendMessageParams{ChatID: chatID, Text: text}); err != nil {
		h.log.Error("send message", "chat_id", chatID, "error", err)
	}
}

// startPayload достаёт токен из «/start <токен>»; пустая строка,
// если параметра нет. Понимает и форму «/start@ИмяБота <токен>».
func startPayload(text string) string {
	fields := strings.Fields(text)
	if len(fields) < 2 {
		return ""
	}
	return fields[1]
}
