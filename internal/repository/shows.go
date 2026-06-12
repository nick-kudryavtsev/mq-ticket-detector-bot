package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

type ShowRepo struct {
	db db
}

// ListWithSubscription возвращает все шоу с флагом подписки конкретного
// пользователя — ровно то, что нужно для отрисовки клавиатуры /shows.
func (r *ShowRepo) ListWithSubscription(ctx context.Context, telegramID int64) ([]ShowWithSubscription, error) {
	rows, err := r.db.Query(ctx, `
		SELECT s.id, s.title, s.changedetection_label, s.last_changed_at,
		       (sub.user_id IS NOT NULL) AS subscribed
		FROM shows s
		LEFT JOIN subscriptions sub ON sub.show_id = s.id AND sub.user_id = $1
		ORDER BY s.id`,
		telegramID)
	if err != nil {
		return nil, fmt.Errorf("list shows: %w", err)
	}
	defer rows.Close()

	var result []ShowWithSubscription
	for rows.Next() {
		var s ShowWithSubscription
		if err := rows.Scan(&s.ID, &s.Title, &s.ChangedetectionLabel, &s.LastChangedAt, &s.Subscribed); err != nil {
			return nil, fmt.Errorf("scan show: %w", err)
		}
		result = append(result, s)
	}
	return result, rows.Err()
}

// MarkChanged обновляет last_changed_at шоу по метке скрейпера и возвращает
// шоу вместе с ПРЕДЫДУЩИМ значением штампа — по нему сервис решает,
// не слишком ли часто страница «шумит» (анти-спам).
// found=false — метка в базе не зарегистрирована.
//
// Подзапрос в RETURNING читает таблицу в снапшоте на начало стейтмента,
// то есть отдаёт значение ДО обновления.
func (r *ShowRepo) MarkChanged(ctx context.Context, label string) (show Show, prev *time.Time, found bool, err error) {
	var s Show
	var prevChanged *time.Time
	err = r.db.QueryRow(ctx, `
		UPDATE shows SET last_changed_at = NOW()
		WHERE changedetection_label = $1
		RETURNING id, title, changedetection_label, COALESCE(url, ''), last_changed_at,
		          (SELECT s2.last_changed_at FROM shows s2 WHERE s2.changedetection_label = $1)`,
		label).Scan(&s.ID, &s.Title, &s.ChangedetectionLabel, &s.URL, &s.LastChangedAt, &prevChanged)
	if errors.Is(err, pgx.ErrNoRows) {
		return Show{}, nil, false, nil
	}
	if err != nil {
		return Show{}, nil, false, fmt.Errorf("mark show %q changed: %w", label, err)
	}
	return s, prevChanged, true, nil
}
