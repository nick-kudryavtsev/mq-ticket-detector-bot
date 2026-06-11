package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

type UserRepo struct {
	db db
}

// UpsertActive создаёт пользователя или реактивирует существующего
// (повторный приход по новой пригласительной ссылке после блокировки).
func (r *UserRepo) UpsertActive(ctx context.Context, telegramID int64, username string) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO users (telegram_id, username, is_active, updated_at)
		VALUES ($1, NULLIF($2, ''), TRUE, NOW())
		ON CONFLICT (telegram_id) DO UPDATE
		SET username = EXCLUDED.username, is_active = TRUE, updated_at = NOW()`,
		telegramID, username)
	if err != nil {
		return fmt.Errorf("upsert user %d: %w", telegramID, err)
	}
	return nil
}

// Deactivate выставляет is_active = false (сценарий 3 ТЗ: пользователь
// заблокировал бота, Telegram вернул 403). Дальнейшие рассылки его пропускают.
func (r *UserRepo) Deactivate(ctx context.Context, telegramID int64) error {
	_, err := r.db.Exec(ctx, `
		UPDATE users SET is_active = FALSE, updated_at = NOW()
		WHERE telegram_id = $1`,
		telegramID)
	if err != nil {
		return fmt.Errorf("deactivate user %d: %w", telegramID, err)
	}
	return nil
}

// IsActive отвечает, авторизован ли пользователь (есть в users и активен).
// Используется для контроля доступа к командам бота.
func (r *UserRepo) IsActive(ctx context.Context, telegramID int64) (bool, error) {
	var active bool
	err := r.db.QueryRow(ctx,
		`SELECT is_active FROM users WHERE telegram_id = $1`, telegramID).Scan(&active)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil // незнакомец — не ошибка, просто нет доступа
	}
	if err != nil {
		return false, fmt.Errorf("check user %d: %w", telegramID, err)
	}
	return active, nil
}
