# Дизайн: системы-потребители в PostgreSQL + авторизация админки

Дата: 2026-09-23
Статус: согласован (brainstorming)

## Цель

Перенести хранение систем-потребителей (и правил/комбинаций) из YAML в PostgreSQL.
Добавить CRUD для систем с генерацией accessKey (показ один раз, хэш в БД),
переключателями enabled/allow_unmask, и авторизацию админки по логину/паролю
(сессия/токен). UI сокращается до одной вкладки Systems.

## Решения (из уточнений)

- В Postgres хранятся: `systems`, `rules`, `combinations`, `admins`, `sessions`.
- YAML — только seed при первом старте (если таблица пуста).
- accessKey: случайный (32 hex), показывается один раз, в БД — `sha256`.
- Правила/комбинации в БД, без UI, без PUT-эндпоинта. `GET /v1/config` — read-only.
- Авторизация админки: логин/пароль → случайный токен + таблица `sessions`.
- Manager перечитывает системы/правила/комбинации из БД при каждом изменении.
- UI: только вкладка Systems. Вкладки Правила/Комбинации/Конфиг удалены.

## Архитектура

Новый пакет `internal/db` — репозиторий поверх Postgres (pgx/v5). Отвечает за:
миграции, seed из YAML, CRUD систем, чтение правил/комбинаций, auth (admins+sessions).

```
internal/db/
  db.go           // Repo: подключение pgx, миграции, seed
  systems.go      // CRUD систем
  rules.go        // чтение правил
  combinations.go // чтение комбинаций
  auth.go         // admins + sessions (login, logout, validate token)
  seed.go         // seed из YAML при первом старте
internal/api/handlers/
  auth.go         // POST /v1/auth/login, /logout
  systems.go      // CRUD /v1/systems
  config.go       // GET /v1/config (read-only)
  process.go      // авторизация систем через sha256(api_key)
internal/control/
  manager.go      // Reload() вместо Apply(); читает из repo
```

## Таблицы

```sql
systems (
  name          text PRIMARY KEY,
  api_key_hash  text NOT NULL DEFAULT '',
  enabled       boolean NOT NULL DEFAULT true,
  allow_unmask  boolean NOT NULL DEFAULT false,
  masking       text NOT NULL DEFAULT '',      -- "" = глобальный режим
  pii           text[] NOT NULL DEFAULT '{}',  -- пусто = все типы
  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now()
)

rules (
  id          serial PRIMARY KEY,
  type        text NOT NULL,
  regex       text NOT NULL DEFAULT '',
  priority    int  NOT NULL DEFAULT 0,
  context     text NOT NULL DEFAULT '',
  capture     text NOT NULL DEFAULT '',
  keyword     text NOT NULL DEFAULT '',
  confidence  real NOT NULL DEFAULT 0.99
)

combinations (
  id        serial PRIMARY KEY,
  type      text NOT NULL,
  requires  text[] NOT NULL,
  window    int NOT NULL DEFAULT 80
)

admins (
  login         text PRIMARY KEY,
  password_hash text NOT NULL
)

sessions (
  token_hash text PRIMARY KEY,
  login      text NOT NULL REFERENCES admins(login),
  expires_at timestamptz NOT NULL
)
```

## Manager и рантайм

`Manager` получает ссылку на `*db.Repo`. `buildLocked()` читает системы/правила/
комбинации из БД, остальное (контекст, whitelist, detector, sensitive, masking) —
из `config.Config` (YAML).

- `Reload()` — новый метод: перечитывает БД-часть, пересобирает pipeline атомарно
  под write-lock, инкрементирует `rev`. Заменяет `Apply(cfg)`.
- `System(name)` — читает из in-memory кэша Manager (заполняется в `Reload()`)
  для быстрой авторизации в `/process`. Кэш хранит `name → {api_key_hash, enabled}`.
- `Apply(cfg)` удаляется (PUT /v1/config удаляется).

## API

```
POST /v1/auth/login     {login, password} → {token}
POST /v1/auth/logout    (Authorization: Bearer <token>)
GET  /v1/systems        → [{name, api_key_set, enabled, allow_unmask, masking, pii}]
POST /v1/systems        {name, enabled, allow_unmask, masking, pii} → {name, access_key}
GET  /v1/systems/{name} → system view
PUT  /v1/systems/{name} {enabled, allow_unmask, masking, pii} → {rev}
DELETE /v1/systems/{name} → 204
POST /v1/systems/{name}/regenerate-key → {access_key}
GET  /v1/config         (read-only) → {rev, masking, systems, rules, combinations, known_types}
```

Убираются: `PUT /v1/config`, `GET /v1/config/rules`, `X-Admin-Key`.

## Авторизация

- `POST /v1/auth/login` — публичный. Проверяет bcrypt-хэш, создаёт сессию
  (случайный токен, хэш в `sessions`, TTL 24ч).
- Все остальные `/v1/*` — требуют `Authorization: Bearer <token>` (middleware).
- `/process` — `X-API-Key` остаётся; проверка `sha256(X-API-Key) == api_key_hash`.

## Безопасность ключей систем

- При создании/regenerate генерируется случайный ключ (32 hex), возвращается один раз.
- В БД хранится только `sha256(access_key)`.

## UI

- Экран входа (login/password) → сохраняет токен (localStorage), шлёт `Authorization: Bearer`.
- SystemsTab — текущий вид, но через CRUD-эндпоинты. Кнопка «копировать ключ» при создании/regenerate.
- Вкладки Правила/Комбинации/Конфиг удалены.

## Конфиг (config.yaml)

```yaml
database:
  dsn: "postgres://pii:pii@localhost:5432/pii?sslmode=disable"
admin:
  login: "admin"
  password: "admin123"   # seed при первом старте, bcrypt в БД
```

## Зависимости

- `github.com/jackc/pgx/v5`
- `golang.org/x/crypto/bcrypt`

## Docker

В `docker-compose.yml` добавить сервис `postgres` (postgres:16-alpine). Gateway
подключается по DSN. `store.type` остаётся memory/redis для payload.

## Что НЕ меняется

Детекторы, pipeline, resolve, context, whitelist, masker, NER, payload store
(memory/redis), логика маскирования `/process`.

## Тесты

- `internal/db`: unit-тесты CRUD/auth (pgxmock) + опциональный интеграционный с Postgres.
- `internal/api/handlers`: CRUD систем, auth (login/logout/validate), авторизация `/process` через хэш.
- `internal/control`: `Reload()` пересобирает pipeline из БД.