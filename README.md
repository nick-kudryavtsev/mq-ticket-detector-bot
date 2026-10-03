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
  -c "INSERT INTO shows (title, changedetection_label, url) \
      VALUES ('Стендап', 'show_standup', 'https://mediumquality.ru/standup');"
```

Шоу сразу появится в `/shows` у всех авторизованных пользователей.
`url` — страница покупки: она прикрепляется к уведомлению кнопкой
«🎟 Открыть страницу» (NULL — уведомление без кнопки).

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
     шоу по этой метке.

5. Поле **Notification Body** — задаёт, что попадёт в текст уведомления.
   Поставьте `{{diff_added}}` (только добавленные строки диффа): бэкенд
   возьмёт из него **первые 600 символов** и покажет под заголовком шоу.
   Пустой Body → уведомление без блока изменений (только «🔥 …» + кнопка).
   Title бэкенд игнорирует.

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
       "notification_body": "{{diff_added}}",
       "notification_urls": ["json://go_backend:8000/api/v1/trigger?+X-Changedetection-Auth=<секрет>&:notification_tags=show_standup"]}'
```

**Важно про кодировку**: если сайт отдаёт `Content-Type` без `charset`,
кириллический триггер-текст может не совпасть (страница декодируется
как latin-1). Реальные сайты обычно отдают charset корректно.

**Группы (tags) в changedetection в интеграции не участвуют** — это просто
папки для организации watch-ей. Метка шоу передаётся только через
`:notification_tags=...` в notification-URL. Правило: один watch = одно
шоу = свой URL со своей меткой.

**JS-страницы (виджеты билетных систем, напр. intickets.ru)**: их контент
подгружается JavaScript-ом, обычный HTTP видит пустую оболочку. Для таких
watch-ей в стеке есть `sockpuppetbrowser` (headless-Chrome) — у watch-а
ставится fetch backend «Chrome/Javascript». Обычным HTML-страницам (Tilda,
mediumquality.ru) браузер не нужен, они работают на «Plain HTTP».
Браузер прожорлив (лимит 768 МБ) — поднимайте его только при реальной
надобности. Некоторые виджеты (intickets.ru) отдают 403 на headless-Chrome:
лечится переопределением User-Agent на реальный браузер в настройках watch-а
(Request → Headers). Эти настройки живут в volume `changedet_data`, в git
их нет — при пересоздании скрейпера с нуля их нужно задать заново.

**Анти-дребезг (опционально)**: `NOTIFY_COOLDOWN` подавляет повторные
рассылки одного шоу внутри окна (окно скользящее, `last_changed_at`
обновляется на каждое срабатывание). **По умолчанию выключен (`0`)**:
в целевой нише Medium Quality «мусорная» правка страницы и старт продаж
разделены секундами, поэтому подписчик должен получать каждое изменение
и проверять страницу сам. Включайте (например `30m`) только для шумных
страниц вне основной ниши — иначе рискуете подавить настоящий старт.

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

## Мониторинг

Prometheus собирает метрики хоста с `node_exporter` (интервал 15с, retention
2 суток), Grafana их показывает. Оба сервиса настраиваются **файлами**, а не
руками в UI — конфигурация лежит в git:

| Файл | Что задаёт |
|---|---|
| `prometheus/prometheus.yml` | что собирать (`node_exporter` + сам Prometheus) |
| `grafana/provisioning/datasources/prometheus.yml` | data source с фиксированным `uid: prometheus` |
| `grafana/provisioning/dashboards/node-exporter.yml` | провайдер, читающий дашборды из каталога |
| `grafana/dashboards/*.json` | сами дашборды |

### Доступ к панели

Порт Grafana привязан к `127.0.0.1` сервера — снаружи панель недоступна, и
открывать под неё порт в firewall не нужно. Заходим SSH-туннелем:

```bash
ssh -L 3000:127.0.0.1:3000 <пользователь>@<сервер>
# затем в браузере http://localhost:3000, логин из GF_SECURITY_ADMIN_* в .env
```

