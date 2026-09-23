#!/usr/bin/env bash
set -euo pipefail

U="${1:-https://73aa008a39a0b6.lhr.life}"
PID="demo-$(date +%s)-$$"

echo "=== 1. GET /health ==="
curl -s -w "\n[%{http_code}]\n" "$U/health"

echo "=== 2. POST /process (маскирование, system без ключа) ==="
curl -s -X POST "$U/process" -H 'Content-Type: application/json' \
  -d '{"payload":"Клиент Иванов Иван Иванович, паспорт 4509 123456, email ivanov@test.ru, тел +7 912 345-67-89, карта 4276 1234 5678 9012","payload_id":"'$PID'"}' \
  -w "\n[%{http_code}]\n"

echo "=== 3. POST /process (демаскирование по payload_id) ==="
curl -s -X POST "$U/process" -H 'Content-Type: application/json' \
  -d '{"payload":"любой другой текст","payload_id":"'$PID'","allow_unmask":true}' \
  -w "\n[%{http_code}]\n"

echo "=== 4. Система chat: mask (token, API-ключ) ==="
curl -s -X POST "$U/process" -H 'Content-Type: application/json' -H 'X-API-Key: demo-chat-key' \
  -d '{"payload":"Клиент Иванов Иван, тел +7 912 345-67-89, паспорт 4509 123456","payload_id":"chat-'"$PID"'","system":"chat"}' \
  -w "\n[%{http_code}]\n"

echo "=== 5. Система chat: unmask (разрешено) ==="
curl -s -X POST "$U/process" -H 'Content-Type: application/json' -H 'X-API-Key: demo-chat-key' \
  -d '{"payload":"x","payload_id":"chat-'"$PID"'","system":"chat"}' \
  -w "\n[%{http_code}]\n"

echo "=== 6. Система analytics: маскируются только ТЕЛЕФОН и EMAIL ==="
curl -s -X POST "$U/process" -H 'Content-Type: application/json' -H 'X-API-Key: demo-analytics-key' \
  -d '{"payload":"Клиент Иванов Иван, тел +7 912 345-67-89, email i@t.ru, паспорт 4509 123456","payload_id":"an-'"$PID"'","system":"analytics"}' \
  -w "\n[%{http_code}]\n"

echo "=== 7. Система analytics: unmask (запрещено) ==="
curl -s -X POST "$U/process" -H 'Content-Type: application/json' -H 'X-API-Key: demo-analytics-key' \
  -d '{"payload":"x","payload_id":"an-'"$PID"'","system":"analytics"}' \
  -w "\n[%{http_code}]\n"

echo "=== 8. Ошибка: неверный API-ключ -> 401 ==="
curl -s -o /dev/null -w "[%{http_code}]\n" -X POST "$U/process" -H 'Content-Type: application/json' -H 'X-API-Key: wrong' \
  -d '{"payload":"тел +7 912 345-67-89","payload_id":"k-'"$PID"'","system":"chat"}'

echo "=== 9. Ошибка: неизвестная система -> 404 ==="
curl -s -o /dev/null -w "[%{http_code}]\n" -X POST "$U/process" -H 'Content-Type: application/json' \
  -d '{"payload":"тел +7 912 345-67-89","payload_id":"u-'"$PID"'","system":"nope"}'

echo "=== 10. Ошибка: отключённая система -> 403 ==="
curl -s -o /dev/null -w "[%{http_code}]\n" -X POST "$U/process" -H 'Content-Type: application/json' \
  -d '{"payload":"тел +7 912 345-67-89","payload_id":"d-'"$PID"'","system":"disabled_system"}'

echo "=== 11. Ошибки 400: нет payload_id / битый JSON ==="
curl -s -o /dev/null -w "[%{http_code}] " -X POST "$U/process" -H 'Content-Type: application/json' -d '{"payload":"текст"}'
curl -s -o /dev/null -w "[%{http_code}]\n" -X POST "$U/process" -H 'Content-Type: application/json' -d '{oops'

echo "=== 12. Ловушки (не должно маскироваться) ==="
curl -s -X POST "$U/process" -H 'Content-Type: application/json' \
  -d '{"payload":"Александр Пушкин написал роман; отделение банка на ул. Ленина, 10; банк открылся 12 марта 1998 года; VIN JTDBR90S780123456","payload_id":"fp-'"$PID"'"}' \
  -w "\n[%{http_code}]\n"

echo "=== 13. GET /metrics (первые строки Prometheus) ==="
curl -s "$U/metrics" | grep -E "^(pii_|go_goroutines)" | head -8

echo "=== 14. GET /openapi.yaml (спецификация контракта) ==="
curl -s "$U/openapi.yaml" | head -5
echo "..."

echo "=== 15. GET /docs (интерактивный Swagger UI, Try it out) ==="
curl -s -o /dev/null -w "[%{http_code}] %{content_type}\n" "$U/docs"
echo "Откройте $U/docs в браузере: кнопка Try it out на POST /process."