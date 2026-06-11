-- Откат: таблицы удаляются в порядке, обратном создания (сначала зависимые).
DROP TABLE IF EXISTS subscriptions;
DROP TABLE IF EXISTS shows;
DROP TABLE IF EXISTS invite_tokens;
DROP TABLE IF EXISTS users;
