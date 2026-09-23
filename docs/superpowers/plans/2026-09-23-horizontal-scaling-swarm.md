# Горизонтальное масштабирование PII Gateway на Docker Swarm — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Сделать PII Gateway устойчивым к 100k пользователей: worker pool с очередями, layered store (Redis + локальный кэш + circuit breaker), автоскейлинг на Docker Swarm.

**Architecture:** Каждый инстанс stateless. Внутри — пул воркеров с двумя очередями (fast/heavy) для контроля concurrency. Соответствия payload_id→original в Redis (общий) + локальный in-memory кэш с circuit breaker. Автоскейлинг — отдельный контейнер, читающий Prometheus и вызывающий `docker service scale`.

**Tech Stack:** Go 1.25, chi, redis/go-redis, prometheus/client_golang, Docker Swarm.

## Global Constraints

- Go 1.25.0 (go.mod `go 1.25.0`).
- Модуль: `github.com/kind-earthquake/pii-module`.
- Синхронный контракт `POST /process` сохраняется.
- Логи без значений ПД: только `payload_id`, `types`, `latency_ms`, `queue_depth`.
- Все новые пакеты — в `internal/`.
- TDD: тест пишется до реализации.
- Частые коммиты после каждого зелёного теста.

---

### Task 1: Circuit breaker

**Files:**
- Create: `internal/store/circuit.go`
- Test: `internal/store/circuit_test.go`

**Interfaces:**
- Consumes: ничего.
- Produces: `type Breaker struct`; `func NewBreaker(failures int, cooldown time.Duration) *Breaker`; `func (b *Breaker) Allow() bool`; `func (b *Breaker) Success()`; `func (b *Breaker) Failure()`; `func (b *Breaker) State() bool` (true = closed/up).

- [ ] **Step 1: Write the failing test**

```go
package store

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBreakerOpensAfterFailures(t *testing.T) {
	b := NewBreaker(3, time.Minute)
	require.True(t, b.Allow())
	b.Failure()
	b.Failure()
	require.True(t, b.Allow())
	b.Failure()
	require.False(t, b.Allow(), "breaker must open after 3 failures")
	require.False(t, b.State())
}

func TestBreakerRecoversAfterCooldown(t *testing.T) {
	b := NewBreaker(2, 50*time.Millisecond)
	b.Failure()
	b.Failure()
	require.False(t, b.Allow())
	time.Sleep(60 * time.Millisecond)
	require.True(t, b.Allow(), "breaker must close after cooldown")
	b.Success()
	require.True(t, b.State())
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestBreaker -v`
Expected: FAIL — `undefined: NewBreaker`

- [ ] **Step 3: Write minimal implementation**

```go
package store

import (
	"sync"
	"time"
)

// Breaker is a simple circuit breaker. It opens after `failures` consecutive
// failures and stays open for `cooldown`, then allows a probe request.
type Breaker struct {
	mu        sync.Mutex
	failures  int
	threshold int
	cooldown  time.Duration
	openUntil time.Time
}

func NewBreaker(failures int, cooldown time.Duration) *Breaker {
	if failures <= 0 {
		failures = 1
	}
	return &Breaker{threshold: failures, cooldown: cooldown}
}

// Allow reports whether a call to the guarded resource is permitted now.
func (b *Breaker) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.failures >= b.threshold && time.Now().Before(b.openUntil) {
		return false
	}
	return true
}

// Success resets the failure count.
func (b *Breaker) Success() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failures = 0
}

// Failure increments the failure count and opens the breaker when the
// threshold is reached.
func (b *Breaker) Failure() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failures++
	if b.failures >= b.threshold {
		b.openUntil = time.Now().Add(b.cooldown)
	}
}

// State reports whether the breaker is closed (up).
func (b *Breaker) State() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.failures < b.threshold || time.Now().After(b.openUntil)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ -run TestBreaker -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/store/circuit.go internal/store/circuit_test.go
git commit -m "feat: circuit breaker for store"
```

---

### Task 2: Layered store (Redis + local cache + breaker)

**Files:**
- Create: `internal/store/layered.go`
- Test: `internal/store/layered_test.go`

**Interfaces:**
- Consumes: `store.Store` (from `internal/store/store.go`), `store.RedisStore` (from `internal/store/redis.go`), `store.MemoryStore` (from `internal/store/memory.go`), `store.Breaker` (Task 1).
- Produces: `type LayeredStore struct`; `func NewLayered(redis *RedisStore, local *MemoryStore, breaker *Breaker) *LayeredStore`; implements `store.Store` (`Save(ctx, id, Entry) error`, `Get(ctx, id) (Entry, bool, error)`); `func (s *LayeredStore) RedisUp() bool`.

- [ ] **Step 1: Write the failing test**

