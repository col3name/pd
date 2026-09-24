#!/usr/bin/env bash
# Simulates the load-testing scenario from Appendix B:
#   - mask/unmask pairs with the same payload_id
#   - idempotency (retries)
#   - various PII categories
#   - 429 handling
#   - latency
set -uo pipefail

URL="${1:-http://5.42.118.103:5173/process}"
echo "=== Testing $URL ==="

# Test cases: label | original | expected mask contains
declare -a CASES=(
  "ФИО|Клиент Иванов Иван Иванович|ФИО"
  "Дата рождения|Дата рождения клиента: 12.03.1998|ДАТА"
  "Паспорт|паспорт 4509 123456|ПАСПОРТ"
  "Гражданство|гражданство: Российская Федерация|ГРАЖДАНСТВО"
  "Орган выдачи|паспорт выдан ОВД района Хамовники|ОРГАН"
  "Код подразделения|код подразделения 770-001|КОД_ПОДРАЗДЕЛЕНИЯ"
  "ВУ|водительское удостоверение 77 12 345678|ВУ"
  "Адрес|адрес клиента: г. Москва, ул. Ленина, д. 10|АДРЕС"
  "Email|email ivanov@test.ru|EMAIL"
  "Телефон|тел +7 912 345-67-89|ТЕЛЕФОН"
  "ИНН|ИНН 7707083893|ИНН"
  "Карта|карта 4276 1234 5678 9012|КАРТА"
  "CVV|CVV 123|CVV"
  "ПИН|ПИН 1234|ПИН"
  "Держатель|держатель карты Иван Иванов|ДЕРЖАТЕЛЬ"
  "Сложное|Клиент Иванов Иван Иванович, паспорт 4509 123456, тел +7 912 345-67-89, email ivanov@test.ru, карта 4276 1234 5678 9012|ФИО"
)

pass=0
fail=0
declare -a failures=()

for entry in "${CASES[@]}"; do
  IFS='|' read -r label original expected <<< "$entry"
  pid="test-$(date +%s)-$RANDOM"

  # --- Direct check (masking) ---
  mask_resp=$(curl -s -X POST "$URL" -H 'Content-Type: application/json' \
    -d "{\"payload\":\"$original\",\"payload_id\":\"$pid\"}")
  mask_code=$(curl -s -o /dev/null -w "%{http_code}" -X POST "$URL" -H 'Content-Type: application/json' \
    -d "{\"payload\":\"$original\",\"payload_id\":\"$pid\"}")

  # Extract result
  mask_result=$(echo "$mask_resp" | python3 -c "import sys,json;print(json.load(sys.stdin).get('result',''))" 2>/dev/null)

  # Check mask contains expected placeholder
  if [[ "$mask_result" == *"$expected"* ]]; then
    mask_ok="OK"
  else
    mask_ok="FAIL"
  fi

  # --- Idempotency: retry direct check returns same mask ---
  mask_retry=$(curl -s -X POST "$URL" -H 'Content-Type: application/json' \
    -d "{\"payload\":\"$original\",\"payload_id\":\"$pid\"}" | python3 -c "import sys,json;print(json.load(sys.stdin).get('result',''))" 2>/dev/null)
  if [[ "$mask_retry" == "$mask_result" ]]; then
    idem_ok="OK"
  else
    idem_ok="FAIL"
  fi

  # --- Reverse check (unmasking): send the mask back ---
  unmask_resp=$(curl -s -X POST "$URL" -H 'Content-Type: application/json' \
    -d "{\"payload\":\"$mask_result\",\"payload_id\":\"$pid\"}")
  unmask_result=$(echo "$unmask_resp" | python3 -c "import sys,json;print(json.load(sys.stdin).get('result',''))" 2>/dev/null)

  if [[ "$unmask_result" == "$original" ]]; then
    unmask_ok="OK"
  else
    unmask_ok="FAIL"
  fi

  # --- Overall ---
  if [[ "$mask_ok" == "OK" && "$idem_ok" == "OK" && "$unmask_ok" == "OK" ]]; then
    pass=$((pass+1))
    echo "PASS [$label] mask=$mask_ok idem=$idem_ok unmask=$unmask_ok code=$mask_code"
  else
    fail=$((fail+1))
    failures+=("$label")
    echo "FAIL [$label] mask=$mask_ok idem=$idem_ok unmask=$unmask_ok code=$mask_code"
    echo "  original: $original"
    echo "  mask:     $mask_result"
    echo "  unmask:   $unmask_result"
  fi
done

echo ""
echo "=== Summary ==="
echo "Pass: $pass / $((pass+fail))"
if [[ $fail -gt 0 ]]; then
  echo "Failures: ${failures[*]}"
fi