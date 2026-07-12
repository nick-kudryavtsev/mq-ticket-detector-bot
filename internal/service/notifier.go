package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/time/rate"

	"gitlab.com/kabanza/mq-ticket-detector/internal/repository"
)

// ErrBlockedByUser — Telegram ответил 403: пользователь заблокировал бота
// или удалил чат. Адаптер отправки транслирует ошибку библиотеки в эту.
var ErrBlockedByUser = errors.New("bot blocked by user")

// maxChangeTextRunes — сколько символов диффа со скрейпера показываем
// в уведомлении. Считаем в рунах, а не байтах: кириллица в UTF-8 —
// 2 байта на символ, обрезка по байтам разрезала бы символ пополам.
const maxChangeTextRunes = 600

type ShowMarker interface {
	MarkChanged(ctx context.Context, label string) (show repository.Show, prev *time.Time, found bool, err error)
}

type SubscriberLister interface {
	ActiveSubscriberIDs(ctx context.Context, showID int64) ([]int64, error)
}

type UserDeactivator interface {
	Deactivate(ctx context.Context, telegramID int64) error
}

type MessageSender interface {
	// url непустой — к сообщению прикрепляется кнопка-ссылка на страницу шоу.
	SendNotification(ctx context.Context, telegramID int64, text, url string) error
}

// TriggerResult — ответ вебхуку: что нашли и скольким будем слать.
type TriggerResult struct {
	ShowTitle   string
	Subscribers int
	// Suppressed — изменение зафиксировано (last_changed_at обновлён),
	// но рассылка не запускалась: предыдущее срабатывание было слишком
	// недавно (анти-спам, окно cooldown).
	Suppressed bool
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
	cooldown   time.Duration // минимальная пауза между рассылками одного шоу

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

// WithCooldown задаёт окно подавления повторных рассылок одного шоу.
// 0 отключает анти-спам.
func WithCooldown(d time.Duration) NotifierOption {
	return func(nf *Notifier) { nf.cooldown = d }
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
		// По умолчанию ВЫКЛЮЧЕН: в нише Medium Quality «мусорная» правка
		// страницы и старт продаж разделены секундами — подавлять нельзя
		// ничего. Включается опцией для шумных страниц вне ниши.
		cooldown: 0,
	}
	for _, opt := range opts {
		opt(n)
	}
	return n
}

// Trigger — сценарий 2 ТЗ: обновляет last_changed_at шоу по метке скрейпера
// и запускает асинхронную рассылку активным подписчикам.
// found=false — метка не зарегистрирована в таблице shows.
//
// Анти-спам: если предыдущее срабатывание этого шоу было меньше cooldown
// назад, рассылка подавляется — «шумная» страница (новости, реклама,
// мелкие правки после старта продаж) даёт одно уведомление, а не поток.
// changeText — дифф со скрейпера (поле message вебхука); первые
// maxChangeTextRunes символов уходят в тело уведомления. Пустая строка —
// уведомление без блока изменений.
func (n *Notifier) Trigger(ctx context.Context, label, changeText string) (TriggerResult, bool, error) {
	show, prev, found, err := n.shows.MarkChanged(ctx, label)
	if err != nil || !found {
		return TriggerResult{}, found, err
	}

	if n.cooldown > 0 && prev != nil && time.Since(*prev) < n.cooldown {
		n.log.Info("trigger suppressed by cooldown",
			"label", label, "show", show.Title,
			"previous_change", prev.Format(time.RFC3339), "cooldown", n.cooldown.String())
		return TriggerResult{ShowTitle: show.Title, Suppressed: true}, true, nil
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
		n.broadcast(bctx, show, changeText, ids)
	}()

	return result, true, nil
}

// broadcast веером раздаёт ids пулу из workers горутин (ТЗ §5.3).
func (n *Notifier) broadcast(ctx context.Context, show repository.Show, changeText string, ids []int64) {
	// Нейтральный заголовок: скрейпер ловит ЛЮБОЕ изменение страницы, а не
	// строго старт продаж (в отличие от буквального текста ТЗ §5.3). Что
	// именно поменялось — видно из тела диффа ниже и по кнопке-ссылке.
	text := fmt.Sprintf("🔔 Обновление на странице шоу «%s»", show.Title)
	if body := truncateRunes(changeText, maxChangeTextRunes); body != "" {
		text += "\n\n" + body
	}

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
				err := n.sender.SendNotification(ctx, id, text, show.URL)
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

	n.log.Info("broadcast finished", "show", show.Title,
		"sent", sent.Load(), "blocked", blocked.Load(), "failed", failed.Load())
}

// truncateRunes обрезает строку до max рун (не байт) и ставит «…», если
// что-то отрезали. Пустую/пробельную строку возвращает как "".
func truncateRunes(s string, max int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return strings.TrimSpace(string(r[:max])) + "…"
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
