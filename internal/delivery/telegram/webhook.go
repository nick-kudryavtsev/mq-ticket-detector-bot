package telegram

import (
	"context"
	"fmt"
	"time"

	"github.com/go-telegram/bot"
)

// SetupWebhook регистрирует вебхук в Telegram (BOT_MODE=webhook, ТЗ §5.2):
// апдейты пойдут POST-запросами на url с секретом в заголовке
// X-Telegram-Bot-Api-Secret-Token.
func SetupWebhook(ctx context.Context, b *bot.Bot, url, secret string) error {
	ok, err := b.SetWebhook(ctx, &bot.SetWebhookParams{
		URL:         url,
		SecretToken: secret,
	})
	if err != nil {
		return fmt.Errorf("set telegram webhook: %w", err)
	}
	if !ok {
		return fmt.Errorf("set telegram webhook: API вернул false")
	}
	return nil
}

// RemoveWebhook удаляет вебхук: при graceful shutdown (ТЗ §5.4)
// и перед запуском polling (активный вебхук конфликтует с getUpdates).
//
// До трёх попыток: к моменту shutdown keep-alive-соединение с Telegram
// часто уже закрыто сервером, и первый POST может оборваться пустым
// ответом — это не повод оставлять вебхук висеть.
func RemoveWebhook(ctx context.Context, b *bot.Bot) error {
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		if attempt > 1 {
			select {
			case <-time.After(time.Second):
			case <-ctx.Done():
				return fmt.Errorf("delete telegram webhook: %w", ctx.Err())
			}
		}

		// Свой таймаут на каждую попытку: повисший запрос не должен
		// съесть весь бюджет shutdown.
		//
		// DropPendingUpdates: true не только семантика, но и обход бага
		// go-telegram/bot v1.21: с пустыми params библиотека шлёт
		// multipart-форму без единого поля, на которую Telegram отвечает
		// пустым телом, и декодирование ответа падает. Поле заодно
		// отбрасывает апдейты, накопившиеся за время простоя, — для
		// этого бота они неактуальны.
		attemptCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		ok, err := b.DeleteWebhook(attemptCtx, &bot.DeleteWebhookParams{DropPendingUpdates: true})
		cancel()
		if err == nil && ok {
			return nil
		}
		if err != nil {
			lastErr = err
		} else {
			lastErr = fmt.Errorf("API вернул false")
		}
	}
	return fmt.Errorf("delete telegram webhook: %w", lastErr)
}
