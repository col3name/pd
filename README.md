# PII Gateway v2 (version2)

Гибридный шлюз маскирования персональных данных (ПД): быстрые детерминированные правила +
контекстная оценка уверенности + опциональная NER-модель + span resolver + обратимый
токенизированный режим. Заменяет v1 : итог — **та же точность маскирования, но без LLM в
пайплайне** и с исправлением scorer-критики («12.03.1998 рядом с ФИО»).

Проектировался под high-load: RPS 1000+ при latency < 100 ms, round-trip `POST /process`
без потерь (демаскирование возвращает исходную строку байт-в-байт).

## Архитектура

```
Система-потребитель
     │  POST /process {payload, payload_id}
     ▼
┌──────────────────────────────────────────────────────────────┐
│                      pii-module-v2                           │
│                                                              │
│  ┌────────────┐  ┌──────────────┐  ┌────────────┐  ┌──────┐  │
│  │  Detector  │→ │ дата-эскалация│→│  Resolver  │→ │  …  │  │
│  │ (rules+)   │  │ ДАТА→ДАТА_…   │  │  (span-    │  │      │  │
│  └────────────┘  └──────────────┘  │  резолютор)│  └──────┘  │
│         │                          └────────────┘           │
│         ▼                          ┌───────────────────────┐ │
│  Whitelist → Context → Confidence  │  Mask / Tokenize →    │ │
│  (не-ПД)     (boost/penalty)       │  Store (memory|redis) │ │
│                                    └───────────────────────┘ │
│  HTTP /process  /health  /metrics                            │
└──────────────────────────────────────────────────────────────┘
```

**Пайплайн (этапы, `internal/pipeline/pipeline.go`):**

1. **Detect** — `internal/detector`: regex-правила + capture-правила с ключевыми словами,
   опционально NER (только для «сомнительных» участков). Для каждого span — `Confidence`.
2. **Эскалация дат** — обычная `ДАТА` (низкая уверенность, 0.7) повышается до
   `ДАТА_РОЖДЕНИЯ` с confidence 1.0, если находится в пределах ~80 байт от «якоря» ПД
   (ФИО, место рождения, адрес, паспорт). Это и есть исправление scorer-критики:
   `указал: Иванов Иван Иванович, 12.03.1998` → `[ДАТА]`, при этом
   `Банк открылся 12 марта 1998 года` не маскируется.
3. **Resolve** — `internal/resolve`: снятие пересечений по приоритету типа
   (КАРТА > ПАСПОРТ > … > ИНН), длине и позиции.
4. **Whitelist** — `internal/whitelist`: известные не-ПД сущности (Пушкин, Толстой,
   «отделение Альфа-Банка») блокируют маскирование независимо от детектора.
5. **Context** — `internal/context`: ключевые слова-усилители (`клиент`, `заёмщик`,
   `адрес регистрации`, …) поднимают confidence, слова-штрафы (`банк`, `филиал`,
   `поэт`, …) опускают. ±100 байт вокруг span.
6. **Gate** — порог 0.95: ниже — не маскируем. Одиночный «чувствительный» тип
   (ПИН/CVV из `sensitive`) маскируется только при наличии другой ПД рядом.
7. **Mask / Tokenize** — `internal/masker`: redact-режим → v1-плейсхолдеры `[КЛАСС]`,
   token-режим → `[LABEL_NNN]` с обратимой картой `token → значение`.

## Быстрый старт

```bash
# Локально — без Redis (in-memory store по умолчанию)
cd version2
go run ./cmd/server

# Docker (демон) — memory store, Redis не требуется
docker compose up -d --build
```

Проверка:
```bash
curl -X POST http://localhost:8080/process \
  -H "Content-Type: application/json" \
  -d '{"payload":"паспорт 4509 123456, email test@example.com","payload_id":"test-1"}'
# → {"result":"паспорт [ПАСПОРТ], email [EMAIL]"}

# Демаскирование — тот же payload_id + наша маска возвращает оригинал байт-в-байт
curl -X POST http://localhost:8080/process \
  -H "Content-Type: application/json" \
  -d '{"payload":"паспорт [ПАСПОРТ], email [EMAIL]","payload_id":"test-1"}'
# → {"result":"паспорт 4509 123456, email test@example.com"}

# Идемпотентность: повтор прямого шага (та же исходная строка) возвращает ту же маску
curl -X POST http://localhost:8080/process \
  -H "Content-Type: application/json" \
  -d '{"payload":"паспорт 4509 123456, email test@example.com","payload_id":"test-1"}'
# → {"result":"паспорт [ПАСПОРТ], email [EMAIL]"}
```

