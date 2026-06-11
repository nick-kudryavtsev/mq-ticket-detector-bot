package service

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"gitlab.com/kabanza/mq-ticket-detector/internal/repository"
)

type fakeShows struct {
	list []repository.ShowWithSubscription
}

func (f *fakeShows) ListWithSubscription(context.Context, int64) ([]repository.ShowWithSubscription, error) {
	return f.list, nil
}

type fakeToggler struct {
	subscribed bool
	err        error
}

func (f *fakeToggler) Toggle(context.Context, int64, int64) (bool, error) {
	return f.subscribed, f.err
}

func TestSubscriptionsToggle(t *testing.T) {
	svc := NewSubscriptions(&fakeShows{}, &fakeToggler{subscribed: true}, slog.New(slog.DiscardHandler))

	subscribed, err := svc.Toggle(context.Background(), 1, 2)
	if err != nil || !subscribed {
		t.Fatalf("Toggle: subscribed=%v, err=%v, ожидается подписка", subscribed, err)
	}
}

func TestSubscriptionsToggleError(t *testing.T) {
	dbErr := errors.New("db down")
	svc := NewSubscriptions(&fakeShows{}, &fakeToggler{err: dbErr}, slog.New(slog.DiscardHandler))

	if _, err := svc.Toggle(context.Background(), 1, 2); !errors.Is(err, dbErr) {
		t.Fatalf("ошибка БД должна пробрасываться, получено: %v", err)
	}
}

func TestSubscriptionsListForUser(t *testing.T) {
	want := []repository.ShowWithSubscription{
		{Show: repository.Show{ID: 1, Title: "Стендап"}, Subscribed: true},
	}
	svc := NewSubscriptions(&fakeShows{list: want}, &fakeToggler{}, slog.New(slog.DiscardHandler))

	got, err := svc.ListForUser(context.Background(), 1)
	if err != nil || len(got) != 1 || got[0].Title != "Стендап" {
		t.Fatalf("ListForUser = %+v, err=%v", got, err)
	}
}