```go
package store

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func newLayered(t *testing.T) (*LayeredStore, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	redisStore := NewRedis(client, time.Hour)
	local := NewMemory(time.Hour, 1000)
	breaker := NewBreaker(3, time.Minute)
	return NewLayered(redisStore, local, breaker), mr
}

func TestLayeredSaveGet(t *testing.T) {
	s, _ := newLayered(t)
	ctx := context.Background()
	require.NoError(t, s.Save(ctx, "id-1", Entry{Original: "orig"}))
	got, ok, err := s.Get(ctx, "id-1")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "orig", got.Original)
}

func TestLayeredRedisDownMaskStillWorks(t *testing.T) {
	s, mr := newLayered(t)
	ctx := context.Background()
	// Save while Redis is up.
	require.NoError(t, s.Save(ctx, "id-2", Entry{Original: "orig2"}))
	// Kill Redis.
	mr.Close()
	// Mask path: Save must not error (falls back to local cache).
	require.NoError(t, s.Save(ctx, "id-3", Entry{Original: "orig3"}))
	// Unmask path: local cache hit still works.
	got, ok, err := s.Get(ctx, "id-2")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "orig2", got.Original)
	require.False(t, s.RedisUp())
}

func TestLayeredRedisDownNoLocalMiss(t *testing.T) {
	s, mr := newLayered(t)
	ctx := context.Background()
	// Save to Redis only (bypass local by using a fresh layered store sharing
	// the same Redis but empty local).
	redisStore := NewRedis(redis.NewClient(&redis.Options{Addr: mr.Addr()}), time.Hour)
	require.NoError(t, redisStore.Save(ctx, "id-4", Entry{Original: "remote"}))
	mr.Close()
	// Local cache empty + Redis down -> miss, no error.
	_, ok, err := s.Get(ctx, "id-4")
	require.NoError(t, err)
	require.False(t, ok)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestLayered -v`
Expected: FAIL — `undefined: NewLayered`

- [ ] **Step 3: Write minimal implementation**

```go
package store

import (
	"context"
	"log/slog"
)

// LayeredStore persists entries to Redis (source of truth) and a local
// in-memory cache (fast path + fallback). A circuit breaker guards Redis: when
// it is open, Save writes only to the local cache and Get serves only local
// hits. Masking never fails; unmask degrades to local-only.
type LayeredStore struct {
	redis   *RedisStore
	local   *MemoryStore
	breaker *Breaker
}

func NewLayered(redis *RedisStore, local *MemoryStore, breaker *Breaker) *LayeredStore {
	return &LayeredStore{redis: redis, local: local, breaker: breaker}
}

// RedisUp reports whether the breaker is closed (Redis reachable).
func (s *LayeredStore) RedisUp() bool { return s.breaker.State() }

func (s *LayeredStore) Save(ctx context.Context, id string, e Entry) error {
	// Always write to local cache first (fast path + fallback).
	if err := s.local.Save(ctx, id, e); err != nil {
		return err
	}
	if !s.breaker.Allow() {
		return nil // Redis open; local write is enough
	}
	if err := s.redis.Save(ctx, id, e); err != nil {
		s.breaker.Failure()
		slog.Warn("layered: redis save failed", "error", err)
		return nil // masking must not fail
	}
	s.breaker.Success()
	return nil
}

func (s *LayeredStore) Get(ctx context.Context, id string) (Entry, bool, error) {
	if e, ok, err := s.local.Get(ctx, id); err == nil && ok {
		return e, true, nil
	}
	if !s.breaker.Allow() {
		return Entry{}, false, nil // Redis open; no local hit
	}
	e, ok, err := s.redis.Get(ctx, id)
	if err != nil {
		s.breaker.Failure()
		slog.Warn("layered: redis get failed", "error", err)
		return Entry{}, false, nil
	}
	s.breaker.Success()
	if ok {
		_ = s.local.Save(ctx, id, e) // populate local cache
	}
	return e, ok, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ -run TestLayered -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/store/layered.go internal/store/layered_test.go
git commit -m "feat: layered store with local cache and circuit breaker"
```

---

### Task 3: Worker pool with fast/heavy queues

**Files:**
- Create: `internal/queue/pool.go`
- Test: `internal/queue/pool_test.go`

**Interfaces:**
- Consumes: ничего.
- Produces:
  - `type Job struct { Payload string; Heavy bool }`
  - `type Result struct { Masked string; Types []string; Tokens map[string]string; Err error }`
  - `type Handler func(payload string) Result`
  - `type Pool struct`; `func New(workers, fastCap, heavyCap int, h Handler) *Pool`
  - `func (p *Pool) Submit(ctx context.Context, payload string, heavy bool) (Result, error)`
  - `func (p *Pool) QueueDepth() int`
  - `func (p *Pool) WorkersBusy() int`
  - `func (p *Pool) Close()`

- [ ] **Step 1: Write the failing test**