Готовый сценарий демо для жюри — `scripts/curl_demo.sh` (health, маскирование,
демаскирование, системы с API-ключами, 401/403/404, ловушки, метрики):
```bash
./scripts/curl_demo.sh            # использует публичный URL по умолчанию
./scripts/curl_demo.sh http://localhost:8080
```

## Контракт API

### `POST /process`

Запрос:
```json
{ "payload": "строка", "payload_id": "идентификатор", "system": "chat" }
```

Ответ: `200` `{ "result": "строка" }`.

Логика:
- **Новый `payload_id`** → маскирование: `result` — это `Pipeline.Result.Masked`
  (redact: `[КЛАСС]`, token: `[LABEL_NNN]`). Оригинал, маска и token-карта сохраняются в store.
- **Существующий `payload_id`** (и `allow_unmask: true` для системы, см. «Системы-потребители»)
  → корреляция по содержимому payload, эндпоинт идемпотентен (важно для ретраев нагрузочного теста):
  - payload == сохранённый **оригинал** → это повтор прямого шага (маскирования): возвращается
    **сохранённая маска** — ретраи не портят метрику маскирования.
  - payload == сохранённая **маска** → это обратный шаг (демаскирования): возвращается
    `store.Entry.Original`, т.е. исходная строка **байт-в-байт**, включая не-ПД подстроки.
- Ответ всегда `{"result": "..."}`.

Коды: `200` — успех, `400` — нет `payload_id`/битый JSON и битый `payload`, `401` —
неверный API-ключ системы, `403` — система отключена, `404` — неизвестная система,
`429` — rate limit (`Retry-After`), `500` — внутренняя ошибка.

### `GET /health`
Возвращает `ok`.

### `GET /metrics`
Prometheus-метрики (см. «Метрики»).

### `GET /docs` и `GET /openapi.yaml`
- `/docs` — интерактивный Swagger UI: спецификация контракта `/process` с кнопкой
  **Try it out** (можно вбить `payload`/`payload_id` и отправить запрос прямо со страницы).
- `/openapi.yaml` (алиас `/process_api.yaml`) — сам файл спецификации OpenAPI 3, эмбеднут
  в бинарь при сборке (источник — `process_api.yaml` в корне репозитория).
- `/` — редирект на `/docs`.

## Системы-потребители

Поведение модуля настраивается per-system через `config.yaml` — без правки кода.
Запрос указывает систему полем `"system"`; при указании имени без настроенного API-ключа
система авторизуется неявно.

```yaml
systems:
  - name: chat
    api_key: "demo-chat-key"        # непустой => требуется заголовок X-API-Key
    enabled: true                    # false => 403
    pii: [ФИО, ПАСПОРТ, ТЕЛЕФОН]     # пусто = все типы
    masking: token                   # "redact" | "token"; пусто = глобальный
    allow_unmask: true               # разрешено ли демаскирование
  - name: analytics
    enabled: true
    pii: [ТЕЛЕФОН, EMAIL]            # аналитике недоступны ФИО/паспорт/карты
    allow_unmask: false              # аналитике демаскирование запрещено
```

Пример (chat, token-режим, с ключом):
```bash
curl -X POST http://localhost:8080/process -H 'Content-Type: application/json' \
  -H 'X-API-Key: demo-chat-key' \
  -d '{"payload":"Клиент Иванов Иван, тел +7 912 345-67-89","payload_id":"c-1","system":"chat"}'
# {"result":"Клиент [PERSON_001], тел [PHONE_002]"}
```

## Режимы маскирования

| Режим | `masking.mode` | Пример |
|-------|----------------|--------|
| **redact** (по умолчанию) | `redact` | `паспорт [ПАСПОРТ], email [EMAIL]` |
| **token** | `token` | `паспорт [PASSPORT_001], email [EMAIL_002]` |

В token-режиме `[LABEL_NNN]` — семантически нейтральный обратимый токен
(label: `PERSON`, `DATE`, `PHONE`, `EMAIL`, `CARD`, `PASSPORT`, `INN`, `CVV`, `PIN`,
`ADDRESS`, `CARDHOLDER`, иначе — имя типа). Нумерация глобальная по отсортированным
span-ам, поэтому детерминированная: повторный прогон даёт те же токены. LLM можно
передавать текст с токенами, а после ответа восстановить значения через карту
`token → original`, сохранённую в store.

## Конфигурация

Файл: `version2/configs/config.yaml`, флаг сервера `-config` (по умолчанию
`configs/config.yaml`, относительно рабочей директории).

