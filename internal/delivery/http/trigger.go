package http

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"gitlab.com/kabanza/mq-ticket-detector/internal/service"
)

// HeaderScraperAuth — кастомный заголовок авторизации вебхука
// changedetection.io (ТЗ §3.2).
const HeaderScraperAuth = "X-Changedetection-Auth"

// maxTriggerBodyBytes — потолок тела вебхука. changedetection кладёт в поле
// message ПОЛНЫЙ дифф страницы (может быть в десятки КБ, кириллица в UTF-8 —
// по 2 байта на символ). Нам из тела нужен только notification_tags, но
// распарсить надо весь JSON, иначе он обрежется на середине и Decode упадёт.
// 1 МБ с запасом; эндпоинт под авторизацией и доступен лишь из docker-сети.
const maxTriggerBodyBytes = 1 << 20

// TriggerService запускает рассылку по метке шоу.
type TriggerService interface {
	Trigger(ctx context.Context, label string) (service.TriggerResult, bool, error)
}

// triggerRequest — тело вебхука: {"notification_tags": "show_standup"}.
type triggerRequest struct {
	NotificationTags string `json:"notification_tags"`
}

// requireScraperAuth мгновенно отвечает 403 при отсутствии или несовпадении
// заголовка (ТЗ §3.2). Сравнение — constant-time, чтобы по времени ответа
// нельзя было подбирать секрет посимвольно.
func requireScraperAuth(logger *slog.Logger, secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got := r.Header.Get(HeaderScraperAuth)
			if subtle.ConstantTimeCompare([]byte(got), []byte(secret)) != 1 {
				logger.Warn("scraper auth failed", "remote", r.RemoteAddr, "path", r.URL.Path)
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func handleTrigger(logger *slog.Logger, svc TriggerService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req triggerRequest
		if err := json.NewDecoder(io.LimitReader(r.Body, maxTriggerBodyBytes)).Decode(&req); err != nil {
			http.Error(w, "invalid json body", http.StatusBadRequest)
			return
		}

		label := strings.TrimSpace(req.NotificationTags)
		if label == "" {
			http.Error(w, "notification_tags is required", http.StatusBadRequest)
			return
		}

		result, found, err := svc.Trigger(r.Context(), label)
		if err != nil {
			logger.Error("trigger", "label", label, "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if !found {
			// Скрейпер прислал тег, которого нет в shows, — вероятно,
			// опечатка в notification_tags или в БД
			logger.Warn("trigger for unknown label", "label", label)
			http.Error(w, "unknown label", http.StatusNotFound)
			return
		}

		status := "accepted"
		if result.Suppressed {
			// изменение учтено (last_changed_at обновлён), но рассылка
			// подавлена анти-спам окном
			status = "suppressed"
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted) // обработано; рассылка асинхронна
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":      status,
			"show":        result.ShowTitle,
			"subscribers": result.Subscribers,
		})
	}
}
