package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository — фасад над всеми репозиториями. Сервисный слой получает
// его одним зависимостью и не строит SQL сам.
type Repository struct {
	pool          *pgxpool.Pool
	Users         *UserRepo
	Invites       *InviteRepo
	Shows         *ShowRepo
	Subscriptions *SubscriptionRepo
}

func New(pool *pgxpool.Pool) *Repository {
	return &Repository{
		pool:          pool,
		Users:         &UserRepo{db: pool},
		Invites:       &InviteRepo{db: pool},
		Shows:         &ShowRepo{db: pool},
		Subscriptions: &SubscriptionRepo{db: pool},
	}
}

// RedeemInvite атомарно гасит пригласительный токен и создаёт (или
// реактивирует) пользователя — сценарий 1 ТЗ, шаг 3. Одна транзакция:
// токен не может «сгореть» без создания пользователя и наоборот.
//
// ok=false — токен не существует или уже использован; UPDATE ... WHERE
// is_used = FALSE гарантирует одноразовость даже при гонке двух запросов.
func (r *Repository) RedeemInvite(ctx context.Context, token string, telegramID int64, username string) (ok bool, err error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op после успешного Commit

	var tokenID int64
	err = tx.QueryRow(ctx, `
		UPDATE invite_tokens SET is_used = TRUE
		WHERE token = $1 AND is_used = FALSE
		RETURNING id`,
		token).Scan(&tokenID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("redeem token: %w", err)
	}

	users := &UserRepo{db: tx}
	if err := users.UpsertActive(ctx, telegramID, username); err != nil {
		return false, err
	}

	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit tx: %w", err)
	}
	return true, nil
}
