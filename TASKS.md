# План работ

Каждая задача — отдельная ветка от `main` и отдельный Merge Request.
Задачи выполняются последовательно: каждая следующая опирается на результат предыдущей.

Формат ветки: `feature/<номер>-<краткое-имя>`.

---

## ✅ 1. `feature/1-docker-networks` — Каркас docker-compose и сетей

**Цель**: вся инфраструктура поднимается одной командой, сетевая изоляция работает.

- [x] Две сети: `external_web` (bridge) и `internal_db` (bridge, `internal: true`)
- [x] 7 сервисов, распределённых по сетям согласно ТЗ §2
- [x] Лимиты памяти: prometheus 400M, grafana 300M, node_exporter 50M
- [x] Заглушки `nginx.conf` и `prometheus.yml`, чтобы стек стартовал
- [x] `.gitignore`, `.env.example`; `.env` не попадает в git
- [x] Проверена изоляция: postgres не видит интернет, nginx не видит postgres

---

## ✅ 2. `feature/2-db-schema` — Схема БД и миграции

**Цель**: воспроизводимая схема PostgreSQL из ТЗ §4.

- [x] Выбрать и подключить инструмент миграций (`golang-migrate`)
- [x] Миграция `users`: telegram_id (BigInt PK), username, is_active (default true), updated_at
- [x] Миграция `invite_tokens`: id, token (unique), is_used (default false), created_at
- [x] Миграция `shows`: id, title, changedetection_label (unique), last_changed_at (nullable)
- [x] Миграция `subscriptions`: составной PK (user_id, show_id), FK с ON DELETE CASCADE
- [x] Запуск миграций при деплое (отдельный one-shot контейнер в compose)

**Готово, когда**: миграции применяются на чистую БД, `up`/`down` работают без ошибок.

---

## ✅ 3. `feature/3-go-skeleton` — Скелет Go-приложения и Dockerfile

**Цель**: компилируемое модульное приложение в контейнере вместо заглушки.

- [x] `go.mod`, структура: `/cmd/bot/main.go`, `/internal/config`, `/internal/repository`, `/internal/service`, `/internal/delivery/http`
- [x] Чтение конфигурации из переменных окружения (`.env`)
- [x] HTTP-сервер на `go-chi` (middleware RealIP, Recoverer) с эндпоинтом `/healthz`
- [x] Graceful shutdown: перехват SIGINT/SIGTERM, корректное закрытие сервера
- [x] Multi-stage Dockerfile (builder → distroless), образ 14MB
- [x] Замена alpine-заглушки в `docker-compose.yml` на `build: .`

**Готово, когда**: `docker compose up --build` собирает образ, `/healthz` отвечает 200 изнутри сети.

---

## ✅ 4. `feature/4-gitlab-ci` — Пайплайн CI

**Цель**: каждый следующий MR автоматически проверяется. Делаем рано, чтобы CI охранял всю последующую Go-разработку.

- [x] `.gitlab-ci.yml` со стадиями `lint` → `test` → `build`
- [x] `lint`: golangci-lint `v2.12.2-alpine` (v1.55 из ТЗ собран на Go 1.21 и несовместим с Go 1.26)
- [x] `test`: `go test -v -race -cover ./...` на `golang:1.26` (не alpine: `-race` требует glibc)
- [x] `build`: `docker build` для проверки Dockerfile (docker:27 + dind)
- [x] `workflow:rules`: запуск только на MR в `main` и на пуш в `main` (экономия минут)
- [ ] В настройках GitLab включить «Pipelines must succeed» для merge — **после слияния этого MR**

**Готово, когда**: пайплайн зелёный на MR, не запускается на промежуточных ветках без MR.

---

## ✅ 5. `feature/5-repository` — Слой доступа к данным

**Цель**: вся работа с PostgreSQL — через пул соединений и репозитории.

- [x] Пул соединений `pgxpool`, закрытие пула при shutdown (после остановки HTTP)
- [x] `UserRepo`: UpsertActive (создание/реактивация), Deactivate, IsActive
- [x] `RedeemInvite`: валидация + пометка токена и создание user в одной транзакции
- [x] `ShowRepo`: ListWithSubscription (для клавиатуры /shows), MarkChanged (по label)
- [x] `SubscriptionRepo`: Toggle (подписка/отписка), ActiveSubscriberIDs (для рассылки)
- [x] Интеграционные тесты против дев-БД из compose (env-gated, в CI скипаются)
- [x] Бонус: `/readyz` — readiness-проба с ping БД

**Готово, когда**: `go test -race` зелёный, методы покрывают все сценарии ТЗ §1.

---

## ✅ 6. `feature/6-bot-invites` — Бот: приватная регистрация по инвайтам

**Цель**: сценарий 1 (шаги 1–3): доступ строго по пригласительным ссылкам.

