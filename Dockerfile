# --- Этап 1: сборка ---
FROM golang:1.26-alpine AS builder

WORKDIR /app

# Сначала копируем только манифесты зависимостей: пока они не меняются,
# Docker переиспользует кеш слоя с выкачанными модулями
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO_ENABLED=0 — статический бинарник без libc;
# -s -w — отрезаем отладочные таблицы, образ меньше
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /bot ./cmd/bot && \
    CGO_ENABLED=0 go build -ldflags="-s -w" -o /invitegen ./cmd/invitegen

# --- Этап 2: рантайм ---
# distroless/static: нет shell и пакетного менеджера (минимальная поверхность
# атаки), но есть CA-сертификаты — нужны для HTTPS к Telegram API.
# Процесс работает от непривилегированного пользователя (тег :nonroot).
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /bot /bot
# Админская утилита генерации инвайтов:
#   docker compose run --rm --entrypoint /invitegen go_backend -n 5
COPY --from=builder /invitegen /invitegen

EXPOSE 8000

ENTRYPOINT ["/bot"]
