package service

import (
	"context"
	"errors"
	"log/slog"
	"testing"
)

type fakeRedeemer struct {
	called bool
	ok     bool
	err    error
}

func (f *fakeRedeemer) RedeemInvite(_ context.Context, _ string, _ int64, _ string) (bool, error) {
	f.called = true
	return f.ok, f.err
}

type fakeUsers struct {
	active bool
}

func (f *fakeUsers) IsActive(context.Context, int64) (bool, error) { return f.active, nil }

func newAuth(r *fakeRedeemer, u *fakeUsers) *Auth {
	return NewAuth(r, u, slog.New(slog.DiscardHandler))
}

func TestRegisterByInviteEmptyToken(t *testing.T) {
	r := &fakeRedeemer{ok: true}
	ok, err := newAuth(r, &fakeUsers{}).RegisterByInvite(context.Background(), 1, "alice", "")

	if err != nil || ok {
		t.Fatalf("пустой токен: ok=%v, err=%v, ожидается отказ без ошибки", ok, err)
	}
	if r.called {
		t.Fatal("пустой токен не должен доходить до БД")
	}
}

func TestRegisterByInviteValid(t *testing.T) {
	ok, err := newAuth(&fakeRedeemer{ok: true}, &fakeUsers{}).
		RegisterByInvite(context.Background(), 1, "alice", "tok")

	if err != nil || !ok {
		t.Fatalf("валидный токен: ok=%v, err=%v, ожидается успех", ok, err)
	}
}

func TestRegisterByInviteUsedToken(t *testing.T) {
	ok, err := newAuth(&fakeRedeemer{ok: false}, &fakeUsers{}).
		RegisterByInvite(context.Background(), 1, "alice", "tok")

	if err != nil || ok {
		t.Fatalf("использованный токен: ok=%v, err=%v, ожидается отказ", ok, err)
	}
}

func TestRegisterByInviteError(t *testing.T) {
	dbErr := errors.New("db down")
	_, err := newAuth(&fakeRedeemer{err: dbErr}, &fakeUsers{}).
		RegisterByInvite(context.Background(), 1, "alice", "tok")

	if !errors.Is(err, dbErr) {
		t.Fatalf("ошибка БД должна пробрасываться, получено: %v", err)
	}
}
