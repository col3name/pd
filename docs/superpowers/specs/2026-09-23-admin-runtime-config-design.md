# Admin UI + Runtime Config for PII Gateway — Design

Date: 2026-09-23
Status: Approved by user (sections 1–4, with JSON config format)

## Goal

Набрать баллы по критериям 3.4 (гибкая настройка и расширяемость), 3.6 (наблюдаемость)
и 3.7 (доп. сценарии) через:

- live-настройка систем-потребителей и типов ПДН без перезапуска процесса;
- расширение списка ПДН через YAML/JSON-regex (overlay поверх ядра);
- настраиваемые правила комбинаций (PIN → КАРТА);
- React-админка (Vite + TanStack Query) поверх конфиг-API.

## Вводные решения (согласованы с пользователем)

1. Применение конфига — **hot-reload через API** (без перезапуска).
2. Админка — **отдельный сервис** (React SPA) с CORS; dev — Vite dev server,
   prod — Dockerfile с nginx.
3. Запись конфига — **только через JSON** (`PUT /v1/config`), YAML — только при старте.
4. Защита записи — `X-Admin-Key` header (секция `admin:` конфига); чтение и метрики открыты.
5. Расширение списка ПДН — **overlay-правила из конфига** поверх встроенного ядра
   (встроенные правила нельзя переопределить, новые — можно добавить).
6. Новые типы из конфига получают placeholder автоматически (`[<TYPE>]`), без правки ядра.

## Архитектура

```
React SPA (web/, Vite/nginx)
      │  GET/PUT /v1/config (JSON), GET /v1/config/rules, GET /v1/logs, CORS
      ▼
Go server (:8080, chi)
  ├─ /process                       → Handler → control.Manager.Pipeline(system)
  ├─ /v1/config  GET/PUT            → control.Manager (hot-reload, admin_key)
  ├─ /v1/config/rules               → список известных типов ПДН
  ├─ /v1/logs                       → последние N строк process-логов
  ├─ /health, /metrics, /docs, /openapi.yaml
```

### Новый пакет `internal/control`

```go
type Manager struct {
    mu       sync.RWMutex
    cfg      *config.Config
    base     *pipeline.Pipeline
    systems  map[string]*pipeline.Pipeline
    detector *detector.Detector
    ctx      *context.Resolver
    whitelist *whitelist.Whitelist
    rev      uint64
}
```

- `NewManager(path string) (*Manager, error)` — стартовая загрузка (любая логика из
  текущего `main.go` переезжает сюда, включая инициализацию NER-модели).
- `Get() ConfigView` — читаемая форма конфига для UI (без паролей).
- `Apply(cfg *config.Config) (rev uint64, err error)` — пересборка детектора, контекста,
  whitelist, базового и всех per-system pipeline, атомарный swap под lock, `rev++`.
  При ошибке — изменения не применяются.
- `Pipeline(system string) *pipeline.Pipeline` — базовый или системный pipeline
  (переиспользует логику текущего `systemPipeline` из handler).
- `RawYAML() []byte` — текущий YAML для отображения в UI (опционально).

- `SystemConfig(name)` — поиск системы по имени (для authorize/404/403, переиспользует текущую логику).
- `Limiter() *ratelimit.Limiter` — пересоздаётся при Apply, если изменился `rate_limit.rps`;
  `nil` при rps==0.
- NER-модель (если `ml.enabled`) загружается один раз при старте и переживает reload — на
  каждый Apply она не перечитывается с диска.

`Handler` в `internal/api/handlers` переходит с `*pipeline.Pipeline` / `*config.Config` /
`Limiter` на `*control.Manager`:
каждый `/process` вызывает `h.Mgr.Pipeline(req.System)`, auth — `h.Mgr.SystemConfig(name)`,
лимитер — `h.Mgr.Limiter()`. Store остаётся внешним, не перезагружается.

## Эндпоинты

### `GET /v1/config`
```json
{
  "rev": 3,
  "masking": { "mode": "redact" },
  "systems": [
    {
      "name": "chat",
      "api_key_set": true,
      "enabled": true,
      "masking": "token",
      "allow_unmask": true,
      "pii": ["ФИО", "ПАСПОРТ"]
    }
  ],
  "rules": [
    { "type": "СНИЛС", "regex": "...", "priority": 0, "context": "", "capture": "", "keyword": "снилс", "confidence": 0.99 }
  ],
  "combinations": [
    { "type": "ПИН", "requires": ["КАРТА"], "window": 80 }
  ],
  "known_types": ["ФИО", "ДАТА", "..."]
}
```

### `PUT /v1/config`
- body: тот же JSON-объект (праваться может как частями, так и целиком);
- header `X-Admin-Key` — обязателен;
- сервер парсит в `config.Config`, валидирует (compiled regex, названия систем, режимы),
  применяет, возвращает `{"rev": N}` или `400 {"error": "..."}`.

Существующие поля `config.Config` и вложенных структур получают JSON-теги (рядом с
существующими yaml-тегами); `ConfigView` — отдельная структура для `GET` (без секретов).

### Round-trip api_key (важно)
`GET /v1/config` НЕ возвращает значения ключей — только флаг `api_key_set`. Чтобы UI мог
сохранять системы, не перезатирая ключ, `SystemConfig.APIKey` в JSON объявляется как
`*string`:
- `null` (поле отсутствует) → ключ не меняется;
- `""` → ключ удаляется;
- непустая строка → ключ заменяется.

