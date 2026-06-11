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
	msgHelp         = "Доступные команды:\n/shows — список шоу и управление подписками."
	msgInternalErr  = "Произошла внутренняя ошибка. Попробуйте позже."
	msgNoShows      = "Шоу пока не добавлены — загляните позже."
	msgChooseShow   = "Нажмите на шоу, чтобы подписаться или отписаться:"

	toastSubscribed   = "Подписка оформлена ✅"
	toastUnsubscribed = "Подписка отключена ❌"
)

type handler struct {
	auth *service.Auth
	subs *service.Subscriptions
	log  *slog.Logger
}

// New собирает бота: /start открыт для всех (это и есть вход по приглашению),
// всё остальное — только для авторизованных через requireAuth.
func New(ctx context.Context, token string, auth *service.Auth, subs *service.Subscriptions, log *slog.Logger) (*bot.Bot, error) {
	h := &handler{auth: auth, subs: subs, log: log}

	b, err := bot.New(token, bot.WithDefaultHandler(h.requireAuth(h.handleHelp)))
	if err != nil {
		return nil, fmt.Errorf("init telegram bot: %w", err)
	}
	b.RegisterHandler(bot.HandlerTypeMessageText, "/start", bot.MatchTypePrefix, h.handleStart)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/shows", bot.MatchTypePrefix,
		h.requireAuth(h.handleShows))
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, callbackToggle, bot.MatchTypePrefix,
		h.requireAuth(h.handleToggleShow))

	// Меню команд (всплывающий список при вводе «/»). Ошибка не фатальна:
	// меню — косметика, бот работает и без него.
	_, err = b.SetMyCommands(ctx, &bot.SetMyCommandsParams{
		Commands: []models.BotCommand{
			{Command: "shows", Description: "Список шоу и управление подписками"},
		},
	})
	if err != nil {
		log.Warn("set bot commands", "error", err)
	}

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

// handleShows — сценарий 1 ТЗ, шаг 4: список шоу с статусами подписки.
func (h *handler) handleShows(ctx context.Context, b *bot.Bot, update *models.Update) {
	msg := update.Message
	if msg == nil || msg.From == nil {
		return
	}

	shows, err := h.subs.ListForUser(ctx, msg.From.ID)
	if err != nil {
		h.log.Error("list shows", "telegram_id", msg.From.ID, "error", err)
		h.reply(ctx, b, msg.Chat.ID, msgInternalErr)
		return
	}
	if len(shows) == 0 {
		h.reply(ctx, b, msg.Chat.ID, msgNoShows)
		return
	}

	_, err = b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      msg.Chat.ID,
		Text:        msgChooseShow,
		ReplyMarkup: showsKeyboard(shows),
	})
	if err != nil {
		h.log.Error("send shows keyboard", "chat_id", msg.Chat.ID, "error", err)
	}
}

// handleToggleShow — сценарий 1 ТЗ, шаг 5: перехват CallbackQuery,
// переключение подписки и перерисовка клавиатуры.
func (h *handler) handleToggleShow(ctx context.Context, b *bot.Bot, update *models.Update) {
	cq := update.CallbackQuery
	if cq == nil {
		return
	}

	showID, err := parseToggleShowID(cq.Data)
	if err != nil {
		h.log.Error("parse callback data", "data", cq.Data, "error", err)
		h.answerCallback(ctx, b, cq.ID, msgInternalErr)
		return
	}

	subscribed, err := h.subs.Toggle(ctx, cq.From.ID, showID)
	if err != nil {
		h.log.Error("toggle subscription",
			"telegram_id", cq.From.ID, "show_id", showID, "error", err)
		h.answerCallback(ctx, b, cq.ID, msgInternalErr)
		return
	}

	// Сначала гасим «часики» на кнопке — мгновенная обратная связь,
	// перерисовка клавиатуры идёт следом.
	toast := toastUnsubscribed
	if subscribed {
		toast = toastSubscribed
	}
	h.answerCallback(ctx, b, cq.ID, toast)
	h.refreshKeyboard(ctx, b, cq)
}

// refreshKeyboard перечитывает подписки и обновляет статусы ✅/❌
// на клавиатуре исходного сообщения.
func (h *handler) refreshKeyboard(ctx context.Context, b *bot.Bot, cq *models.CallbackQuery) {
	// Message может быть недоступен (например, слишком старое сообщение) —
	// тогда обновлять нечего, подписка в БД уже переключена.
	if cq.Message.Message == nil {
		return
	}

	shows, err := h.subs.ListForUser(ctx, cq.From.ID)
	if err != nil {
		h.log.Error("refresh keyboard: list shows", "telegram_id", cq.From.ID, "error", err)
		return
	}

	_, err = b.EditMessageReplyMarkup(ctx, &bot.EditMessageReplyMarkupParams{
		ChatID:      cq.Message.Message.Chat.ID,
		MessageID:   cq.Message.Message.ID,
		ReplyMarkup: showsKeyboard(shows),
	})
	if err != nil {
		h.log.Error("edit keyboard", "chat_id", cq.Message.Message.Chat.ID, "error", err)
	}
}

func (h *handler) handleHelp(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update.Message == nil {
		return
	}
	h.reply(ctx, b, update.Message.Chat.ID, msgHelp)
}

// requireAuth пропускает дальше только пользователей из таблицы users
// с is_active = true. Работает и для сообщений, и для CallbackQuery.
func (h *handler) requireAuth(next bot.HandlerFunc) bot.HandlerFunc {
	return func(ctx context.Context, b *bot.Bot, update *models.Update) {
		var telegramID int64
		switch {
		case update.Message != nil && update.Message.From != nil:
			telegramID = update.Message.From.ID
		case update.CallbackQuery != nil:
			telegramID = update.CallbackQuery.From.ID
		default:
			return
		}

		authorized, err := h.auth.IsAuthorized(ctx, telegramID)
		if err != nil {
			h.log.Error("authorization check", "telegram_id", telegramID, "error", err)
			h.deny(ctx, b, update, msgInternalErr)
			return
		}
		if !authorized {
			h.deny(ctx, b, update, msgAccessDenied)
			return
		}
		next(ctx, b, update)
	}
}

// deny отвечает отказом в форме, соответствующей типу апдейта:
// сообщению — реплай, нажатию кнопки — всплывающий алерт.
func (h *handler) deny(ctx context.Context, b *bot.Bot, update *models.Update, text string) {
	switch {
	case update.Message != nil:
		h.reply(ctx, b, update.Message.Chat.ID, text)
	case update.CallbackQuery != nil:
		h.answerCallback(ctx, b, update.CallbackQuery.ID, text)
	}
}

func (h *handler) reply(ctx context.Context, b *bot.Bot, chatID int64, text string) {
	if _, err := b.SendMessage(ctx, &bot.SendMessageParams{ChatID: chatID, Text: text}); err != nil {
		h.log.Error("send message", "chat_id", chatID, "error", err)
	}
}

func (h *handler) answerCallback(ctx context.Context, b *bot.Bot, callbackID, text string) {
	_, err := b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
		CallbackQueryID: callbackID,
		Text:            text,
	})
	if err != nil {
		h.log.Error("answer callback", "error", err)
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
