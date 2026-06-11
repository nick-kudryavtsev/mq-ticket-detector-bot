-- Схема БД из ТЗ §4. Одна атомарная миграция: golang-migrate выполняет её
-- внутри транзакции, поэтому либо создаётся вся схема, либо ничего.

-- Пользователи бота. PK — telegram_id, своего автоинкремента не нужно.
CREATE TABLE users (
    telegram_id BIGINT PRIMARY KEY,
    username    TEXT,
    is_active   BOOLEAN     NOT NULL DEFAULT TRUE,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Одноразовые пригласительные токены для deep-linking (/start <token>).
CREATE TABLE invite_tokens (
    id         INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    token      TEXT        NOT NULL UNIQUE,
    is_used    BOOLEAN     NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Отслеживаемые шоу. changedetection_label — связка со скрейпером
-- (значение notification_tags в вебхуке).
CREATE TABLE shows (
    id                    INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    title                 TEXT NOT NULL,
    changedetection_label TEXT NOT NULL UNIQUE,
    last_changed_at       TIMESTAMPTZ
);

-- Подписки: связь many-to-many с каскадным удалением с обеих сторон.
CREATE TABLE subscriptions (
    user_id BIGINT  NOT NULL REFERENCES users (telegram_id) ON DELETE CASCADE,
    show_id INTEGER NOT NULL REFERENCES shows (id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, show_id)
);

-- Главный запрос системы — «все подписчики шоу X» при рассылке.
-- Составной PK начинается с user_id и тут не помогает, нужен отдельный индекс.
CREATE INDEX idx_subscriptions_show_id ON subscriptions (show_id);
