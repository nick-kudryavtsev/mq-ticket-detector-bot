# mq-ticket-detector

Telegram-бот на Go: моментально уведомляет подписчиков о старте продаж билетов,
когда скрейпер [changedetection.io](https://changedetection.io) фиксирует изменения
на сайте Medium Quality. План работ — в [TASKS.md](TASKS.md).

## Запуск

```bash
cp .env.example .env   # заполнить значения
docker compose up -d   # вся инфраструктура + миграции применяются автоматически
```

## Telegram-бот

1. Создайте бота у [@BotFather](https://t.me/BotFather) (`/newbot`), скопируйте токен.
2. В `.env` заполните `BOT_TOKEN` и `BOT_USERNAME` (юзернейм без `@`).
   Без `BOT_TOKEN` приложение сознательно не стартует.
3. Пересоберите бэкенд: `docker compose up -d --build go_backend`.
4. Сгенерируйте пригласительные ссылки:

   ```bash
   docker compose run --rm --entrypoint /invitegen go_backend -n 5
   ```

5. Откройте ссылку в Telegram и нажмите Start. Бот пускает только
   по валидной одноразовой ссылке; `/start` без неё отвечает отказом.

## Управление шоу

Добавление шоу — два шага: запись в БД и watch в скрейпере. Связка —
строка-метка (`changedetection_label` = тег в notification-URL watch-а).

**Шаг 1.** Запись в `shows`:

```bash
docker exec ticket_postgres psql -U ticket_bot -d ticket_bot \
  -c "INSERT INTO shows (title, changedetection_label) VALUES ('Стендап', 'show_standup');"
```

Шоу сразу появится в `/shows` у всех авторизованных пользователей.

**Шаг 2.** Watch в changedetection.io (см. следующий раздел).

## Настройка changedetection.io

Веб-интерфейс скрейпера доступен только с localhost:
локально — <http://localhost:5001> (на macOS порт 5000 занят AirPlay),
на сервере — через SSH-туннель: `ssh -L 5001:localhost:5001 user@server`.

Для каждого шоу создаётся watch:

1. **URL** — страница шоу на сайте Medium Quality.
2. **Recheck time** — 60 секунд (ТЗ, сценарий 2).
3. Вкладка **Filters & Triggers → Trigger/wait for text** — `Билеты в продаже`:
   изменение засчитывается, только если на странице появился этот текст.
4. Вкладка **Notifications → Notification URL List**:

   ```
   json://go_backend:8000/api/v1/trigger?+X-Changedetection-Auth=<CHANGEDETECTION_AUTH_TOKEN из .env>&:notification_tags=<changedetection_label шоу>
   ```

   - `json://` — POST по http (внутри docker-сети, в интернет не выходит);
   - `+Имя=значение` добавляет HTTP-заголовок (авторизация на бэкенде);
   - `:notification_tags=...` добавляет поле в JSON-тело — бэкенд найдёт
     шоу по этой метке. Title/Body можно оставить любыми: бэкенд читает
     только `notification_tags`.

Кнопка **Send test notification** на вкладке Notifications дёргает боевую
рассылку — все подписчики шоу получат уведомление, удобно для проверки.

То же самое можно сделать через API скрейпера (`x-api-key` — в Settings → API):

```bash
curl -X POST http://localhost:5001/api/v1/watch \
  -H "x-api-key: <ключ>" -H 'Content-Type: application/json' \
  -d '{"url": "https://mediumquality.ru/standup",
       "title": "Стендап",
       "time_between_check": {"seconds": 60},
       "trigger_text": ["Билеты в продаже"],
       "notification_urls": ["json://go_backend:8000/api/v1/trigger?+X-Changedetection-Auth=<секрет>&:notification_tags=show_standup"]}'
```

**Важно про кодировку**: если сайт отдаёт `Content-Type` без `charset`,
кириллический триггер-текст может не совпасть (страница декодируется
как latin-1). Реальные сайты обычно отдают charset корректно.

## Nginx и SSL

Весь внешний трафик идёт через nginx:443; наружу проксируется только
`/telegram/webhook`. Сертификаты nginx читает из `nginx/certs/`
(`fullchain.pem` + `privkey.pem`).

**Локально** (самоподписанный, для проверки):

```bash
openssl req -x509 -newkey rsa:2048 -nodes \
  -keyout nginx/certs/privkey.pem -out nginx/certs/fullchain.pem \
  -days 365 -subj "/CN=localhost"
```

**На сервере** (Let's Encrypt, webroot-челлендж — порт 80 уже отдаёт
`/.well-known/acme-challenge/` из `nginx/certbot/`):

```bash
docker run --rm \
  -v "$PWD/nginx/certbot:/var/www/certbot" \
  -v "$PWD/letsencrypt:/etc/letsencrypt" \
  certbot/certbot certonly --webroot -w /var/www/certbot \
  -d ваш-домен.ru --email admin@ваш-домен.ru --agree-tos --no-eff-email

cp letsencrypt/live/ваш-домен.ru/fullchain.pem nginx/certs/
cp letsencrypt/live/ваш-домен.ru/privkey.pem nginx/certs/
docker compose restart nginx
```

Продление — тот же `certonly` по cron раз в месяц (Let's Encrypt живёт 90 дней).

## Вебхук скрейпера

`POST /api/v1/trigger` принимает уведомления changedetection.io. Защита —
заголовок `X-Changedetection-Auth` со значением `CHANGEDETECTION_AUTH_TOKEN`
из `.env` (сгенерировать: `openssl rand -hex 32`). Тело:
`{"notification_tags": "<changedetection_label шоу>"}`.

Симуляция срабатывания скрейпера (изнутри docker-сети, наружу порт закрыт):

```bash
SECRET=$(grep '^CHANGEDETECTION_AUTH_TOKEN=' .env | cut -d= -f2)
docker exec ticket_nginx wget -qO- \
  --header "X-Changedetection-Auth: ${SECRET}" \
  --post-data '{"notification_tags":"show_standup"}' \
  http://go_backend:8000/api/v1/trigger
```

Ответы: `202` — рассылка запущена, `403` — неверный секрет,
`404` — метка не зарегистрирована в таблице `shows`.

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
