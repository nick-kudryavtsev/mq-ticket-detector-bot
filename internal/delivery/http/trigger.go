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
		// Лимит на тело: вебхук — это пара коротких полей, не файл
		if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil {
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

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted) // рассылка пошла асинхронно
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":      "accepted",
			"show":        result.ShowTitle,
			"subscribers": result.Subscribers,
		})
	}
}
