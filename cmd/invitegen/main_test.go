package main

import "testing"

func TestNewToken(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		token, err := newToken()
		if err != nil {
			t.Fatalf("newToken: %v", err)
		}
		if len(token) != 22 {
			t.Fatalf("длина токена %d, ожидается 22: %q", len(token), token)
		}
		for _, r := range token {
			ok := r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_'
			if !ok {
				t.Fatalf("недопустимый для deep-link символ %q в токене %q", r, token)
			}
		}
		if seen[token] {
			t.Fatalf("токен повторился: %q", token)
		}
		seen[token] = true
	}
}
