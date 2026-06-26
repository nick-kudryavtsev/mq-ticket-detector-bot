package service

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"gitlab.com/kabanza/mq-ticket-detector/internal/repository"
)

type fakeMarker struct {
	show  repository.Show
	prev  *time.Time // предыдущий last_changed_at (для cooldown)
	found bool
}

func (f *fakeMarker) MarkChanged(context.Context, string) (repository.Show, *time.Time, bool, error) {
	return f.show, f.prev, f.found, nil
}

type fakeSubscribers struct {
	ids []int64
}

func (f *fakeSubscribers) ActiveSubscriberIDs(context.Context, int64) ([]int64, error) {
	return f.ids, nil
}

type fakeDeactivator struct {
	mu  sync.Mutex
	ids []int64
}

func (f *fakeDeactivator) Deactivate(_ context.Context, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ids = append(f.ids, id)
	return nil
}

// fakeSender потокобезопасно записывает отправки; blockedID получает
// ErrBlockedByUser, failID — обычную ошибку.
type fakeSender struct {
	mu        sync.Mutex
	sent      []int64
	urls      []string
	texts     []string
	blockedID int64
	failID    int64
}

func (f *fakeSender) SendNotification(_ context.Context, id int64, text, url string) error {
	if id == f.blockedID {
		return ErrBlockedByUser
	}
	if id == f.failID {
		return errors.New("telegram unavailable")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, id)
	f.urls = append(f.urls, url)
	f.texts = append(f.texts, text)
	return nil
}

func newTestNotifier(m *fakeMarker, subs *fakeSubscribers, d *fakeDeactivator, s *fakeSender) *Notifier {
	return NewNotifier(m, subs, d, s, slog.New(slog.DiscardHandler),
		WithWorkers(4), WithSendRate(100000, 1000)) // в тестах темп не ограничиваем
}

func waitNotifier(t *testing.T, n *Notifier) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := n.Wait(ctx); err != nil {
		t.Fatalf("рассылка не завершилась: %v", err)
	}
}

// Главный тест конкурентности: 40 подписчиков, один заблокировал бота,
// у одного сетевая ошибка. Запускается с -race в CI.
func TestTriggerFanout(t *testing.T) {
	ids := make([]int64, 40)
	for i := range ids {
		ids[i] = int64(i + 1)
	}
	marker := &fakeMarker{show: repository.Show{ID: 1, Title: "Стендап"}, found: true}
	sender := &fakeSender{blockedID: 7, failID: 13}
	deact := &fakeDeactivator{}

	n := newTestNotifier(marker, &fakeSubscribers{ids: ids}, deact, sender)

	res, found, err := n.Trigger(context.Background(), "show_standup", "")
	if err != nil || !found {
		t.Fatalf("Trigger: found=%v, err=%v", found, err)
	}
	if res.Subscribers != 40 || res.ShowTitle != "Стендап" {
		t.Fatalf("TriggerResult = %+v", res)
	}

	waitNotifier(t, n)

	if len(sender.sent) != 38 { // 40 минус заблокировавший и сбойный
		t.Errorf("отправлено %d сообщений, ожидается 38", len(sender.sent))
	}
	if !slices.Equal(deact.ids, []int64{7}) {
		t.Errorf("деактивированы %v, ожидается [7]", deact.ids)
	}
}

func TestTriggerUnknownLabel(t *testing.T) {
	sender := &fakeSender{}
	n := newTestNotifier(&fakeMarker{found: false}, &fakeSubscribers{}, &fakeDeactivator{}, sender)

	_, found, err := n.Trigger(context.Background(), "nope", "")
	if err != nil || found {
		t.Fatalf("неизвестная метка: found=%v, err=%v", found, err)
	}
	waitNotifier(t, n)
	if len(sender.sent) != 0 {
		t.Errorf("рассылка по неизвестной метке: %v", sender.sent)
	}
}

