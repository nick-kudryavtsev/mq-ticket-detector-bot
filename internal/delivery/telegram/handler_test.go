package telegram

import "testing"

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
