# mq-ticket-detector

Telegram-бот на Go: моментально уведомляет подписчиков о старте продаж билетов,
когда скрейпер [changedetection.io](https://changedetection.io) фиксирует изменения
на сайте Medium Quality. План работ — в [TASKS.md](TASKS.md).

## Запуск

```bash
cp .env.example .env   # заполнить значения
docker compose up -d   # вся инфраструктура + миграции применяются автоматически
```

## Разработка

```bash
go test ./...   # юнит-тесты (интеграционные скипаются без TEST_DATABASE_DSN)
go vet ./...
```

Линт — тем же образом, что в CI:

```bash
docker run --rm -v "$PWD":/app -w /app \
  golangci/golangci-lint:v2.12.2-alpine golangci-lint run ./...
```

### Интеграционные тесты репозиториев

Гоняются против дев-БД из compose. Сеть `internal_db` изолирована от интернета,
поэтому контейнер с тестами получает кеш Go-модулей с хоста:

```bash
docker compose up -d postgres_db migrator

docker run --rm \
  --network mq-ticket-detector_internal_db \
  -v "$PWD":/app -w /app \
  -v "$(go env GOMODCACHE)":/go/pkg/mod \
  -e GOTOOLCHAIN=local -e GOFLAGS=-buildvcs=false \
  -e TEST_DATABASE_DSN='postgres://ticket_bot:<пароль из .env>@postgres_db:5432/ticket_bot?sslmode=disable' \
  golang:1.26 go test -v -race -count=1 ./internal/repository/
```

**Внимание**: тесты очищают таблицы (`TRUNCATE`) — только для дев-БД.