```yaml
port: 8080                  # HTTP-порт
allow_unmask: true          # разрешить демаскирование по существующему payload_id
store:
  type: memory              # "memory" | "redis"
  ttl_hours: 24             # TTL записей
  capacity: 262144          # ёмкость in-memory LRU
  redis_url: "redis://localhost:6379/0"   # только для store.type: redis
masking:
  mode: redact              # "redact" | "token"
context:
  enabled: true             # контекстный confidence-resolver
  boost:                    # ключевые слова-усилители (тип: [слова])
    "ФИО": [клиент, заёмщик, паспорт, держатель, анкета, указал]
    "АДРЕС": [адрес клиента, проживает, зарегистрирован]
  penalty:                  # ключевые слова-штрафы (банк/филиал и т.п.)
    "АДРЕС": [отделение, офис, банк]
    "ФИО": [поэт, писатель, написал]
whitelist:
  enabled: true             # known non-PII, никогда не маскируются
  persons: [Александр Пушкин, Лев Толстой, Антон Чехов]
  addresses: [Красная площадь, отделение Альфа-Банка]
  organizations: [Альфа-Банк]
resolve:
  priority:                 # приоритеты типов для resolve (выше = побеждает)
    "КАРТА": 100
    "ПАСПОРТ": 95
    "ИНН": 40
ml:
  enabled: false            # NER: https://github.com/yalue/onnxruntime_go
  model_path: ""            # rubert-tiny ONNX-модель + vocab + labels
  vocab_path: ""
  labels: [O, B-PER, I-PER, B-LOC, I-LOC, B-ORG, I-ORG]
sensitive:                  # одиночный тип из списка маскируется только
  - ПИН                    # при наличии другой ПД рядом
  - CVV
rate_limit:
  rps: 0                    # 0 — выключено; иначе 429 при превышении
  burst: 0
```

Env-overrides (побеждают YAML): `PORT`, `STORE` (`memory`|`redis`), `MASK_MODE`
(`redact`|`token`). Обратите внимание: для `redis_url` env-переопределения нет
(Task 9): переключение на Redis из контейнера требует смонтированного YAML с
`store.type: redis` и правильным `store.redis_url` (пример ниже).

### Store: memory vs redis

- **memory** (по умолчанию) — in-process bounded LRU с TTL и FIFO-вытеснением
  (`internal/store/memory.go`). Ноль внешних зависимостей, максимум RPS.
- **redis** — `internal/store/redis.go`, запись = JSON `{Original, Tokens}` с TTL.
  Подходит для нескольких реплик шлюза за балансировщиком.

Включение Redis в Docker Compose (требуется профиль и смонтированный конфиг из-за
отсутствия env-override для `redis_url`):

```bash
# configs/config.redis.yaml:
#   store:
#     type: redis
#     ttl_hours: 24
#     redis_url: "redis://redis:6379/0"
docker compose --profile redis up -d --build
# и смонтировать конфиг: -v ./configs/config.redis.yaml:/srv/configs/config.yaml:ro
```

## Live-настройка (hot-reload) через admin API

Конфиг читается из YAML при старте, но менять его можно на лету — без рестарта и
без правки кода. `PUT /v1/config` атомарно пересобирает детектор, контекст, whitelist
и pipeline'ы систем. Запись защищена заголовком `X-Admin-Key` (значение —
`config.Admin.Key` из YAML, по умолчанию `pii-admin-key`).

| Эндпоинт | Метод | Описание |
|----------|-------|----------|
| `/v1/config` | `GET` | Текущий конфиг (`rev`, `masking`, `systems`, `rules`, `combinations`, `known_types`). Ключи API-систем **не возвращаются** — только флаг `api_key_set`. |
| `/v1/config` | `PUT` | Запись конфига, тело — тот же JSON-вид, что у `GET` (плюс опциональный `api_key` на систему). Требует `X-Admin-Key`; ответ `{"rev": N}`. |
| `/v1/config/rules` | `GET` | Список известных типов ПДН. |

CORS для admin-эндпоинтов настраивается env `ADMIN_ORIGIN` (по умолчанию `*`) — чтобы
SPA в `web/` могла читать/писать конфиг из браузера.

Пример: включить комбинацию «ПИН маскируется только рядом с картой»:

```bash
curl -s -X PUT http://localhost:8080/v1/config \
  -H 'X-Admin-Key: pii-admin-key' \
  -H 'Content-Type: application/json' \
  -d '{"combinations":[{"type":"ПИН","requires":["КАРТА"],"window":80}]}'
# → {"rev":1}

# теперь одиночный ПИН не маскируется…
curl -s -X POST http://localhost:8080/process \
  -d '{"payload":"пин 1234","payload_id":"cdemo1"}'
# → {"result":"пин 1234"}

# …а рядом с картой — маскируются оба типа
curl -s -X POST http://localhost:8080/process \
  -d '{"payload":"карта 4276 1234 5678 9012, пин 1234","payload_id":"cdemo2"}'
# → {"result":"карта [КАРТА], пин [ПИН]"}
```