### `GET /v1/config/rules`
Список всех известных типов ПДН (константы ядра + types, добавленные правилами).

### `GET /v1/logs?n=50`
Последние N строк лога процесса (`pii-*` level INFO-записи: mask/unmask, types, latency_ms)
— без значений ПД (критерий 3.6: «показать лог одного запроса»).

## Overlay-правила (расширение списка ПДН)

Секция конфига:
```yaml
rules:
  - type: "СНИЛС"
    regex: "(?i)\\b\\d{3}-\\d{3}-\\d{3}\\s\\d{2}\\b"
    priority: 0
    context: ""
    capture: ""
    keyword: "снилс"
    confidence: 0.99
```

- `internal/detector`: `ExtraRules([]Rule) Option` — append к `StructuredRules()` (overlay),
  `RuleFromConfig(...)` мапит YAML/JSON в `detector.Rule`.
- `Placeholder()`/`Valid()` в `types.go` получают fallback для неизвестных типов:
  `Placeholder() -> "[" + type + "]"` если нет записи.
- Валидация `regex` при Apply: невалидный → 400, swap не происходит.

## Комбинации (критерий 3.7)

```yaml
combinations:
  - type: "ПИН"
    requires: ["КАРТА"]
    window: 80
```

- `internal/pipeline`: `Options.Combinations []Combination`; пост-фильтр после
  `escalateDates`: если тип спана в комбинации, но требуемых типов нет в window — span
  дропается. Существующая логика `Sensitive` (lone sensitive) сохраняется как fallback.
- В дефолтном конфиге секция отключена по умолчанию (пустая); пример — в README.

## Доп. документы (критерий 3.7)

- Уже есть: ВУ/загран/военный билет/свидетельство о рождении.
- Добавить тип `УДОСТОВЕРЕНИЕ` (удостоверение личности) **как overlay-правило в дефолтном
  конфиге** — это демонстрирует «расширение без переписывания ядра» прямо из YAML (новые
  типы не требуют констант в `types.go`, placeholder формируется из имени типа).
- README: инструкция «добавить новый тип ПДН через админку за 30 секунд».

## CORS

Middleware на `/v1/*` и `/process`: `Access-Control-Allow-Origin` из env
`ADMIN_ORIGIN` (default `*`), методы `GET, POST, PUT, OPTIONS`, headers
`Content-Type, X-Admin-Key`. В dev Vite-прокси шлёт на `localhost:8080`.

## React SPA (`web/`) на TelegramUI

- Stack: React + TypeScript + Vite + `@tanstack/react-query` + **`@telegram-apps/telegram-ui`**
  (UI-кит в стиле Telegram: `AppRoot`, `Cell`/`List`, `Switch`, `Input`, `Select`,
  `Button`, `Placeholder`, `Section`...) + его `styles.css`.
- `vite.config.ts`: dev-прокси `/v1`, `/process` → `http://localhost:8080`.
- Прод: `web/Dockerfile` (nginx:alpine; статика + `proxy_pass` на Go-сервис).
- Компоненты:
  - `api.ts` — типы и fetch-обёртки (GET config, PUT config, logs).
  - `Admin.tsx` — корневой `AppRoot`, табы: Systems, Rules, Combinations, Config JSON, Logs.
  - Systems: `Cell`-список систем, `Switch` для enabled, `Select` masking,
    `Checkbox` для типов ПДН, кнопка «+ система».
  - Rules: CRUD overlay-правил (`Input` для regex/type/context/keyword).
  - Combinations: CRUD комбинаций.
  - Config JSON: raw-JSON editor + Apply (PUT), показывает `rev`.
  - Logs: `Cell`-список логов, refetch 2с.
  - Admin-key: `Input` в шапке → header `X-Admin-Key` на PUT.
- Query-кэш: `useQuery(['config'])`; после успеха mutation → `invalidateQueries(['config'])`.

## Тестирование

- Go: unit-тесты `control.Manager` (Apply валидный/невалидный, swap pipeline,
  rev-инкремент), overlay-правил (новый тип маскируется), комбинаций (PIN без карты
  не маскируется; PIN+КАРТА — маскируется), handler API-key на PUT.
- Существующие тесты `process_test.go`, `detector/*`, `pipeline/*` остаются зелёными
  (overlay не ломает ядро).
- Frontend: TypeScript-check при сборке (`tsc --noEmit`), ручная проверка флоу
  через Vite dev + Go. Полноценного frontend-теста не закладываем (YAGNI для хакатона).

## Docker / compose

- `web/Dockerfile` — nginx: статика + proxy `/v1`, `/process`, `/health`, `/metrics`.
- `docker-compose.yml` — добавить сервис `admin` (build `./web`), порт 5173 в dev
  или отдельный prod-порт.

## README

Добавить разделы:
- «Админка»: как поднять (npm i; npm run dev), что умеет.
- «Настройка live (hot-reload)»: пример `PUT /v1/config` со сменой систем.
- «Расширение типов ПДН»: добавление нового типа через rules-секцию.
- «Комбинации»: PIN → КАРТА, пример конфига и ожидаемого маскирования.