// Интеграционные тесты слоя repository: гоняются против реального PostgreSQL.
// Без TEST_DATABASE_DSN пропускаются (в CI стадия test их скипает,
// локально запускаются против дев-БД из docker compose — см. README).
package repository_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"gitlab.com/kabanza/mq-ticket-detector/internal/repository"
)

func setup(t *testing.T) (*repository.Repository, *pgxpool.Pool) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("TEST_DATABASE_DSN не задан — интеграционные тесты пропущены")
	}

	pool, err := repository.NewPool(context.Background(), dsn)
	if err != nil {
		t.Fatalf("подключение к тестовой БД: %v", err)
	}
	t.Cleanup(pool.Close)

	// каждый тест начинает с чистых таблиц
	_, err = pool.Exec(context.Background(),
		`TRUNCATE users, invite_tokens, shows, subscriptions RESTART IDENTITY CASCADE`)
	if err != nil {
		t.Fatalf("очистка таблиц: %v", err)
	}

	return repository.New(pool), pool
}

func insertToken(t *testing.T, pool *pgxpool.Pool, token string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO invite_tokens (token) VALUES ($1)`, token); err != nil {
		t.Fatalf("вставка токена: %v", err)
	}
}

func insertShow(t *testing.T, pool *pgxpool.Pool, title, label string) int64 {
	t.Helper()
	var id int64
	if err := pool.QueryRow(context.Background(),
		`INSERT INTO shows (title, changedetection_label) VALUES ($1, $2) RETURNING id`,
		title, label).Scan(&id); err != nil {
		t.Fatalf("вставка шоу: %v", err)
	}
	return id
}

func TestRedeemInvite(t *testing.T) {
	repo, pool := setup(t)
	ctx := context.Background()
	insertToken(t, pool, "tok-1")

	ok, err := repo.RedeemInvite(ctx, "tok-1", 100, "alice")
	if err != nil {
		t.Fatalf("RedeemInvite: %v", err)
	}
	if !ok {
		t.Fatal("валидный токен должен погаситься")
	}

	active, err := repo.Users.IsActive(ctx, 100)
	if err != nil || !active {
		t.Fatalf("после погашения пользователь должен быть активен: active=%v, err=%v", active, err)
	}

	// повторное использование того же токена
	ok, err = repo.RedeemInvite(ctx, "tok-1", 200, "bob")
	if err != nil {
		t.Fatalf("повторный RedeemInvite: %v", err)
	}
	if ok {
		t.Fatal("одноразовый токен погасился дважды")
	}
	if active, _ := repo.Users.IsActive(ctx, 200); active {
		t.Fatal("пользователь не должен создаваться по использованному токену")
	}

	// несуществующий токен
	if ok, _ := repo.RedeemInvite(ctx, "no-such", 300, ""); ok {
		t.Fatal("несуществующий токен погасился")
	}
}

func TestUserDeactivateAndReactivate(t *testing.T) {
	repo, _ := setup(t)
	ctx := context.Background()

	if err := repo.Users.UpsertActive(ctx, 100, "alice"); err != nil {
		t.Fatalf("UpsertActive: %v", err)
	}

	// сценарий 3 ТЗ: Telegram вернул 403 — деактивируем
	if err := repo.Users.Deactivate(ctx, 100); err != nil {
		t.Fatalf("Deactivate: %v", err)
	}
	if active, _ := repo.Users.IsActive(ctx, 100); active {
		t.Fatal("после Deactivate пользователь активен")
	}

	// пользователь вернулся по новому инвайту — реактивация через upsert
	if err := repo.Users.UpsertActive(ctx, 100, "alice"); err != nil {
		t.Fatalf("повторный UpsertActive: %v", err)
	}
	if active, _ := repo.Users.IsActive(ctx, 100); !active {
		t.Fatal("после повторного UpsertActive пользователь неактивен")
	}

	// незнакомый пользователь — false без ошибки
	if active, err := repo.Users.IsActive(ctx, 999); err != nil || active {
		t.Fatalf("незнакомец: active=%v, err=%v", active, err)
	}
}

func TestSubscriptionToggleAndFanout(t *testing.T) {
	repo, pool := setup(t)
	ctx := context.Background()

	showID := insertShow(t, pool, "Стендап", "show_standup")
	_ = repo.Users.UpsertActive(ctx, 100, "alice")
	_ = repo.Users.UpsertActive(ctx, 200, "bob")

	for _, id := range []int64{100, 200} {
		subscribed, err := repo.Subscriptions.Toggle(ctx, id, showID)
		if err != nil || !subscribed {
			t.Fatalf("первый Toggle(%d): subscribed=%v, err=%v", id, subscribed, err)
		}
	}

	// bob заблокировал бота — в рассылку попадать не должен
	if err := repo.Users.Deactivate(ctx, 200); err != nil {
		t.Fatalf("Deactivate: %v", err)
	}

	ids, err := repo.Subscriptions.ActiveSubscriberIDs(ctx, showID)
	if err != nil {
		t.Fatalf("ActiveSubscriberIDs: %v", err)
	}
	if len(ids) != 1 || ids[0] != 100 {
		t.Fatalf("активные подписчики = %v, ожидается [100]", ids)
	}

	// отписка
	subscribed, err := repo.Subscriptions.Toggle(ctx, 100, showID)
	if err != nil || subscribed {
		t.Fatalf("второй Toggle: subscribed=%v, err=%v", subscribed, err)
	}
	if ids, _ := repo.Subscriptions.ActiveSubscriberIDs(ctx, showID); len(ids) != 0 {
		t.Fatalf("после отписки подписчики = %v, ожидается пусто", ids)
	}
}

func TestMarkChanged(t *testing.T) {
	repo, pool := setup(t)
	ctx := context.Background()
	insertShow(t, pool, "Стендап", "show_standup")

	before := time.Now().Add(-time.Second)
	show, found, err := repo.Shows.MarkChanged(ctx, "show_standup")
	if err != nil {
		t.Fatalf("MarkChanged: %v", err)
	}
	if !found {
		t.Fatal("зарегистрированная метка не найдена")
	}
	if show.Title != "Стендап" {
		t.Errorf("Title = %q, ожидается %q", show.Title, "Стендап")
	}
	if show.LastChangedAt == nil || show.LastChangedAt.Before(before) {
		t.Errorf("LastChangedAt = %v, ожидается свежий штамп", show.LastChangedAt)
	}

	if _, found, _ := repo.Shows.MarkChanged(ctx, "unknown_label"); found {
		t.Fatal("незарегистрированная метка нашлась")
	}
}

func TestListWithSubscription(t *testing.T) {
	repo, pool := setup(t)
	ctx := context.Background()

	standupID := insertShow(t, pool, "Стендап", "show_standup")
	insertShow(t, pool, "Импровизация", "show_improv")
	_ = repo.Users.UpsertActive(ctx, 100, "alice")
	if _, err := repo.Subscriptions.Toggle(ctx, 100, standupID); err != nil {
		t.Fatalf("Toggle: %v", err)
	}

	list, err := repo.Shows.ListWithSubscription(ctx, 100)
	if err != nil {
		t.Fatalf("ListWithSubscription: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("получено %d шоу, ожидается 2", len(list))
	}
	if !list[0].Subscribed || list[0].Title != "Стендап" {
		t.Errorf("первое шоу: %+v, ожидается подписка на Стендап", list[0])
	}
	if list[1].Subscribed {
		t.Errorf("второе шоу: %+v, подписки быть не должно", list[1])
	}
}