```go
package queue

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPoolProcessesJob(t *testing.T) {
	p := New(2, 16, 8, func(payload string) Result {
		return Result{Masked: "masked:" + payload}
	})
	defer p.Close()
	res, err := p.Submit(context.Background(), "hello", false)
	require.NoError(t, err)
	require.Equal(t, "masked:hello", res.Masked)
}

func TestPoolBackpressure(t *testing.T) {
	// Handler blocks forever; fill the fast queue, then Submit must time out.
	block := make(chan struct{})
	p := New(1, 2, 2, func(payload string) Result {
		<-block
		return Result{}
	})
	defer p.Close()
	defer close(block)
	// Occupy the single worker.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	_, _ = p.Submit(ctx, "a", false)
	cancel()
	// Fill the fast queue (capacity 2).
	_, _ = p.Submit(context.Background(), "b", false)
	_, _ = p.Submit(context.Background(), "c", false)
	// Next submit with a short timeout -> error (queue full).
	ctx2, cancel2 := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel2()
	_, err := p.Submit(ctx2, "d", false)
	require.Error(t, err)
}

func TestPoolMetrics(t *testing.T) {
	var busy atomic.Int32
	p := New(2, 16, 8, func(payload string) Result {
		busy.Add(1)
		time.Sleep(20 * time.Millisecond)
		busy.Add(-1)
		return Result{}
	})
	defer p.Close()
	_, _ = p.Submit(context.Background(), "x", false)
	require.GreaterOrEqual(t, p.WorkersBusy(), 0)
	require.GreaterOrEqual(t, p.QueueDepth(), 0)
	_ = busy.Load()
}

func TestPoolHandlerError(t *testing.T) {
	p := New(1, 4, 4, func(payload string) Result {
		return Result{Err: errors.New("boom")}
	})
	defer p.Close()
	res, err := p.Submit(context.Background(), "x", false)
	require.NoError(t, err)
	require.Error(t, res.Err)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/queue/ -v`
Expected: FAIL — `undefined: New`

- [ ] **Step 3: Write minimal implementation**

```go
package queue

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
)

// ErrQueueFull is returned when both queues are full and the context expires.
var ErrQueueFull = errors.New("queue full")

// Job is a unit of work submitted to the pool.
type Job struct {
	Payload string
	Heavy   bool
	done    chan Result
}

// Result is the outcome of processing a Job.
type Result struct {
	Masked string
	Types  []string
	Tokens map[string]string
	Err    error
}

// Handler processes a payload into a Result.
type Handler func(payload string) Result

// Pool runs a fixed number of workers consuming from two queues: fast (short
// payloads, low latency) and heavy (long payloads). Submit blocks until a
// worker produces a result or the context expires.
type Pool struct {
	fast  chan Job
	heavy chan Job
	done  chan struct{}
	wg    sync.WaitGroup
	busy  atomic.Int32
}

// New returns a Pool with `workers` goroutines and the given queue capacities.
func New(workers, fastCap, heavyCap int, h Handler) *Pool {
	if workers <= 0 {
		workers = 1
	}
	p := &Pool{
		fast:  make(chan Job, fastCap),
		heavy: make(chan Job, heavyCap),
		done:  make(chan struct{}),
	}
	for i := 0; i < workers; i++ {
		p.wg.Add(1)
		go p.worker(h)
	}
	return p
}

func (p *Pool) worker(h Handler) {
	defer p.wg.Done()
	for {
		select {
		case <-p.done:
			return
		case j := <-p.fast:
			p.run(h, j)
		case j := <-p.heavy:
			p.run(h, j)
		}
	}
}

func (p *Pool) run(h Handler, j Job) {
	p.busy.Add(1)
	res := h(j.Payload)
	p.busy.Add(-1)
	j.done <- res
}

// Submit enqueues a job and waits for its result. It returns ErrQueueFull if
// the context expires before a worker picks up the job.
func (p *Pool) Submit(ctx context.Context, payload string, heavy bool) (Result, error) {
	j := Job{Payload: payload, Heavy: heavy, done: make(chan Result, 1)}
	select {
	case <-ctx.Done():
		return Result{}, ErrQueueFull
	default:
	}
	if heavy {
		select {
		case p.heavy <- j:
		case <-ctx.Done():
			return Result{}, ErrQueueFull
		}
	} else {
		select {
		case p.fast <- j:
		case <-ctx.Done():
			return Result{}, ErrQueueFull
		}
	}
	select {
	case res := <-j.done:
		return res, nil
	case <-ctx.Done():
		return Result{}, ErrQueueFull
	}
}

// QueueDepth returns the total number of queued jobs.
func (p *Pool) QueueDepth() int {
	return len(p.fast) + len(p.heavy)
}

// WorkersBusy returns the number of workers currently processing.
func (p *Pool) WorkersBusy() int {
	return int(p.busy.Load())
}

// Close stops the workers and waits for them to finish.
func (p *Pool) Close() {
	close(p.done)
	p.wg.Wait()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/queue/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/queue/pool.go internal/queue/pool_test.go
git commit -m "feat: worker pool with fast/heavy queues"
```

---

### Task 4: Config for queue, store circuit, autoscale

