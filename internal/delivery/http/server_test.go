package http

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gitlab.com/kabanza/mq-ticket-detector/internal/service"
)

const testSecret = "test-scraper-secret"

// fakePinger подменяет пул БД в тестах.
type fakePinger struct {
	err error
}

func (f fakePinger) Ping(context.Context) error { return f.err }

// fakeTrigger подменяет сервис рассылки.
type fakeTrigger struct {
	result service.TriggerResult
	found  bool
	err    error
	gotLbl string
}

func (f *fakeTrigger) Trigger(_ context.Context, label string) (service.TriggerResult, bool, error) {
	f.gotLbl = label
	return f.result, f.found, f.err
}

func serve(t *testing.T, db Pinger, trigger TriggerService, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	srv := NewServer(":0", slog.New(slog.DiscardHandler), db, trigger, testSecret)
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)
	return rec
}

func get(t *testing.T, db Pinger, path string) *httptest.ResponseRecorder {
	t.Helper()
	return serve(t, db, &fakeTrigger{}, httptest.NewRequest(http.MethodGet, path, nil))
}

func postTrigger(t *testing.T, trigger TriggerService, header, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/trigger", strings.NewReader(body))
	if header != "" {
		req.Header.Set(HeaderScraperAuth, header)
	}
	return serve(t, fakePinger{}, trigger, req)
}

func TestHealthz(t *testing.T) {
	rec := get(t, fakePinger{}, "/healthz")

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /healthz: статус %d, ожидается %d", rec.Code, http.StatusOK)
	}
	body, _ := io.ReadAll(rec.Body)
	if string(body) != "ok" {
		t.Errorf("тело ответа %q, ожидается %q", body, "ok")
	}
}

func TestReadyzOK(t *testing.T) {
	rec := get(t, fakePinger{}, "/readyz")

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /readyz: статус %d, ожидается %d", rec.Code, http.StatusOK)
	}
}

func TestReadyzDatabaseDown(t *testing.T) {
	rec := get(t, fakePinger{err: context.DeadlineExceeded}, "/readyz")

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("GET /readyz при недоступной БД: статус %d, ожидается %d",
			rec.Code, http.StatusServiceUnavailable)
	}
}

func TestUnknownRouteReturns404(t *testing.T) {
	rec := get(t, fakePinger{}, "/nope")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /nope: статус %d, ожидается %d", rec.Code, http.StatusNotFound)
	}
}

func TestTriggerWithoutHeader(t *testing.T) {
	trigger := &fakeTrigger{found: true}
	rec := postTrigger(t, trigger, "", `{"notification_tags":"show_standup"}`)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("без заголовка: статус %d, ожидается 403", rec.Code)
	}
	if trigger.gotLbl != "" {
		t.Fatal("неавторизованный запрос дошёл до сервиса")
	}
}

func TestTriggerWrongSecret(t *testing.T) {
	rec := postTrigger(t, &fakeTrigger{found: true}, "wrong-secret", `{"notification_tags":"show_standup"}`)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("неверный секрет: статус %d, ожидается 403", rec.Code)
	}
}

func TestTriggerOK(t *testing.T) {
	trigger := &fakeTrigger{
		result: service.TriggerResult{ShowTitle: "Стендап", Subscribers: 5},
		found:  true,
	}
	rec := postTrigger(t, trigger, testSecret, `{"notification_tags":"show_standup"}`)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("валидный запрос: статус %d, ожидается 202", rec.Code)
	}
	if trigger.gotLbl != "show_standup" {
		t.Errorf("в сервис пришла метка %q, ожидается show_standup", trigger.gotLbl)
	}
	body, _ := io.ReadAll(rec.Body)
	for _, want := range []string{`"accepted"`, `"Стендап"`, `"subscribers":5`} {
		if !strings.Contains(string(body), want) {
			t.Errorf("в ответе нет %s: %s", want, body)
		}
	}
}

func TestTriggerUnknownLabel(t *testing.T) {
	rec := postTrigger(t, &fakeTrigger{found: false}, testSecret, `{"notification_tags":"nope"}`)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("неизвестная метка: статус %d, ожидается 404", rec.Code)
	}
}

func TestTriggerBadBody(t *testing.T) {
	for name, body := range map[string]string{
		"кривой json": `{не json`,
		"пустой тег":  `{"notification_tags":"  "}`,
		"нет поля":    `{}`,
		"пустое тело": ``,
	} {
		rec := postTrigger(t, &fakeTrigger{found: true}, testSecret, body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: статус %d, ожидается 400", name, rec.Code)
		}
	}
}