Тот же механизм покрывает overlay-правила: новые типы ПДН (например, СНИЛС из
`configs/config.yaml`) добавляются в `rules` без переписывания ядра. Готовая админка
(SPA) — `web/` (Vite dev :5173 / nginx prod), поверх описанных эндпоинтов.

## NER (smart path, опционально)

`ml.enabled: true` + пути к ONNX-модели и vocab. NER подключается к детектору и
используется для неоднозначных случаев; при недоступности модели сервер стартует
без неё (`slog.Warn`, graceful degradation). Всё остальное — rule-based и fast path.
Для установки модели см. `scripts/setup_ner.sh` из v1.

## Метрики (Prometheus)

| Метрика | Тип | Описание |
|---------|-----|----------|
| `pii_requests_total{type,status}` | Counter | Запросы по направлению (mask/unmask) и статусу |
| `pii_latency_seconds{type}` | Histogram | Latency запросов |
| `pii_detected_total{type}` | Counter | Обнаружено ПД по типам |

Логи — структурированные (slog), без значений ПД: `payload_id`, `types`, `latency_ms`.

## Производительность

```bash
cd version2
go run ./cmd/bench -c 100 -d 10s              # короткий payload (по умолчанию)
go run ./cmd/bench -c 20 -d 6s -size 100000   # длинный текст до 100k символов
```

Bench отправляет реалистичный payload `паспорт 4509 123456, email …, телефон +7 …`
с уникальным `payload_id` на каждый запрос (всегда путь маскирования). Флаг `-size N`
повторяет ПД-фрагмент до ~N символов (UTF-8), покрывая порог 4096 байт, после
которого детектор переключается на параллельную по правилам обработку. Клиент
переиспользует keep-alive-соединения (читает тело ответа) и пул idle-коннектов
масштабируется вместе с `-c`. Примеры на данной машине:

```
$ go run ./cmd/bench -c 100 -d 10s
total=402584 ok=402584 fail=0 rps=40242
p50=3.431042ms p99=26.903375ms

$ go run ./cmd/bench -c 50 -d 10s
total=405704 ok=405704 fail=0 rps=40565
p50=809.625µs p99=6.302083ms

$ go run ./cmd/bench -c 500 -d 10s
total=395036 ok=395036 fail=0 rps=39449
p50=11.582459ms p99=43.612541ms
```

Длинные тексты (одна машина, сервер на ~7.7 CPU):

```
$ go run ./cmd/bench -c 20 -d 6s -size 1024     # p50=3.3ms
total=28143 ok=28143 fail=0 rps=4687

$ go run ./cmd/bench -c 20 -d 6s -size 10240    # 10k символов
total=2978 ok=2978 fail=0 rps=493
p50=36.7ms p99=107ms

$ go run ./cmd/bench -c 50 -d 8s -size 100000   # 100k символов, p50=1.0s
total=379 ok=379 fail=0 rps=44

$ go run ./cmd/bench -c 10 -d 8s -size 100000   # 100k символов, p50=219ms
total=353 ok=353 fail=0 rps=43
```

```
size      c    rps     p50      p99
1k       20   4687    3.3ms    17ms
10k      20    493   36.7ms   107ms
25k      20    203   87 ms   255ms
50k      20     98  188 ms   403ms
100k     10     43  219 ms   372ms
100k     20     45  413 ms   980ms
```

Узкое место на коротких текстах — CPU детекции. На 100k символов сервер
утилизирует все ядра (детект параллелит правила), throughput насыщается на
`rps≈44` независимо от concurrency; при `c=200` начинается перегрузка
(p50=4s, нечитаемый лог) — для long-текстов эффективнее вертикальное масштабирование
или сегментирование, а не больше конкурентности. Повторные прогоны одного сервера
(store заполнен) дают те же цифры без деградации; демаскирование на 100k
возвращает оригинал байт-в-байт.

## Анти-паттерны (адversarial-кейсы)

- `Александр Пушкин написал роман` — **не** маскируется (penalty + whitelist).
- `Ближайшее отделение банка на ул. Тверская, 10` — **не** маскируется (penalty).
- `адрес клиента: г. Москва, ул. Ленина, д. 10` — маскируется (boost «адрес клиента»).
- `Введите пин 1234` — **не** маскируется (одиночный sensitive-тип).
- `пин 1234, карта 4276 1234 5678 9012` — маскируется (co-occurrence).

Accurancy-набор: `tests/pii_cases.json` + `tests/false_positives.json`, гоняется
тестом `TestAccuracyDataset`.

## Тестирование

```bash
go build ./...          # сборка
go vet ./...            # статический анализ
go test ./...           # unit + integration + accuracy
```