package repository

import (
	"context"
	"fmt"
)

type SubscriptionRepo struct {
	db db
}

// Toggle переключает подписку (нажатие кнопки шоу в /shows):
// не было — создаёт, была — удаляет. Возвращает новое состояние.
// INSERT ... ON CONFLICT DO NOTHING делает операцию безопасной
// при двойном клике по кнопке.
func (r *SubscriptionRepo) Toggle(ctx context.Context, telegramID, showID int64) (bool, error) {
	tag, err := r.db.Exec(ctx, `
		INSERT INTO subscriptions (user_id, show_id)
		VALUES ($1, $2)
		ON CONFLICT DO NOTHING`,
		telegramID, showID)
	if err != nil {
		return false, fmt.Errorf("subscribe %d to show %d: %w", telegramID, showID, err)
	}
	if tag.RowsAffected() == 1 {
		return true, nil // подписки не было — создали
	}

	_, err = r.db.Exec(ctx,
		`DELETE FROM subscriptions WHERE user_id = $1 AND show_id = $2`,
		telegramID, showID)
	if err != nil {
		return false, fmt.Errorf("unsubscribe %d from show %d: %w", telegramID, showID, err)
	}
	return false, nil
}

// ActiveSubscriberIDs — главный запрос рассылки (сценарий 2 ТЗ, шаг 6):
// telegram_id всех АКТИВНЫХ подписчиков шоу. Заблокировавшие бота
// (is_active = false) отфильтровываются ещё в БД.
func (r *SubscriptionRepo) ActiveSubscriberIDs(ctx context.Context, showID int64) ([]int64, error) {
	rows, err := r.db.Query(ctx, `
		SELECT u.telegram_id
		FROM users u
		JOIN subscriptions s ON s.user_id = u.telegram_id
		WHERE s.show_id = $1 AND u.is_active`,
		showID)
	if err != nil {
		return nil, fmt.Errorf("list subscribers of show %d: %w", showID, err)
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan subscriber id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
