package telegram

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/go-telegram/bot/models"

	"gitlab.com/kabanza/mq-ticket-detector/internal/repository"
)

// callbackToggle — префикс callback_data кнопок шоу: "toggle_show:<id>".
const callbackToggle = "toggle_show:"

// showsKeyboard строит инлайн-клавиатуру /shows: одна кнопка на шоу,
// статус подписки — в тексте кнопки (✅/❌, как в ТЗ).
func showsKeyboard(shows []repository.ShowWithSubscription) *models.InlineKeyboardMarkup {
	rows := make([][]models.InlineKeyboardButton, 0, len(shows))
	for _, s := range shows {
		mark := "❌"
		if s.Subscribed {
			mark = "✅"
		}
		rows = append(rows, []models.InlineKeyboardButton{{
			Text:         fmt.Sprintf("%s %s", mark, s.Title),
			CallbackData: callbackToggle + strconv.FormatInt(s.ID, 10),
		}})
	}
	return &models.InlineKeyboardMarkup{InlineKeyboard: rows}
}

// parseToggleShowID достаёт id шоу из callback_data кнопки.
func parseToggleShowID(data string) (int64, error) {
	raw, ok := strings.CutPrefix(data, callbackToggle)
	if !ok {
		return 0, fmt.Errorf("callback data %q: нет префикса %q", data, callbackToggle)
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("callback data %q: %w", data, err)
	}
	return id, nil
}