// Cooldown: недавнее предыдущее срабатывание подавляет рассылку,
// давнее и нулевой cooldown — нет.
func TestTriggerCooldown(t *testing.T) {
	recent := time.Now().Add(-time.Minute)
	old := time.Now().Add(-48 * time.Hour)
	show := repository.Show{ID: 1, Title: "Стендап"}

	cases := []struct {
		name       string
		prev       *time.Time
		cooldown   time.Duration
		suppressed bool
	}{
		{"первое срабатывание", nil, 24 * time.Hour, false},
		{"недавнее — подавляется", &recent, 24 * time.Hour, true},
		{"давнее — проходит", &old, 24 * time.Hour, false},
		{"cooldown выключен", &recent, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sender := &fakeSender{}
			n := NewNotifier(&fakeMarker{show: show, prev: tc.prev, found: true},
				&fakeSubscribers{ids: []int64{100}}, &fakeDeactivator{}, sender,
				slog.New(slog.DiscardHandler),
				WithWorkers(2), WithSendRate(100000, 1000), WithCooldown(tc.cooldown))

			res, found, err := n.Trigger(context.Background(), "show_standup", "")
			if err != nil || !found {
				t.Fatalf("Trigger: found=%v, err=%v", found, err)
			}
			if res.Suppressed != tc.suppressed {
				t.Fatalf("Suppressed = %v, ожидается %v", res.Suppressed, tc.suppressed)
			}
			waitNotifier(t, n)
			wantSent := 1
			if tc.suppressed {
				wantSent = 0
			}
			if len(sender.sent) != wantSent {
				t.Fatalf("отправлено %d, ожидается %d", len(sender.sent), wantSent)
			}
		})
	}
}

// URL шоу доезжает до отправителя (кнопка-ссылка в уведомлении).
func TestTriggerPassesShowURL(t *testing.T) {
	marker := &fakeMarker{
		show:  repository.Show{ID: 1, Title: "ПВН", URL: "https://example.com/pvn"},
		found: true,
	}
	sender := &fakeSender{}
	n := newTestNotifier(marker, &fakeSubscribers{ids: []int64{100}}, &fakeDeactivator{}, sender)

	if _, _, err := n.Trigger(context.Background(), "pvn", ""); err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	waitNotifier(t, n)
	if len(sender.urls) != 1 || sender.urls[0] != "https://example.com/pvn" {
		t.Fatalf("url у отправителя = %v, ожидается ссылка шоу", sender.urls)
	}
}

// Дифф со скрейпера попадает в текст уведомления под заголовком шоу.
func TestTriggerIncludesChangeText(t *testing.T) {
	marker := &fakeMarker{show: repository.Show{ID: 1, Title: "КРАСНОДАР"}, found: true}
	sender := &fakeSender{}
	n := newTestNotifier(marker, &fakeSubscribers{ids: []int64{100}}, &fakeDeactivator{}, sender)

	if _, _, err := n.Trigger(context.Background(), "krd", "(added) 1 000 ₽"); err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	waitNotifier(t, n)

	if len(sender.texts) != 1 {
		t.Fatalf("отправок %d, ожидается 1", len(sender.texts))
	}
	got := sender.texts[0]
	if !strings.Contains(got, "КРАСНОДАР") || !strings.Contains(got, "(added) 1 000 ₽") {
		t.Errorf("текст не содержит заголовок и дифф: %q", got)
	}
}

// Длинный дифф обрезается по рунам (кириллица), а не по байтам.
func TestTriggerTruncatesChangeText(t *testing.T) {
	marker := &fakeMarker{show: repository.Show{ID: 1, Title: "Шоу"}, found: true}
	sender := &fakeSender{}
	n := newTestNotifier(marker, &fakeSubscribers{ids: []int64{100}}, &fakeDeactivator{}, sender)

	long := strings.Repeat("я", 1000) // 1000 рун = 2000 байт
	if _, _, err := n.Trigger(context.Background(), "krd", long); err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	waitNotifier(t, n)

	body := sender.texts[0]
	runes := []rune(body)
	// заголовок + перевод строки + 600 рун диффа + «…»
	if cnt := strings.Count(body, "я"); cnt != maxChangeTextRunes {
		t.Errorf("символов диффа %d, ожидается %d", cnt, maxChangeTextRunes)
	}
	if !strings.HasSuffix(body, "…") {
		t.Errorf("обрезанный текст должен оканчиваться на «…»: %q", string(runes[len(runes)-10:]))
	}
}

func TestTriggerNoSubscribers(t *testing.T) {
	marker := &fakeMarker{show: repository.Show{ID: 1, Title: "Стендап"}, found: true}
	sender := &fakeSender{}
	n := newTestNotifier(marker, &fakeSubscribers{}, &fakeDeactivator{}, sender)

	res, found, err := n.Trigger(context.Background(), "show_standup", "")
	if err != nil || !found {
		t.Fatalf("Trigger: found=%v, err=%v", found, err)
	}
	if res.Subscribers != 0 {
		t.Errorf("Subscribers = %d, ожидается 0", res.Subscribers)
	}
	waitNotifier(t, n)
}
