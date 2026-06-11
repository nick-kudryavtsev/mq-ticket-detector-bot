package telegram

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-telegram/bot"

	"gitlab.com/kabanza/mq-ticket-detector/internal/service"
)

// Sender — адаптер service.MessageSender поверх Telegram-бота.
type Sender struct {
	b *bot.Bot
}

func NewSender(b *bot.Bot) *Sender {
	return &Sender{b: b}
}

// SendNotification шлёт текст пользователю в личку. Ответ Telegram
// 403 Forbidden (пользователь заблокировал бота или удалил чат)
// транслируется в service.ErrBlockedByUser — сценарий 3 ТЗ.
func (s *Sender) SendNotification(ctx context.Context, telegramID int64, text string) error {
	_, err := s.b.SendMessage(ctx, &bot.SendMessageParams{ChatID: telegramID, Text: text})
	if err == nil {
		return nil
	}
	if errors.Is(err, bot.ErrorForbidden) {
		return fmt.Errorf("%w: %w", service.ErrBlockedByUser, err)
	}
	return fmt.Errorf("send to %d: %w", telegramID, err)
}
