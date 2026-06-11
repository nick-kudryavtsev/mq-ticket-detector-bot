package service

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"sync"
	"testing"
	"time"

	"gitlab.com/kabanza/mq-ticket-detector/internal/repository"
)

type fakeMarker struct {
	show  repository.Show
	found bool
}

func (f *fakeMarker) MarkChanged(context.Context, string) (repository.Show, bool, error) {
	return f.show, f.found, nil
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
	blockedID int64
	failID    int64
}

func (f *fakeSender) SendNotification(_ context.Context, id int64, _ string) error {
	if id == f.blockedID {
		return ErrBlockedByUser
	}
	if id == f.failID {
		return errors.New("telegram unavailable")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, id)
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

	res, found, err := n.Trigger(context.Background(), "show_standup")
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

	_, found, err := n.Trigger(context.Background(), "nope")
	if err != nil || found {
		t.Fatalf("неизвестная метка: found=%v, err=%v", found, err)
	}
	waitNotifier(t, n)
	if len(sender.sent) != 0 {
		t.Errorf("рассылка по неизвестной метке: %v", sender.sent)
	}
}

func TestTriggerNoSubscribers(t *testing.T) {
	marker := &fakeMarker{show: repository.Show{ID: 1, Title: "Стендап"}, found: true}
	sender := &fakeSender{}
	n := newTestNotifier(marker, &fakeSubscribers{}, &fakeDeactivator{}, sender)

	res, found, err := n.Trigger(context.Background(), "show_standup")
	if err != nil || !found {
		t.Fatalf("Trigger: found=%v, err=%v", found, err)
	}
	if res.Subscribers != 0 {
		t.Errorf("Subscribers = %d, ожидается 0", res.Subscribers)
	}
	waitNotifier(t, n)
}