Тот же приём для UI скрейпера: `ssh -L 5001:127.0.0.1:5001 …`.

### Дашборд Node Exporter Full (ID 1860)

Дашборд не импортируется через UI — он подхватывается из `grafana/dashboards/`
при старте контейнера. Скачать и положить в репозиторий (делается один раз,
результат коммитится):

```bash
curl -sS 'https://grafana.com/api/dashboards/1860/revisions/latest/download' \
  | sed 's/${DS_PROMETHEUS}/prometheus/g' \
  > grafana/dashboards/node-exporter-full.json
docker compose up -d grafana
```

`sed` обязателен: JSON с grafana.com ссылается на источник данных через
подстановку `${DS_PROMETHEUS}`, которая заполняется только при импорте руками.
При provisioning её надо заменить на `uid` нашего data source.

### Проверка, что retention 2d действует

Prometheus собирает собственные метрики, поэтому границу хранения видно запросом
(Grafana → Explore, либо curl изнутри сети):

```bash
docker exec ticket_prometheus wget -qO- \
  'http://localhost:9090/api/v1/query?query=time()-prometheus_tsdb_lowest_timestamp_seconds'
```

Через двое суток работы значение выходит на плато около `172800` (48 часов) и
больше не растёт — старые блоки удаляются. На свежем стеке оно равно времени
с момента запуска, и это нормально: проверять имеет смысл после двух суток
непрерывной работы.

### Диск

Дашборд 1860 показывает свободное место на хосте из коробки. Отдельно стоит
поглядывать на том скрейпера: **история снапшотов растёт без встроенного
потолка** (по файлу на каждое задетектированное изменение), и при
`NOTIFY_COOLDOWN=0` с частым интервалом проверки это основной источник роста.
Скриншоты, в отличие от истории, не копятся — на watch хранится один
перезаписываемый `last-screenshot.png`, его высота ограничена
`SCREENSHOT_MAX_HEIGHT=3000`.

```bash
docker compose exec changedetection du -sh /datastore
```

### Требования к машине

Стек рассчитан на **2 vCPU / 4 ГБ RAM / 20–25 ГБ SSD**. Лимиты памяти выставлены
всем сервисам (суммарно ~2.4 ГБ потолка, в простое около 1–1.3 ГБ); самый
тяжёлый — `sockpuppetbrowser` (768 МБ), поэтому на 2 ГБ стек не живёт.
Диск: образы ~3 ГБ (из них браузер с Chrome ~1.2 ГБ), плюс ~1.5 ГБ, если
собирать образ бэкенда на сервере через `docker compose up --build`.

Снаружи достаточно открыть **443** (вебхуки Telegram) и **22** (SSH и туннели);
**80** нужен только на время выпуска и продления сертификата. Порты Grafana,
скрейпера, бэкенда и БД наружу не публикуются.

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

Гоняются против ОТДЕЛЬНОЙ тестовой БД `ticket_bot_test` (тесты очищают
таблицы, поэтому дев-базу с реальными подписками не трогаем). Разовая
подготовка:

```bash
docker exec ticket_postgres createdb -U ticket_bot ticket_bot_test
set -a; source .env; set +a
docker compose run --rm migrator -path=/migrations \
  -database="postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@postgres_db:5432/ticket_bot_test?sslmode=disable" up
```

Сеть `internal_db` изолирована от интернета, поэтому контейнер с тестами
получает кеш Go-модулей с хоста:

```bash
docker run --rm \
  --network mq-ticket-detector_internal_db \
  -v "$PWD":/app -w /app \
  -v "$(go env GOMODCACHE)":/go/pkg/mod \
  -e GOTOOLCHAIN=local -e GOFLAGS=-buildvcs=false \
  -e TEST_DATABASE_DSN='postgres://ticket_bot:<пароль из .env>@postgres_db:5432/ticket_bot_test?sslmode=disable' \
  golang:1.26 go test -v -race -count=1 ./internal/repository/
```

После изменения миграций повторите команду `migrator ... up` для тестовой БД.