**Files:**
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Consumes: существующий `config.Config`.
- Produces:
  - `type QueueConfig struct { Workers int; FastCapacity int; HeavyCapacity int; HeavyThreshold int }`
  - `type CircuitConfig struct { Failures int; CooldownSeconds int }`
  - `type AutoscaleConfig struct { MinReplicas int; MaxReplicas int; CPUUp int; CPUDown int; QueueUp int; LatencyP99MS int; CooldownSeconds int; PollSeconds int }`
  - Поля в `Config`: `Queue QueueConfig`, `Store.Circuit CircuitConfig`, `Autoscale AutoscaleConfig`.

- [ ] **Step 1: Write the failing test**

```go
package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadQueueAndAutoscale(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
queue:
  workers: 8
  fast_capacity: 512
  heavy_capacity: 128
  heavy_threshold: 4096
store:
  type: layered
  circuit:
    failures: 5
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
`
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	cfg, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, 8, cfg.Queue.Workers)
	require.Equal(t, 512, cfg.Queue.FastCapacity)
	require.Equal(t, 128, cfg.Queue.HeavyCapacity)
	require.Equal(t, 4096, cfg.Queue.HeavyThreshold)
	require.Equal(t, "layered", cfg.Store.Type)
	require.Equal(t, 5, cfg.Store.Circuit.Failures)
	require.Equal(t, 5, cfg.Store.Circuit.CooldownSeconds)
	require.Equal(t, 20, cfg.Autoscale.MaxReplicas)
	require.Equal(t, 70, cfg.Autoscale.CPUUp)
	require.Equal(t, 10, cfg.Autoscale.PollSeconds)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestLoadQueueAndAutoscale -v`
Expected: FAIL — `cfg.Queue undefined`

- [ ] **Step 3: Write minimal implementation**

Add to `internal/config/config.go`:

```go
// QueueConfig controls the worker pool.
type QueueConfig struct {
	Workers        int `yaml:"workers"`
	FastCapacity   int `yaml:"fast_capacity"`
	HeavyCapacity  int `yaml:"heavy_capacity"`
	HeavyThreshold int `yaml:"heavy_threshold"` // payload bytes longer => heavy queue
}

// CircuitConfig controls the Redis circuit breaker.
type CircuitConfig struct {
	Failures        int `yaml:"failures"`
	CooldownSeconds int `yaml:"cooldown_seconds"`
}

