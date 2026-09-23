# Горизонтальное масштабирование PII Gateway на Docker Swarm

Дата: 2026-09-23
Статус: Approved

## Цель

Сделать PII Gateway устойчивым к нагрузке 100k пользователей: горизонтальное
масштабирование нескольких stateless-инстансов за балансировщиком Docker Swarm,
автоскейлинг по метрикам Prometheus, внутренние очереди задач (worker pool) для
контроля concurrency и сглаживания пиков, деградация при недоступности
компонентов. Синхронный контракт `POST /process` сохраняется.

## Требования (собраны в ходе брейншторма)

1. Несколько инстансов за балансировщиком, автоскейлинг.
2. Синхронный контракт `/process` сохраняется.
3. Redis — общее хранилище соответствия payload_id → original.
4. Внутренние очереди задач (включая демаскирование).
5. Локальный fallback-кэш при недоступности Redis (маскирование живёт, unmask деградирует).
6. Платформа автоскейлинга — Docker Swarm mode.

## Целевая архитектура

```
                    ┌──────────────────────────────┐
                    │   Docker Swarm (overlay net)  │
                    │                              │
  Clients ──► Ingress LB (routing mesh)            │
                    │        │                     │
                    ▼        ▼                     │
              ┌─────────┐ ┌─────────┐ ┌─────────┐  │
              │ pii-1   │ │ pii-2   │ │ pii-N   │  │  replicas (docker service scale)
              │ stateless│ │ stateless│ │ stateless│ │
              └────┬────┘ └────┬────┘ └────┬────┘  │
                   │           │           │       │
                   ▼           ▼           ▼       │
              ┌─────────────────────────────────┐  │
              │  Redis (общее хранилище)         │  │
              │  + локальный fallback-кэш        │  │
              └─────────────────────────────────┘  │
                   │                               │
                   ▼                               │
              ┌─────────────────────────────────┐  │
              │  Postgres (конфиг систем/правил) │  │
              └─────────────────────────────────┘  │
                    │                              │
                    ▼                              │
              ┌─────────────────────────────────┐  │
              │  Prometheus + autoscaler         │  │
              │  (скрипт → docker service scale) │  │
              └─────────────────────────────────┘  │
                    └──────────────────────────────┘
```

Принципы:
- Инстансы stateless: конфигурация из Postgres, соответствия из Redis.
- Ingress LB Swarm распределяет запросы между репликами.
- Автоскейлинг — внешний autoscaler-контейнер читает Prometheus и вызывает
  `docker service scale`.
- Синхронный `/process` сохраняется.

## Компонент 1: Внутренняя очередь задач (worker pool)

Каждый инстанс имеет пул воркеров для контроля concurrency и сглаживания пиков.

```
HTTP /process
     │
     ▼
┌─────────────────────────────┐
│  Dispatcher                 │
│  - выбирает очередь по типу │
│  - ставит задачу            │
│  - ждёт результат (future)  │
└──────────────┬──────────────┘
               │
     ┌─────────┴─────────┐
     ▼                   ▼
┌────────────┐    ┌────────────┐
│ Fast queue │    │ Heavy queue│
│ (короткие  │    │ (длинные   │
│  payload)  │    │  100k)     │
└─────┬──────┘    └─────┬──────┘
      │                 │
      ▼                 ▼
┌─────────────────────────────┐
│  Worker pool (N goroutines) │
│  - детекция                 │
│  - маскирование             │
│  - демаскирование           │
└─────────────────────────────┘
```

Решения:
- Две очереди: fast (короткие payload, низкая latency) и heavy (длинные 100k,
  параллельная детекция). Короткие не ждут длинные.
- Future/promise: каждый запрос получает канал результата, воркер пишет в него,
  HTTP-хендлер ждёт. Синхронный контракт сохраняется.
- Ограничение concurrency: пул воркеров ограничивает число одновременных
  детекций — защита от перегрузки CPU.
- Backpressure: переполнение очередей → 503 с Retry-After (не бесконечное
  накопление, защита от OOM).
- Демаскирование — тоже задача в пуле (Redis lookup + fallback-кэш).

### Интерфейс пула

```go
package queue

type Task struct {
    Payload string
    // результат
    done chan Result
}

type Result struct {
    Masked string
    Types  []string
    Tokens map[string]string
    Err    error
}

type Pool struct {
    fast  chan Task
    heavy chan Task
    // ...
}

func New(workers, fastCap, heavyCap int) *Pool
func (p *Pool) Submit(ctx context.Context, payload string, heavy bool) (Result, error)
func (p *Pool) Close()
```

## Компонент 2: Хранилище соответствия (Redis + локальный fallback)

```
Save(payload_id, entry)
     │
     ▼
┌─────────────────────────────┐
│  LayeredStore               │
│                             │
│  1. Redis (общий)           │  ← источник истины
│  2. Локальный in-memory     │  ← fallback + быстрый путь
│     LRU (TTL)               │
└─────────────────────────────┘
```

Решения:
- Write-through: при маскировании пишем и в Redis, и в локальный кэш. Локальный
  кэш — быстрый путь для повторных unmask на том же инстансе.
- Read: сначала локальный кэш, при промахе — Redis, заполняем локальный кэш.
- Fallback при недоступности Redis:
  - Маскирование работает всегда (не требует Redis), пишем только в локальный кэш.
  - Unmask: если Redis недоступен и нет локального попадания → 503.
- Circuit breaker на Redis: после N ошибок подряд перестаём ходить в Redis на
  время (например 5s). При восстановлении — снова включаем.
- TTL единый для Redis и локального кэша (из конфига, 24h).

### Интерфейс

