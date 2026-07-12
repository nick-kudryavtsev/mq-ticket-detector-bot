package telegram

import (
	"strings"
	"testing"
)

func TestStartMessages(t *testing.T) {
	// ТЗ, сценарий 1: текст отказа должен оставаться дословным.
	want := "Доступ ограничен. Этот бот работает только по пригласительным ссылкам."
	if msgAccessDenied != want {
		t.Errorf("msgAccessDenied изменён: %q", msgAccessDenied)
	}

	// И приветствие, и «с возвращением» содержат инструкцию с упоминанием /shows.
	for name, m := range map[string]string{"welcome": msgWelcome, "welcomeBack": msgWelcomeBack} {
		if !strings.Contains(m, msgInstruction) {
			t.Errorf("%s не содержит инструкцию", name)
		}
		if !strings.Contains(m, "/shows") {
			t.Errorf("%s не упоминает /shows", name)
		}
	}
}

func TestStartPayload(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"/start", ""},
		{"/start ", ""},
		{"/start abc123", "abc123"},
		{"/start@my_bot abc123", "abc123"},
		{"/start   abc123  ", "abc123"},
	}
	for _, c := range cases {
		if got := startPayload(c.in); got != c.want {
			t.Errorf("startPayload(%q) = %q, ожидается %q", c.in, got, c.want)
		}
	}
}
