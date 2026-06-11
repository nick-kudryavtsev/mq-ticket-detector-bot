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

## 4. `feature/4-gitlab-ci` — Пайплайн CI

**Цель**: каждый следующий MR автоматически проверяется. Делаем рано, чтобы CI охранял всю последующую Go-разработку.

- [ ] `.gitlab-ci.yml` со стадиями `lint` → `test` → `build`
- [ ] `lint`: golangci-lint (alpine-образ)
- [ ] `test`: `go test -v -race -cover ./...`
- [ ] `build`: `docker build` для проверки Dockerfile
- [ ] `workflow:rules`: запуск только на MR в `main` и на пуш в `main` (экономия минут)
- [ ] В настройках GitLab включить «Pipelines must succeed» для merge

**Готово, когда**: пайплайн зелёный на MR, не запускается на промежуточных ветках без MR.

---

## 5. `feature/5-repository` — Слой доступа к данным

**Цель**: вся работа с PostgreSQL — через пул соединений и репозитории.

- [ ] Пул соединений (`pgxpool` + `sqlx`, либо `gorm`), закрытие пула при shutdown
- [ ] `UserRepository`: создать/активировать, деактивировать (is_active=false + updated_at), выбрать активных подписчиков шоу
- [ ] `InviteTokenRepository`: проверить валидность, пометить использованным (в транзакции с созданием user)
- [ ] `ShowRepository`: список шоу, поиск по changedetection_label, обновить last_changed_at
- [ ] `SubscriptionRepository`: создать/удалить, проверить наличие
- [ ] Unit-тесты репозиториев (testcontainers или дев-БД из compose)

**Готово, когда**: `go test -race` зелёный, методы покрывают все сценарии ТЗ §1.

---

## 6. `feature/6-bot-invites` — Бот: приватная регистрация по инвайтам

**Цель**: сценарий 1 (шаги 1–3): доступ строго по пригласительным ссылкам.

- [ ] Подключить библиотеку бота (`go-telegram/bot` или `telego`), режим polling
- [ ] `/start` без параметра или с невалидным токеном → «Доступ ограничен…», пользователь НЕ пишется в БД
- [ ] `/start <token>`: валидация по `invite_tokens`, создание/реактивация user, токен помечается использованным, приветствие
- [ ] Middleware авторизации: команды доступны только пользователям из `users`
- [ ] Утилита/команда генерации инвайт-ссылок для админа

**Готово, когда**: оба пути `/start` работают в живом Telegram, повторное использование токена отклоняется.

---

## 7. `feature/7-bot-subscriptions` — Бот: /shows и подписки

**Цель**: сценарий 1 (шаги 4–5): управление подписками через инлайн-клавиатуру.

- [ ] `/shows`: инлайн-клавиатура со списком шоу и статусом `✅`/`❌` для текущего пользователя
- [ ] Обработка `CallbackQuery`: toggle подписки в БД + перерисовка клавиатуры
- [ ] `/shows` недоступна неавторизованным (middleware из задачи 6)

**Готово, когда**: подписка/отписка работает в живом Telegram, состояние сохраняется в БД.

---

## 8. `feature/8-trigger-api` — Вебхук скрейпера и массовая рассылка

**Цель**: сценарии 2 и 3: моментальные уведомления и обработка блокировок.

- [ ] `POST /api/v1/trigger`: валидация заголовка `X-Changedetection-Auth` из `.env`, иначе мгновенный 403
- [ ] Парсинг `notification_tags`, поиск шоу по `changedetection_label`, обновление `last_changed_at`
- [ ] Выборка активных подписчиков, рассылка пулом горутин (`sync.WaitGroup` / каналы) с учётом rate-limit Telegram
- [ ] Обработка `403 Forbidden` от Telegram → `is_active=false`, `updated_at=NOW()`
- [ ] Graceful shutdown: дождаться завершения текущей пачки рассылки
- [ ] Тесты: авторизация эндпоинта, конкурентная рассылка под `-race`

**Готово, когда**: curl с верным заголовком запускает рассылку, с неверным — 403; заблокировавшие бота помечаются неактивными.

---

## 9. `feature/9-nginx-ssl` — Боевой Nginx и SSL

**Цель**: ТЗ §3: весь внешний трафик — только через nginx:443.

- [ ] Полный `nginx.conf`: server-блок на 443, пути сертификатов Let's Encrypt, редирект 80 → 443
- [ ] Проксирование пути Telegram-вебхука на `go_backend:8000`
- [ ] `/api/v1/trigger` снаружи НЕ проксируется (он ходит по внутренней docker-сети)
- [ ] Решение по выпуску сертификатов (certbot webroot / DNS-челлендж) — задокументировать в README

**Готово, когда**: HTTPS-запрос с хоста доходит до бэкенда, прямые порты бэкенда/БД снаружи закрыты.

---

## 10. `feature/10-bot-webhook-mode` — Режим webhook для бота

**Цель**: ТЗ §5.2: на сервере бот получает обновления через вебхук, а не polling.

- [ ] Переключатель `BOT_MODE=polling|webhook`
- [ ] Регистрация вебхука при старте, валидация `X-Telegram-Bot-Api-Secret-Token`
- [ ] Удаление вебхука из Telegram при graceful shutdown

**Готово, когда**: в режиме webhook сообщения доходят через nginx; в polling — без nginx (локально).

---

## 11. `feature/11-changedetection` — Настройка скрейпера

**Цель**: сценарий 2 (шаги 1–3): связать changedetection.io с бэкендом.

- [ ] Watch на страницу Medium Quality, интервал 60s, триггер по ключевому слову «Билеты в продаже»
- [ ] Notification webhook: POST `http://go_backend:8000/api/v1/trigger` + заголовок `X-Changedetection-Auth`
- [ ] JSON-тело с `notification_tags` (метка шоу)
- [ ] Документация по добавлению нового шоу (watch + запись в `shows`)

**Готово, когда**: изменение тестовой страницы приводит к реальному сообщению в Telegram.

---

## 12. `feature/12-monitoring` — Grafana и дашборды

**Цель**: ТЗ §6: наблюдаемость сервера.

- [ ] Provisioning Grafana: Prometheus как data source (файлами, не руками)
- [ ] Импорт дашборда Node Exporter Full (ID 1860) через provisioning
- [ ] Решить доступ к панели: SSH-туннель или проксирование через nginx (sub-path)
- [ ] Проверить, что retention 2d у Prometheus действует

**Готово, когда**: дашборд 1860 показывает живые метрики CPU/RAM/диска хоста.