```go
package store

// LayeredStore wraps a Redis store with a local in-memory cache and a circuit
// breaker. It implements store.Store.
type LayeredStore struct {
    redis  *RedisStore
    local  *MemoryStore
    cb     *circuit.Breaker
}

func NewLayered(redis *RedisStore, local *MemoryStore, cb *circuit.Breaker) *LayeredStore
```

## Компонент 3: Автоскейлинг (Swarm + Prometheus + autoscaler)

```
┌─────────────────────────────────────────────┐
│  Prometheus                                 │
│  - pii_latency_seconds (histogram)          │
│  - pii_requests_total (counter)             │
│  - pii_queue_depth (gauge, новая)           │
│  - container CPU (cAdvisor/node-exporter)   │
└──────────────────────┬──────────────────────┘
                       │ scrape
                       ▼
┌─────────────────────────────────────────────┐
│  Autoscaler (отдельный контейнер)           │
│  - каждые N сек читает метрики              │
│  - решает: scale up / down / noop           │
│  - вызывает: docker service scale pii=N     │
└─────────────────────────────────────────────┘
```

Метрики для решения:
- CPU инстансов (основной сигнал) — если > 70% в среднем → scale up.
- Глубина очереди (`pii_queue_depth`) — если растёт → scale up (опережающий сигнал).
- Latency p99 — если растёт → scale up.

Правила автоскейлинга:
- Scale up: CPU > 70% ИЛИ queue_depth > порога ИЛИ p99 latency > 100ms.
- Scale down: CPU < 30% в течение 5 минут.
- Min/Max реплик: min=2, max=20 (из конфига).
- Cooldown: не скейлить чаще, чем раз в 30s (защита от флапа).

Новые метрики в коде:
- `pii_queue_depth` (gauge) — текущая глубина очередей.
- `pii_workers_busy` (gauge) — занятые воркеры.
- `pii_redis_up` (gauge) — состояние circuit breaker (0/1).

Autoscaler — отдельный лёгкий контейнер в стеке, с доступом к docker socket
(для `docker service scale`).

## Компонент 4: Обработка ошибок и деградация

Матрица деградации:

| Компонент | Маскирование | Демаскирование | Ответ |
|-----------|-------------|----------------|-------|
| Redis OK | ✅ работает | ✅ работает | 200 |
| Redis down | ✅ работает (локальный кэш) | ⚠️ только локальный кэш, иначе 503 | 200 / 503 |
| Postgres down | ✅ работает (конфиг закэширован) | ✅ работает | 200 |
| Очередь переполнена | ⚠️ 503 + Retry-After | ⚠️ 503 + Retry-After | 503 |

Решения:
- Конфиг кэшируется в памяти при старте (Manager уже это делает). При
  недоступности Postgres — работаем на закэшированном конфиге.
- Backpressure: переполнение очередей → 503 с Retry-After.
- Circuit breaker на Redis и Postgres.
- Понятные ошибки: 400, 401, 403, 404, 429, 503.
- Graceful shutdown: дождаться завершения текущих задач в пуле, потом закрыть
  соединения.

## Компонент 5: Конфигурация

Новые секции в `config.yaml`:

```yaml
queue:
  workers: 16          # число воркеров в пуле
  fast_capacity: 1024  # ёмкость fast-очереди
  heavy_capacity: 256  # ёмкость heavy-очереди
  heavy_threshold: 4096 # payload длиннее → heavy-очередь

store:
  type: layered        # "memory" | "redis" | "layered"
  circuit:
    failures: 5        # ошибок подряд до размыкания
    cooldown_seconds: 5

autoscale:
  min_replicas: 2
  max_replicas: 20
  cpu_up: 70
  cpu_down: 30
  queue_up: 100
  latency_p99_ms: 100
  cooldown_seconds: 30
  poll_seconds: 10
```

## Компонент 6: Структура изменений

```
internal/
├── queue/                  # НОВОЕ: worker pool + очереди
│   ├── pool.go             #   пул воркеров, future/promise
│   ├── dispatcher.go       #   выбор очереди (fast/heavy)
│   └── pool_test.go
├── store/
│   ├── layered.go          # НОВОЕ: Redis + локальный кэш + circuit breaker
│   ├── circuit.go          #   circuit breaker
│   └── layered_test.go
├── observability/
│   └── metrics.go          # + pii_queue_depth, pii_workers_busy, pii_redis_up
├── api/handlers/
│   └── process.go          # использовать пул вместо прямого вызова
├── config/
│   └── config.go           # + секции queue, autoscale, store.circuit
deploy/
├── stack.yml               # НОВОЕ: Swarm stack (pii, redis, postgres, prometheus, autoscaler)
├── autoscaler/
│   ├── Dockerfile          # НОВОЕ
│   └── main.go             # НОВОЕ: скрипт автоскейлинга
└── prometheus.yml          # + cAdvisor/node-exporter scrape
```

## Тестирование

- Unit: пул воркеров (очередь, backpressure, graceful shutdown), layered store
  (fallback, circuit breaker), dispatcher (fast/heavy).
- Integration: `/process` через пул, деградация при выключенном Redis
  (маскирование работает, unmask 503).
- Load/bench: `cmd/bench` — проверить, что пул не теряет RPS, latency под нагрузкой.
- Swarm e2e: `docker stack deploy`, автоскейлинг реагирует на нагрузку.

## Порядок реализации

1. V1: worker pool + очереди (fast/heavy) + future/promise.
2. V2: layered store (Redis + локальный кэш + circuit breaker).
3. V3: новые метрики + autoscaler контейнер + Swarm stack.
4. V4: тесты деградации + e2e на Swarm.