// AutoscaleConfig controls the Swarm autoscaler.
type AutoscaleConfig struct {
	MinReplicas    int `yaml:"min_replicas"`
	MaxReplicas    int `yaml:"max_replicas"`
	CPUUp          int `yaml:"cpu_up"`
	CPUDown        int `yaml:"cpu_down"`
	QueueUp        int `yaml:"queue_up"`
	LatencyP99MS   int `yaml:"latency_p99_ms"`
	CooldownSeconds int `yaml:"cooldown_seconds"`
	PollSeconds    int `yaml:"poll_seconds"`
}
```

Add fields to `StoreConfig`:

```go
type StoreConfig struct {
	Type     string        `yaml:"type"` // "memory" | "redis" | "layered"
	TTLHours int           `yaml:"ttl_hours"`
	Capacity int           `yaml:"capacity"`
	RedisURL string        `yaml:"redis_url"`
	Circuit  CircuitConfig `yaml:"circuit"`
}
```

Add fields to `Config`:

```go
type Config struct {
	// ... existing fields ...
	Queue     QueueConfig     `yaml:"queue"`
	Autoscale AutoscaleConfig `yaml:"autoscale"`
}
```

Add defaults in `Default()`:

```go
return &Config{
	// ... existing ...
	Queue: QueueConfig{Workers: 16, FastCapacity: 1024, HeavyCapacity: 256, HeavyThreshold: 4096},
	Autoscale: AutoscaleConfig{MinReplicas: 2, MaxReplicas: 20, CPUUp: 70, CPUDown: 30, QueueUp: 100, LatencyP99MS: 100, CooldownSeconds: 30, PollSeconds: 10},
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/ -run TestLoadQueueAndAutoscale -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat: queue, circuit, autoscale config"
```

---

### Task 5: New metrics (queue depth, workers busy, redis up)

**Files:**
- Modify: `internal/observability/metrics.go`
- Test: `internal/observability/metrics_test.go`

**Interfaces:**
- Consumes: ничего.
- Produces: `var QueueDepth prometheus.Gauge`; `var WorkersBusy prometheus.Gauge`; `var RedisUp prometheus.Gauge`.

- [ ] **Step 1: Write the failing test**

```go
package observability

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

func TestNewGaugesRegistered(t *testing.T) {
	QueueDepth.Set(5)
	WorkersBusy.Set(3)
	RedisUp.Set(1)
	require.NoError(t, prometheus.Register(prometheus.NewGaugeFunc(
		prometheus.GaugeOpts{Name: "pii_queue_depth"},
		func() float64 { return QueueDepth.Get() },
	)))
	_ = testutil.CollectAndCount(QueueDepth)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/observability/ -run TestNewGauges -v`
Expected: FAIL — `undefined: QueueDepth`

- [ ] **Step 3: Write minimal implementation**

Add to `internal/observability/metrics.go`:

```go
// QueueDepth is the current total depth of the worker-pool queues.
var QueueDepth = promauto.NewGauge(prometheus.GaugeOpts{
	Name: "pii_queue_depth",
	Help: "Current total depth of the worker-pool queues.",
})

// WorkersBusy is the number of workers currently processing a job.
var WorkersBusy = promauto.NewGauge(prometheus.GaugeOpts{
	Name: "pii_workers_busy",
	Help: "Number of workers currently processing a job.",
})

// RedisUp is 1 when the Redis circuit breaker is closed, 0 when open.
var RedisUp = promauto.NewGauge(prometheus.GaugeOpts{
	Name: "pii_redis_up",
	Help: "1 when Redis is reachable, 0 when the circuit breaker is open.",
})
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/observability/ -run TestNewGauges -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/observability/metrics.go internal/observability/metrics_test.go
git commit -m "feat: queue depth, workers busy, redis up metrics"
```

---

### Task 6: Wire pool + layered store into server

**Files:**
- Modify: `cmd/server/main.go`
- Modify: `internal/api/handlers/process.go`
- Test: `internal/api/handlers/process_test.go`

**Interfaces:**
- Consumes: `queue.Pool` (Task 3), `store.LayeredStore` (Task 2), `config.QueueConfig`/`config.CircuitConfig` (Task 4), `observability.QueueDepth`/`WorkersBusy`/`RedisUp` (Task 5).
- Produces: `Handler` gains a `Pool *queue.Pool` field; `Handler.Process` routes through the pool.

- [ ] **Step 1: Write the failing test**

Add to `internal/api/handlers/process_test.go`:

```go
func TestProcessThroughPool(t *testing.T) {
	h := newTestHandler(t)
	// Attach a pool to the handler.
	h.Pool = queue.New(4, 64, 16, func(payload string) queue.Result {
		res := h.Mgr.Pipeline("").Process(payload)
		return queue.Result{Masked: res.Masked, Types: res.Types, Tokens: res.Tokens}
	})
	defer h.Pool.Close()
	rec := doProcess(t, h, "паспорт 4509 123456", "pool-1")
	require.Equal(t, http.StatusOK, rec.Code)
	var resp ProcessResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Contains(t, resp.Result, "[ПАСПОРТ]")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/api/handlers/ -run TestProcessThroughPool -v`
Expected: FAIL — `undefined: queue`

- [ ] **Step 3: Write minimal implementation**

In `internal/api/handlers/process.go`, add the `Pool` field to `Handler`:

```go
type Handler struct {
	Mgr  *control.Manager
	Repo *db.Repo
	Pool *queue.Pool
}
```

Add import: `"github.com/kind-earthquake/pii-module/internal/queue"`.

In `Process`, replace the direct pipeline call with a pool submission when a pool is present. Add a helper:

```go
// runPipeline executes the pipeline directly or through the worker pool.
func (h *Handler) runPipeline(ctx context.Context, p *pipeline.Pipeline, payload string) (pipeline.Result, error) {
	if h.Pool == nil {
		return p.Process(payload), nil
	}
	heavy := len(payload) > h.Mgr.Config().Queue.HeavyThreshold
	res, err := h.Pool.Submit(ctx, payload, heavy)
	if err != nil {
		return pipeline.Result{}, err
	}
	return pipeline.Result{Masked: res.Masked, Types: res.Types, Tokens: res.Tokens}, nil
}
```

In `Process`, replace:

```go
res := p.Process(req.Payload)
```

with:

```go
res, err := h.runPipeline(r.Context(), p, req.Payload)
if err != nil {
	http.Error(w, "service busy", http.StatusServiceUnavailable)
	w.Header().Set("Retry-After", "1")
	return
}
```

In `cmd/server/main.go`, build the pool and layered store. Replace the store construction block:

```go
var st store.Store
var redisClient *redis.Client
if cfg.Store.Type == "redis" || cfg.Store.Type == "layered" {
	redisClient = redis.NewClient(redisOptions(cfg.Store.RedisURL))
	redisStore := store.NewRedis(redisClient, ttl)
	if cfg.Store.Type == "layered" {
		local := store.NewMemory(ttl, cfg.Store.Capacity)
		breaker := store.NewBreaker(cfg.Store.Circuit.Failures, time.Duration(cfg.Store.Circuit.CooldownSeconds)*time.Second)
		st = store.NewLayered(redisStore, local, breaker)
	} else {
		st = redisStore
	}
} else {
	st = store.NewMemory(ttl, cfg.Store.Capacity)
}
```

Build the pool after `h` is created:

```go
h := &handlers.Handler{Mgr: mgr, Repo: repo}
pool := queue.New(cfg.Queue.Workers, cfg.Queue.FastCapacity, cfg.Queue.HeavyCapacity, func(payload string) queue.Result {
	res := mgr.Pipeline("").Process(payload)
	return queue.Result{Masked: res.Masked, Types: res.Types, Tokens: res.Tokens}
})
h.Pool = pool
defer pool.Close()
```

Add imports `"github.com/kind-earthquake/pii-module/internal/queue"` to main.go.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/api/handlers/ -run TestProcessThroughPool -v`
Expected: PASS

- [ ] **Step 5: Run full test suite**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add cmd/server/main.go internal/api/handlers/process.go internal/api/handlers/process_test.go
git commit -m "feat: route process through worker pool and layered store"
```

---

### Task 7: Metrics reporting loop (queue depth, workers busy, redis up)

**Files:**
- Create: `internal/observability/reporter.go`
- Test: `internal/observability/reporter_test.go`

**Interfaces:**
- Consumes: `queue.Pool` (Task 3), `store.LayeredStore` (Task 2), `observability.QueueDepth`/`WorkersBusy`/`RedisUp` (Task 5).
- Produces: `func StartReporter(ctx context.Context, pool *queue.Pool, layered *store.LayeredStore, interval time.Duration)`.

- [ ] **Step 1: Write the failing test**

```go
package observability

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kind-earthquake/pii-module/internal/queue"
	"github.com/kind-earthquake/pii-module/internal/store"
)

func TestReporterSetsGauges(t *testing.T) {
	p := queue.New(2, 8, 4, func(payload string) queue.Result { return queue.Result{} })
	defer p.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	StartReporter(ctx, p, nil, 10*time.Millisecond)
	time.Sleep(30 * time.Millisecond)
	require.GreaterOrEqual(t, QueueDepth.Get(), float64(0))
	require.GreaterOrEqual(t, WorkersBusy.Get(), float64(0))
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/observability/ -run TestReporter -v`
Expected: FAIL — `undefined: StartReporter`

- [ ] **Step 3: Write minimal implementation**

```go
package observability

import (
	"context"
	"time"

	"github.com/kind-earthquake/pii-module/internal/queue"
	"github.com/kind-earthquake/pii-module/internal/store"
)

// StartReporter periodically publishes pool and store health to Prometheus
// gauges. It runs until ctx is cancelled.
func StartReporter(ctx context.Context, pool *queue.Pool, layered *store.LayeredStore, interval time.Duration) {
	if interval <= 0 {
		interval = time.Second
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if pool != nil {
					QueueDepth.Set(float64(pool.QueueDepth()))
					WorkersBusy.Set(float64(pool.WorkersBusy()))
				}
				if layered != nil {
					if layered.RedisUp() {
						RedisUp.Set(1)
					} else {
						RedisUp.Set(0)
					}
				}
			}
		}
	}()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/observability/ -run TestReporter -v`
Expected: PASS

- [ ] **Step 5: Wire into main.go**

In `cmd/server/main.go`, after building the pool and store, start the reporter:

```go
var layeredStore *store.LayeredStore
if ls, ok := st.(*store.LayeredStore); ok {
	layeredStore = ls
}
reporterCtx, stopReporter := context.WithCancel(context.Background())
defer stopReporter()
observability.StartReporter(reporterCtx, pool, layeredStore, time.Duration(cfg.Autoscale.PollSeconds)*time.Second)
```

- [ ] **Step 6: Run full test suite**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/observability/reporter.go internal/observability/reporter_test.go cmd/server/main.go
git commit -m "feat: metrics reporter for queue and redis health"
```

---

### Task 8: Autoscaler container

**Files:**
- Create: `deploy/autoscaler/main.go`
- Create: `deploy/autoscaler/Dockerfile`
- Create: `deploy/autoscaler/go.mod`

**Interfaces:**
- Consumes: Prometheus HTTP API (query `pii_queue_depth`, `pii_workers_busy`, `pii_redis_up`, container CPU), Docker socket (`docker service scale`).
- Produces: автономный бинарник, читающий `AUTOSCALE_*` env vars.

- [ ] **Step 1: Write the autoscaler main**

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"time"
)

type cfg struct {
	promURL       string
	service       string
	minReplicas   int
	maxReplicas   int
	cpuUp         float64
	cpuDown       float64
	queueUp       float64
	latencyP99MS  float64
	cooldown      time.Duration
	poll          time.Duration
}

func load() cfg {
	atoi := func(k string, def int) int {
		if v := os.Getenv(k); v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				return n
			}
		}
		return def
	}
	atof := func(k string, def float64) float64 {
		if v := os.Getenv(k); v != "" {
			if n, err := strconv.ParseFloat(v, 64); err == nil {
				return n
			}
		}
		return def
	}
	return cfg{
		promURL:      os.Getenv("PROM_URL"),
		service:      os.Getenv("SERVICE"),
		minReplicas:  atoi("MIN_REPLICAS", 2),
		maxReplicas:  atoi("MAX_REPLICAS", 20),
		cpuUp:        atof("CPU_UP", 70),
		cpuDown:      atof("CPU_DOWN", 30),
		queueUp:      atof("QUEUE_UP", 100),
		latencyP99MS: atof("LATENCY_P99_MS", 100),
		cooldown:     time.Duration(atoi("COOLDOWN_SECONDS", 30)) * time.Second,
		poll:         time.Duration(atoi("POLL_SECONDS", 10)) * time.Second,
	}
}

