package repository

import "time"

type User struct {
	TelegramID int64
	Username   string // пустая строка, если username в Telegram не задан
	IsActive   bool
	UpdatedAt  time.Time
}

type Show struct {
	ID                   int64
	Title                string
	ChangedetectionLabel string
	LastChangedAt        *time.Time // nil, пока изменений ещё не было
}

// ShowWithSubscription — строка для инлайн-клавиатуры /shows:
// само шоу плюс флаг «подписан ли текущий пользователь».
type ShowWithSubscription struct {
	Show
	Subscribed bool
}
