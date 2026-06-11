package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/time/rate"

	"gitlab.com/kabanza/mq-ticket-detector/internal/repository"
)

// ErrBlockedByUser — Telegram ответил 403: пользователь заблокировал бота
// или удалил чат. Адаптер отправки транслирует ошибку библиотеки в эту.
var ErrBlockedByUser = errors.New("bot blocked by user")

type ShowMarker interface {
	MarkChanged(ctx context.Context, label string) (repository.Show, bool, error)
}

type SubscriberLister interface {
	ActiveSubscriberIDs(ctx context.Context, showID int64) ([]int64, error)
}

type UserDeactivator interface {
	Deactivate(ctx context.Context, telegramID int64) error
}

type MessageSender interface {
	SendNotification(ctx context.Context, telegramID int64, text string) error
}

// TriggerResult — ответ вебхуку: что нашли и скольким будем слать.
type TriggerResult struct {
	ShowTitle   string
	Subscribers int
}

// Notifier — сценарии 2 и 3 ТЗ: веерная рассылка уведомлений пулом горутин
// и деактивация пользователей, заблокировавших бота.
type Notifier struct {
	shows  ShowMarker
	subs   SubscriberLister
	users  UserDeactivator
	sender MessageSender
	log    *slog.Logger

	workers    int
	limiter    *rate.Limiter
	sendWindow time.Duration

	wg sync.WaitGroup // активные рассылки; Wait() ждёт их при shutdown
}

type NotifierOption func(*Notifier)

// WithWorkers задаёт размер пула горутин-отправителей.
func WithWorkers(n int) NotifierOption {
	return func(nf *Notifier) { nf.workers = n }
}

// WithSendRate ограничивает скорость отправки (сообщений в секунду).
func WithSendRate(perSecond float64, burst int) NotifierOption {
	return func(nf *Notifier) { nf.limiter = rate.NewLimiter(rate.Limit(perSecond), burst) }
}

func NewNotifier(shows ShowMarker, subs SubscriberLister, users UserDeactivator,
	sender MessageSender, log *slog.Logger, opts ...NotifierOption) *Notifier {
	n := &Notifier{
		shows:  shows,
		subs:   subs,
		users:  users,
		sender: sender,
		log:    log,
		// 8 горутин хватает: лимитер всё равно держит общий темп.
		workers: 8,
		// Глобальный лимит Telegram ~30 msg/s; держимся ниже с запасом.
		limiter:    rate.NewLimiter(25, 25),
		sendWindow: 5 * time.Minute,
	}
	for _, opt := range opts {
		opt(n)
	}
	return n
}

// Trigger — сценарий 2 ТЗ: обновляет last_changed_at шоу по метке скрейпера
// и запускает асинхронную рассылку активным подписчикам.
// found=false — метка не зарегистрирована в таблице shows.
func (n *Notifier) Trigger(ctx context.Context, label string) (TriggerResult, bool, error) {
	show, found, err := n.shows.MarkChanged(ctx, label)
	if err != nil || !found {
		return TriggerResult{}, found, err
	}

	ids, err := n.subs.ActiveSubscriberIDs(ctx, show.ID)
	if err != nil {
		return TriggerResult{}, true, err
	}

	result := TriggerResult{ShowTitle: show.Title, Subscribers: len(ids)}
	n.log.Info("trigger accepted", "label", label, "show", show.Title, "subscribers", len(ids))
	if len(ids) == 0 {
		return result, true, nil
	}

	// Рассылка живёт дольше HTTP-запроса и не зависит от сигнала остановки
	// процесса: graceful shutdown дожидается её через Wait. Поэтому контекст —
	// собственный, с потолком sendWindow, а не ctx запроса.
	n.wg.Add(1)
	go func() {
		defer n.wg.Done()
		bctx, cancel := context.WithTimeout(context.Background(), n.sendWindow)
		defer cancel()
		n.broadcast(bctx, show.Title, ids)
	}()

	return result, true, nil
}

// broadcast веером раздаёт ids пулу из workers горутин (ТЗ §5.3).
func (n *Notifier) broadcast(ctx context.Context, title string, ids []int64) {
	text := fmt.Sprintf("🔥 Внимание! Стартовали продажи билетов на %s!", title)

	jobs := make(chan int64)
	var sent, blocked, failed atomic.Int64

	var wg sync.WaitGroup
	for i := 0; i < n.workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for id := range jobs {
				if err := n.limiter.Wait(ctx); err != nil {
					failed.Add(1)
					continue // ctx истёк — дочитываем канал без отправки
				}
				err := n.sender.SendNotification(ctx, id, text)
				switch {
				case errors.Is(err, ErrBlockedByUser):
					// Сценарий 3 ТЗ: пользователь заблокировал бота —
					// деактивируем, дальше рассылки его пропускают.
					blocked.Add(1)
					if derr := n.users.Deactivate(ctx, id); derr != nil {
						n.log.Error("deactivate blocked user", "telegram_id", id, "error", derr)
					} else {
						n.log.Info("user deactivated (blocked the bot)", "telegram_id", id)
					}
				case err != nil:
					failed.Add(1)
					n.log.Error("send notification", "telegram_id", id, "error", err)
				default:
					sent.Add(1)
				}
			}
		}()
	}

	for _, id := range ids {
		jobs <- id
	}
	close(jobs)
	wg.Wait()

	n.log.Info("broadcast finished", "show", title,
		"sent", sent.Load(), "blocked", blocked.Load(), "failed", failed.Load())
}

// Wait блокируется до завершения всех активных рассылок — вызывается
// при graceful shutdown, чтобы «дослать текущую пачку» (ТЗ §5.4).
func (n *Notifier) Wait(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		n.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("broadcasts not finished: %w", ctx.Err())
	}
}