func queryFloat(ctx context.Context, url string) (float64, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return 0, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var out struct {
		Data struct {
			Result []struct {
				Value []interface{} `json:"value"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return 0, err
	}
	if len(out.Data.Result) == 0 || len(out.Data.Result[0].Value) < 2 {
		return 0, fmt.Errorf("no data")
	}
	return strconv.ParseFloat(out.Data.Result[0].Value[1].(string), 64)
}

func currentReplicas(ctx context.Context, service string) (int, error) {
	cmd := exec.CommandContext(ctx, "docker", "service", "inspect", "--format", "{{.Spec.Mode.Replicated.Replicas}}", service)
	out, err := cmd.Output()
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(string(out))
}

func scale(ctx context.Context, service string, replicas int) error {
	cmd := exec.CommandContext(ctx, "docker", "service", "scale", fmt.Sprintf("%s=%d", service, replicas))
	return cmd.Run()
}

func main() {
	c := load()
	lastScale := time.Time{}
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		queueDepth, _ := queryFloat(ctx, c.promURL+"/api/v1/query?query=pii_queue_depth")
		latency, _ := queryFloat(ctx, c.promURL+"/api/v1/query?query=histogram_quantile(0.99, sum(rate(pii_latency_seconds_bucket[1m])) by (le))")
		cancel()

		replicas, err := currentReplicas(context.Background(), c.service)
		if err != nil {
			fmt.Fprintf(os.Stderr, "autoscaler: inspect: %v\n", err)
			time.Sleep(c.poll)
			continue
		}

		scaleUp := queueDepth > c.queueUp || latency > c.latencyP99MS
		scaleDown := queueDepth < c.queueUp/10 && latency < c.latencyP99MS/10

		if scaleUp && replicas < c.maxReplicas && time.Since(lastScale) > c.cooldown {
			fmt.Printf("autoscaler: scale up %s %d->%d (queue=%.0f latency=%.0fms)\n", c.service, replicas, replicas+1, queueDepth, latency)
			if err := scale(context.Background(), c.service, replicas+1); err != nil {
				fmt.Fprintf(os.Stderr, "autoscaler: scale up: %v\n", err)
			} else {
				lastScale = time.Now()
			}
		} else if scaleDown && replicas > c.minReplicas && time.Since(lastScale) > c.cooldown {
			fmt.Printf("autoscaler: scale down %s %d->%d\n", c.service, replicas, replicas-1)
			if err := scale(context.Background(), c.service, replicas-1); err != nil {
				fmt.Fprintf(os.Stderr, "autoscaler: scale down: %v\n", err)
			} else {
				lastScale = time.Now()
			}
		}
		time.Sleep(c.poll)
	}
}
```

- [ ] **Step 2: Write the Dockerfile**

```dockerfile
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY main.go ./
RUN CGO_ENABLED=0 go build -o /autoscaler .

FROM alpine:3.20
RUN apk add --no-cache docker-cli
COPY --from=build /autoscaler /autoscaler
ENTRYPOINT ["/autoscaler"]
```

- [ ] **Step 3: Write go.mod**

```
module autoscaler

go 1.25.0
```

- [ ] **Step 4: Build to verify**

Run: `cd deploy/autoscaler && go build ./...`
Expected: builds without error

- [ ] **Step 5: Commit**

```bash
git add deploy/autoscaler/
git commit -m "feat: swarm autoscaler container"
```

---

### Task 9: Swarm stack + prometheus config

**Files:**
- Create: `deploy/stack.yml`
- Modify: `prometheus.yml`

**Interfaces:**
- Consumes: Dockerfile (root), autoscaler image (Task 8).
- Produces: `docker stack deploy`-able stack with services: `pii`, `redis`, `postgres`, `prometheus`, `autoscaler`.

- [ ] **Step 1: Write the stack file**

```yaml
version: "3.8"

services:
  pii:
    image: pii-module:latest
    build:
      context: ..
      dockerfile: Dockerfile
    environment:
      - PORT=8080
      - DATABASE_DSN=postgres://pii:pii@postgres:5432/pii?sslmode=disable
      - STORE=layered
      - REDIS_URL=redis://redis:6379/0
    deploy:
      replicas: 2
      update_config:
        parallelism: 1
        delay: 5s
      resources:
        limits:
          cpus: "1.0"
          memory: 512M
    networks:
      - pii-net

  redis:
    image: redis:7-alpine
    command: ["redis-server", "--appendonly", "yes"]
    deploy:
      replicas: 1
    networks:
      - pii-net

  postgres:
    image: postgres:16-alpine
    environment:
      - POSTGRES_USER=pii
      - POSTGRES_PASSWORD=pii
      - POSTGRES_DB=pii
    deploy:
      replicas: 1
    networks:
      - pii-net

  prometheus:
    image: prom/prometheus:latest
    volumes:
      - ./prometheus.yml:/etc/prometheus/prometheus.yml:ro
    deploy:
      replicas: 1
    networks:
      - pii-net

  autoscaler:
    image: pii-autoscaler:latest
    build:
      context: ./autoscaler
      dockerfile: Dockerfile
    environment:
      - PROM_URL=http://prometheus:9090
      - SERVICE=pii
      - MIN_REPLICAS=2
      - MAX_REPLICAS=20
      - CPU_UP=70
      - CPU_DOWN=30
      - QUEUE_UP=100
      - LATENCY_P99_MS=100
      - COOLDOWN_SECONDS=30
      - POLL_SECONDS=10
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
    deploy:
      replicas: 1
    networks:
      - pii-net

networks:
  pii-net:
    driver: overlay
```

- [ ] **Step 2: Update prometheus.yml**

Add a scrape job for the pii service (via Swarm DNS) and node-exporter/cAdvisor if present:

```yaml
scrape_configs:
  - job_name: 'pii'
    static_configs:
      - targets: ['pii:8080']
    metrics_path: /metrics
```

- [ ] **Step 3: Validate YAML**

Run: `docker compose -f deploy/stack.yml config`
Expected: valid config output (or note that stack.yml uses `deploy:` keys which compose validates)

- [ ] **Step 4: Commit**

```bash
git add deploy/stack.yml prometheus.yml
git commit -m "feat: swarm stack and prometheus scrape config"
```

---

### Task 10: Degradation integration tests

**Files:**
- Modify: `internal/api/handlers/process_test.go`

**Interfaces:**
- Consumes: `store.LayeredStore` (Task 2), `queue.Pool` (Task 3).
- Produces: тесты деградации при недоступном Redis.

- [ ] **Step 1: Write the failing test**

Add to `internal/api/handlers/process_test.go`:

```go
func TestProcessLayeredRedisDown(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	redisStore := store.NewRedis(client, time.Hour)
	local := store.NewMemory(time.Hour, 1000)
	breaker := store.NewBreaker(1, time.Minute)
	layered := store.NewLayered(redisStore, local, breaker)
	m, err := control.New(control.WithConfig(config.Default()), control.WithStore(layered))
	require.NoError(t, err)
	h := &Handler{Mgr: m}
	// Mask while Redis is up.
	rec := doProcess(t, h, "паспорт 4509 123456", "deg-1")
	require.Equal(t, http.StatusOK, rec.Code)
	var resp ProcessResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Contains(t, resp.Result, "[ПАСПОРТ]")
	// Kill Redis.
	mr.Close()
	// Unmask from local cache still works.
	rec2 := doProcess(t, h, resp.Result, "deg-1")
	require.Equal(t, http.StatusOK, rec2.Code)
	var resp2 ProcessResponse
	require.NoError(t, json.Unmarshal(rec2.Body.Bytes(), &resp2))
	require.Equal(t, "паспорт 4509 123456", resp2.Result)
	// Masking still works with Redis down.
	rec3 := doProcess(t, h, "паспорт 4509 123456", "deg-2")
	require.Equal(t, http.StatusOK, rec3.Code)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/api/handlers/ -run TestProcessLayeredRedisDown -v`
Expected: FAIL — `undefined: store.NewLayered` (if not yet wired) or behavior mismatch

- [ ] **Step 3: Run test to verify it passes**

Run: `go test ./internal/api/handlers/ -run TestProcessLayeredRedisDown -v`
Expected: PASS (after Task 2/6 are done)

- [ ] **Step 4: Run full test suite**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/api/handlers/process_test.go
git commit -m "test: layered store degradation when redis is down"
```

---

### Task 11: Update config.yaml and README

**Files:**
- Modify: `configs/config.yaml`
- Modify: `README.md`

**Interfaces:**
- Consumes: новые секции конфига (Task 4).
- Produces: документированная конфигурация и инструкция по Swarm-деплою.

- [ ] **Step 1: Update config.yaml**

Add to `configs/config.yaml`:

```yaml
queue:
  workers: 16
  fast_capacity: 1024
  heavy_capacity: 256
  heavy_threshold: 4096

store:
  type: layered
  ttl_hours: 24
  capacity: 262144
  redis_url: "redis://localhost:6379/0"
  circuit:
    failures: 5
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

- [ ] **Step 2: Update README**

Add a section "Горизонтальное масштабирование (Docker Swarm)" describing:
- `docker stack deploy -c deploy/stack.yml pii`
- Автоскейлинг через autoscaler-контейнер
- Деградация при недоступности Redis

- [ ] **Step 3: Commit**

```bash
git add configs/config.yaml README.md
git commit -m "docs: swarm scaling config and README"
```

---

### Task 12: Final verification

**Files:**
- None (verification only).

**Interfaces:**
- Consumes: все предыдущие задачи.

- [ ] **Step 1: Run full test suite**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 2: Run vet**

Run: `go vet ./...`
Expected: no output

- [ ] **Step 3: Build**

Run: `go build ./...`
Expected: builds without error

- [ ] **Step 4: Run bench smoke test**

Run: `go run ./cmd/bench -c 10 -d 3s`
Expected: completes, rps > 0, no failures

- [ ] **Step 5: Commit any remaining changes**

```bash
git add -A
git commit -m "chore: final verification"
```