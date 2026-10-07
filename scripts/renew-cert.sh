#!/usr/bin/env bash
# Продление сертификата Let's Encrypt и установка его в nginx.
# Запускается из cron раз в сутки — см. README, раздел «Nginx и SSL».
#
# Почему ежедневно, а не раз в месяц: `certbot renew` ничего не делает,
# пока до истечения больше 30 дней, поэтому частый запуск бесплатен и
# даёт тридцать попыток вместо одной.
#
# Почему не просто `certbot renew`: certbot обновляет файлы в
# letsencrypt/live/, а nginx читает nginx/certs/. Без копирования и
# reload nginx продолжит отдавать прежний сертификат до истечения.
set -euo pipefail

cd "$(dirname "$0")/.."

# Домен берётся из WEBHOOK_URL, чтобы не держать его в двух местах
DOMAIN=$(sed -n 's#^WEBHOOK_URL=https://\([^/]*\)/.*#\1#p' .env | head -n1)
if [ -z "$DOMAIN" ]; then
    echo "renew-cert: не удалось определить домен — в .env нет строки" \
         "WEBHOOK_URL=https://<домен>/telegram/webhook" >&2
    exit 1
fi

LIVE="letsencrypt/live/$DOMAIN"
if [ ! -f "$LIVE/fullchain.pem" ]; then
    echo "renew-cert: нет $LIVE/fullchain.pem — сертификат ещё не выпускался." \
         "Сначала выполните первый выпуск по README." >&2
    exit 1
fi

docker run --rm \
    -v "$PWD/nginx/certbot:/var/www/certbot" \
    -v "$PWD/letsencrypt:/etc/letsencrypt" \
    certbot/certbot renew --webroot -w /var/www/certbot --quiet

# Если продлевать было нечего, файл не изменился — nginx не трогаем.
# cmp идёт по содержимому: в live/ лежат симлинки в archive/.
if cmp -s "$LIVE/fullchain.pem" nginx/certs/fullchain.pem; then
    exit 0
fi

cp "$LIVE/fullchain.pem" "$LIVE/privkey.pem" nginx/certs/
docker compose exec -T nginx nginx -s reload
echo "renew-cert: сертификат $DOMAIN обновлён, nginx перезагружен"
