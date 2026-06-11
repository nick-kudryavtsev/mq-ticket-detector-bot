package repository

import (
	"context"
	"fmt"
)

type InviteRepo struct {
	db db
}

// Create регистрирует новый пригласительный токен (используется
// админской утилитой invitegen). Гашение токена — в Repository.RedeemInvite.
func (r *InviteRepo) Create(ctx context.Context, token string) error {
	_, err := r.db.Exec(ctx,
		`INSERT INTO invite_tokens (token) VALUES ($1)`, token)
	if err != nil {
		return fmt.Errorf("create invite token: %w", err)
	}
	return nil
}
