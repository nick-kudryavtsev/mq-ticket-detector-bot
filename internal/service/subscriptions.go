package service

import (
	"context"
	"log/slog"

	"gitlab.com/kabanza/mq-ticket-detector/internal/repository"
)

// ShowLister отдаёт все шоу с флагом подписки конкретного пользователя.
type ShowLister interface {
	ListWithSubscription(ctx context.Context, telegramID int64) ([]repository.ShowWithSubscription, error)
}

// SubscriptionToggler переключает подписку и возвращает новое состояние.
type SubscriptionToggler interface {
	Toggle(ctx context.Context, telegramID, showID int64) (bool, error)
}

// Subscriptions — сценарий 1 ТЗ, шаги 4–5: просмотр шоу и управление подписками.
type Subscriptions struct {
	shows ShowLister
	subs  SubscriptionToggler
	log   *slog.Logger
}

func NewSubscriptions(shows ShowLister, subs SubscriptionToggler, log *slog.Logger) *Subscriptions {
	return &Subscriptions{shows: shows, subs: subs, log: log}
}

// ListForUser — данные для инлайн-клавиатуры /shows.
func (s *Subscriptions) ListForUser(ctx context.Context, telegramID int64) ([]repository.ShowWithSubscription, error) {
	return s.shows.ListWithSubscription(ctx, telegramID)
}

// Toggle переключает подписку по нажатию кнопки.
func (s *Subscriptions) Toggle(ctx context.Context, telegramID, showID int64) (bool, error) {
	subscribed, err := s.subs.Toggle(ctx, telegramID, showID)
	if err != nil {
		return false, err
	}
	s.log.Info("subscription toggled",
		"telegram_id", telegramID, "show_id", showID, "subscribed", subscribed)
	return subscribed, nil
}
