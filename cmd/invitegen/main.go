// invitegen — админская утилита: генерирует одноразовые пригласительные
// токены, пишет их в invite_tokens и печатает готовые deep-link ссылки.
//
// Запуск на сервере (использует те же POSTGRES_* из .env):
//
//	docker compose run --rm --entrypoint /invitegen go_backend -n 5
package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"flag"
	"fmt"
	"os"
	"time"

	"gitlab.com/kabanza/mq-ticket-detector/internal/config"
	"gitlab.com/kabanza/mq-ticket-detector/internal/repository"
)

func main() {
	count := flag.Int("n", 1, "сколько пригласительных ссылок сгенерировать")
	flag.Parse()

	if err := run(*count); err != nil {
		fmt.Fprintln(os.Stderr, "ошибка:", err)
		os.Exit(1)
	}
}

func run(count int) error {
	if count < 1 || count > 1000 {
		return fmt.Errorf("количество должно быть от 1 до 1000, получено %d", count)
	}

	dsn, err := config.DatabaseDSNFromEnv()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := repository.NewPool(ctx, dsn)
	if err != nil {
		return err
	}
	defer pool.Close()
	repos := repository.New(pool)

	// BOT_USERNAME опционален: без него печатаем голые токены
	botUsername := os.Getenv("BOT_USERNAME")

	for i := 0; i < count; i++ {
		token, err := newToken()
		if err != nil {
			return err
		}
		if err := repos.Invites.Create(ctx, token); err != nil {
			return err
		}
		if botUsername != "" {
			fmt.Printf("https://t.me/%s?start=%s\n", botUsername, token)
		} else {
			fmt.Println(token)
		}
	}
	return nil
}

// newToken — 16 криптослучайных байт в base64url: 22 символа, укладывается
// в лимит Telegram на параметр deep-link (64 символа, [A-Za-z0-9_-]).
func newToken() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw[:]), nil
}
