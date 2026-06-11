package telegram

import (
	"testing"

	"gitlab.com/kabanza/mq-ticket-detector/internal/repository"
)

func TestShowsKeyboard(t *testing.T) {
	kb := showsKeyboard([]repository.ShowWithSubscription{
		{Show: repository.Show{ID: 1, Title: "Стендап"}, Subscribed: true},
		{Show: repository.Show{ID: 42, Title: "Импровизация"}, Subscribed: false},
	})

	if len(kb.InlineKeyboard) != 2 {
		t.Fatalf("рядов в клавиатуре: %d, ожидается 2", len(kb.InlineKeyboard))
	}

	first := kb.InlineKeyboard[0][0]
	if first.Text != "✅ Стендап" {
		t.Errorf("текст кнопки с подпиской: %q, ожидается %q", first.Text, "✅ Стендап")
	}
	if first.CallbackData != "toggle_show:1" {
		t.Errorf("callback_data: %q, ожидается %q", first.CallbackData, "toggle_show:1")
	}

	second := kb.InlineKeyboard[1][0]
	if second.Text != "❌ Импровизация" {
		t.Errorf("текст кнопки без подписки: %q, ожидается %q", second.Text, "❌ Импровизация")
	}
	if second.CallbackData != "toggle_show:42" {
		t.Errorf("callback_data: %q, ожидается %q", second.CallbackData, "toggle_show:42")
	}
}

func TestParseToggleShowID(t *testing.T) {
	if id, err := parseToggleShowID("toggle_show:42"); err != nil || id != 42 {
		t.Errorf("toggle_show:42 -> id=%d, err=%v, ожидается 42", id, err)
	}
	for _, bad := range []string{"toggle_show:", "toggle_show:abc", "other:42", "42"} {
		if _, err := parseToggleShowID(bad); err == nil {
			t.Errorf("parseToggleShowID(%q) должен возвращать ошибку", bad)
		}
	}
}