- [x] Подключена `go-telegram/bot` v1.21, режим polling (webhook — на шаге nginx)
- [x] `/start` без параметра или с невалидным токеном → «Доступ ограничен…», пользователь НЕ пишется в БД
- [x] `/start <token>`: валидация по `invite_tokens`, создание/реактивация user, токен помечается использованным, приветствие
- [x] Middleware `requireAuth`: дефолтный хендлер только для пользователей из `users`
- [x] Утилита `cmd/invitegen` (второй бинарник в образе): крипто-токены + готовые deep-link ссылки

**Готово, когда**: оба пути `/start` работают в живом Telegram, повторное использование токена отклоняется.

---

## ✅ 7. `feature/7-bot-subscriptions` — Бот: /shows и подписки

**Цель**: сценарий 1 (шаги 4–5): управление подписками через инлайн-клавиатуру.

- [x] `/shows`: инлайн-клавиатура со списком шоу и статусом `✅`/`❌` для текущего пользователя
- [x] Обработка `CallbackQuery`: toggle подписки в БД, тост-уведомление + перерисовка клавиатуры
- [x] `/shows` и колбэки недоступны неавторизованным (requireAuth расширен на CallbackQuery)

**Готово, когда**: подписка/отписка работает в живом Telegram, состояние сохраняется в БД.

---

## ✅ 8. `feature/8-trigger-api` — Вебхук скрейпера и массовая рассылка

**Цель**: сценарии 2 и 3: моментальные уведомления и обработка блокировок.

- [x] `POST /api/v1/trigger`: constant-time проверка `X-Changedetection-Auth`, иначе мгновенный 403
- [x] Парсинг `notification_tags`, поиск шоу по `changedetection_label`, обновление `last_changed_at`
- [x] Рассылка: пул из 8 горутин + rate-limiter 25 msg/s (лимит Telegram ~30), `sync.WaitGroup`
- [x] `bot.ErrorForbidden` от Telegram → `ErrBlockedByUser` → `is_active=false`, `updated_at=NOW()`
- [x] Graceful shutdown: `notifier.Wait` досылает текущую пачку до закрытия пула БД
- [x] Тесты: 403/404/400/202 на эндпоинте, веер на 40 подписчиков под `-race`

**Готово, когда**: curl с верным заголовком запускает рассылку, с неверным — 403; заблокировавшие бота помечаются неактивными.

---

## ✅ 9. `feature/9-nginx-ssl` — Боевой Nginx и SSL

**Цель**: ТЗ §3: весь внешний трафик — только через nginx:443.

- [x] Полный `nginx.conf`: server-блок на 443 (TLS 1.2/1.3), пути сертификатов, редирект 80 → 443
- [x] Проксирование `/telegram/webhook` на `go_backend:8000`
- [x] `/api/v1/trigger` снаружи НЕ проксируется (404 от nginx), прямые порты закрыты
- [x] Certbot webroot: ACME-путь на :80 + инструкция выпуска/продления в README

**Готово, когда**: HTTPS-запрос с хоста доходит до бэкенда, прямые порты бэкенда/БД снаружи закрыты.

---

## ✅ 10. `feature/10-bot-webhook-mode` — Режим webhook для бота

**Цель**: ТЗ §5.2: на сервере бот получает обновления через вебхук, а не polling.

- [x] Переключатель `BOT_MODE=polling|webhook` (+ `WEBHOOK_URL`, `TELEGRAM_WEBHOOK_SECRET` обязательны в webhook)
- [x] Регистрация вебхука при старте, валидация `X-Telegram-Bot-Api-Secret-Token` (встроена в библиотеку)
- [x] Удаление вебхука при shutdown (ретраи + обход бага lib v1.21 с пустыми params) и перед polling
- [x] `stop_grace_period: 30s` — Docker не убивает процесс до конца graceful-цепочки

**Готово, когда**: в режиме webhook сообщения доходят через nginx; в polling — без nginx (локально).

---

## ✅ 11. `feature/11-changedetection` — Настройка скрейпера

**Цель**: сценарий 2 (шаги 1–3): связать changedetection.io с бэкендом.

- [x] Watch: интервал 60s, триггер «Билеты в продаже» (проверено на управляемой тест-странице)
- [x] Notification: apprise `json://go_backend:8000/api/v1/trigger` + заголовок через `+X-Changedetection-Auth=...`
- [x] JSON-тело: метка шоу через `:notification_tags=<label>` (инъекция поля в payload)
- [x] Документация в README: добавление шоу (insert + watch, UI и API), нюанс с charset
- [x] Порт UI на хосте: 5001 (на macOS 5000 занят AirPlay), только localhost

**Готово, когда**: изменение тестовой страницы приводит к реальному сообщению в Telegram.

---

## 12. `feature/12-monitoring` — Grafana и дашборды

**Цель**: ТЗ §6: наблюдаемость сервера.

- [ ] Provisioning Grafana: Prometheus как data source (файлами, не руками)
- [ ] Импорт дашборда Node Exporter Full (ID 1860) через provisioning
- [ ] Решить доступ к панели: SSH-туннель или проксирование через nginx (sub-path)
- [ ] Проверить, что retention 2d у Prometheus действует

**Готово, когда**: дашборд 1860 показывает живые метрики CPU/RAM/диска хоста.
