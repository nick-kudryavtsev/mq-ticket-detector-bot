package http

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

// fakePinger подменяет пул БД в тестах.
type fakePinger struct {
	err error
}

func (f fakePinger) Ping(context.Context) error { return f.err }

func get(t *testing.T, db Pinger, path string) *httptest.ResponseRecorder {
	t.Helper()
	srv := NewServer(":0", slog.New(slog.DiscardHandler), db)
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)
	return rec
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
