# Single-Port UI + API Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Expose the web admin UI and the Go API through a single external port by making the web nginx proxy all API routes, and update the README with collected URLs, the architecture scheme, and demo artifacts.

**Architecture:** The web nginx (currently serving the SPA on port 5173) becomes the single entry point. It proxies every API route (`/v1/*`, `/process`, `/health`, `/metrics`, `/docs`, `/openapi.yaml`, `/process_api.yaml`) to the Go API (internal port 8080) and serves the SPA for everything else. The Go API port is removed from external exposure in docker-compose.

**Tech Stack:** nginx, Docker Compose, Markdown.

## Global Constraints

- Single external port = the web UI port `5173`.
- The Go API port `8080` becomes internal-only (reachable only through the web nginx proxy).
- Grafana (`3000`) and Prometheus (`9090`) stay on their own ports; Postgres (`5432`) stays internal.
- `docker compose up -d --build` must start all services.
- Git identity: `git -c user.name="col3name" -c user.email="mmikushov@instaloper.com" commit -m "..."`

---

### Task 1: Proxy all API routes in web nginx

**Files:**
- Modify: `web/nginx.conf`

**Interfaces:**
- Consumes: nothing new.
- Produces: nginx config that proxies `/v1/`, `/process`, `/health`, `/metrics`, `/docs`, `/openapi.yaml`, `/process_api.yaml` to `http://pii-module-v2:8080`, and serves the SPA for everything else.

- [ ] **Step 1: Rewrite web/nginx.conf**

Replace `web/nginx.conf` with:

```nginx
server {
    listen 80;
    server_name _;

    root /usr/share/nginx/html;
    index index.html;

    # API routes → Go API
    location /v1/ {
        proxy_pass http://pii-module-v2:8080;
        proxy_set_header Host $host;
        proxy_set_header Authorization $http_authorization;
    }

    location /process {
        proxy_pass http://pii-module-v2:8080;
        proxy_set_header Host $host;
    }

    location /health {
        proxy_pass http://pii-module-v2:8080;
        proxy_set_header Host $host;
    }

    location /metrics {
        proxy_pass http://pii-module-v2:8080;
        proxy_set_header Host $host;
    }

    location /docs {
        proxy_pass http://pii-module-v2:8080;
        proxy_set_header Host $host;
    }

    location = /openapi.yaml {
        proxy_pass http://pii-module-v2:8080;
        proxy_set_header Host $host;
    }

    location = /process_api.yaml {
        proxy_pass http://pii-module-v2:8080;
        proxy_set_header Host $host;
    }

    # Everything else → SPA
    location / {
        try_files $uri /index.html;
    }
}
```

- [ ] **Step 2: Validate nginx config syntax**

Run: `cd /Users/mikha/Desktop/github/ai/prc-data-challenge-2026/pii-module/version2 && docker run --rm -v "$PWD/web/nginx.conf:/etc/nginx/conf.d/default.conf:ro" nginx:1.27-alpine nginx -t`
Expected: `syntax is ok` and `test is successful`.

- [ ] **Step 3: Commit**

```bash
cd /Users/mikha/Desktop/github/ai/prc-data-challenge-2026/pii-module/version2
git add web/nginx.conf
git -c user.name="col3name" -c user.email="mmikushov@instaloper.com" commit -m "web: proxy all API routes through single nginx entry"
```

---

### Task 2: Make the Go API internal-only in docker-compose

**Files:**
- Modify: `docker-compose.yml`

**Interfaces:**
- Consumes: the web nginx from Task 1 (now proxies all API routes).
- Produces: a compose file where only the web UI port `5173` is exposed externally; the Go API port `8080` is removed from `ports`.

- [ ] **Step 1: Remove the Go API external port**

In `docker-compose.yml`, change the `pii-module-v2` service to remove its `ports` mapping (it becomes internal-only, reachable only via the web nginx proxy):

```yaml
  pii-module-v2:
    build: .
    environment:
      DATABASE_DSN: "postgres://pii:pii@postgres:5432/pii?sslmode=disable"
    restart: unless-stopped
    depends_on:
      - postgres
```

(Remove the `ports: - "8080:8080"` block.)

- [ ] **Step 2: Verify compose config**

Run: `cd /Users/mikha/Desktop/github/ai/prc-data-challenge-2026/pii-module/version2 && docker compose config --quiet`
Expected: no output (config valid).

- [ ] **Step 3: Commit**

```bash
cd /Users/mikha/Desktop/github/ai/prc-data-challenge-2026/pii-module/version2
git add docker-compose.yml
git -c user.name="col3name" -c user.email="mmikushov@instaloper.com" commit -m "docker: expose only web UI port, make API internal"
```

---

### Task 3: Update README with URLs, architecture scheme, and demo artifacts

**Files:**
- Modify: `README.md`

**Interfaces:**
- Consumes: the single-port architecture from Tasks 1-2.
- Produces: an updated README documenting the single external port, all URLs, the architecture scheme, and the demo artifacts.

- [ ] **Step 1: Update the architecture section**

In `README.md`, replace the architecture diagram (lines 11-31) with a version that shows the single-port entry and the Consumer → Module → LLM → Consumer flow:

```markdown
## Архитектура

```
Система-потребитель / Жюри / LLM
     │  POST /process {payload, payload_id}   (один внешний порт :5173)
     ▼
┌──────────────────────────────────────────────────────────────┐
│                    web nginx (единая точка входа)            │
│  /v1/*  /process  /health  /metrics  /docs  /openapi.yaml    │
│  └───────────────► pii-module-v2 (внутренний :8080)          │
│  /  (всё остальное) → SPA (admin UI)                         │
└──────────────────────────────────────────────────────────────┘
     │
     ▼
┌──────────────────────────────────────────────────────────────┐
│                      pii-module-v2                           │
│  Идентификация → Маскирование → (LLM) → Демаскирование       │
│  Detector → эскалация дат → Resolver → Whitelist → Context   │
│  → Confidence → Mask/Tokenize/Synthetic → Store              │
└──────────────────────────────────────────────────────────────┘
```

**Принцип работы (1–2 минуты):** шлюз принимает текст с ПД, детектирует
персональные данные детерминированными правилами + контекстом, маскирует их
(redact/token/synthetic), при необходимости передаёт замаскированный текст в
LLM, а затем демаскирует ответ по сохранённой карте `token → значение`.
Критичные типы ПД определяются независимыми проверяемыми правилами, а не LLM,
что даёт предсказуемость и высокий RPS.
```

- [ ] **Step 2: Add a "URLs" section**

Add a new section after the architecture section listing all URLs on the single external port:

```markdown
## Все URL (единый внешний порт :5173)

| URL | Назначение |
|-----|------------|
| `http://<host>:5173/` | Админка (SPA) — список команд, детальные настройки |
| `http://<host>:5173/process` | POST — маскирование/демаскирование |
| `http://<host>:5173/v1/systems` | Управление системами (Bearer-токен) |
| `http://<host>:5173/v1/config` | Read-only конфигурация |
| `http://<host>:5173/v1/auth/login` | Вход в админку |
| `http://<host>:5173/health` | Health-check → `ok` |
| `http://<host>:5173/metrics` | Prometheus-метрики |
| `http://<host>:5173/docs` | Swagger UI (Try it out) |
| `http://<host>:5173/openapi.yaml` | OpenAPI-спецификация |
| `http://<host>:9090` | Prometheus (отдельный порт) |
| `http://<host>:3000` | Grafana (отдельный порт) |
```

- [ ] **Step 3: Add a "Демо для жюри" section**

Add a section with the demo artifacts (how to send a test text, get masked/unmasked result, where to see logs and metrics):

```markdown
## Демо для жюри

**1. Отправить тестовый текст (маскирование):**
```bash
curl -X POST http://localhost:5173/process -H 'Content-Type: application/json' \
  -d '{"payload":"Клиент Иванов Иван Иванович, паспорт 4509 123456, тел +7 912 345-67-89, карта 4276 1234 5678 9012","payload_id":"demo-1"}'
# → {"result":"Клиент [ФИО], паспорт [ПАСПОРТ], тел [ТЕЛЕФОН], карта [КАРТА]"}
```

**2. Получить демаскированный результат (тот же payload_id + маска):**
```bash
curl -X POST http://localhost:5173/process -H 'Content-Type: application/json' \
  -d '{"payload":"Клиент [ФИО], паспорт [ПАСПОРТ], тел [ТЕЛЕФОН], карта [КАРТА]","payload_id":"demo-1"}'
# → {"result":"Клиент Иванов Иван Иванович, паспорт 4509 123456, тел +7 912 345-67-89, карта 4276 1234 5678 9012"}
```

**3. Логи:** `docker compose logs -f pii-module-v2` — структурированные (slog),
без значений ПД (`payload_id`, `types`, `latency_ms`).

**4. Метрики:** `http://localhost:5173/metrics` (Prometheus) и Grafana
`http://localhost:3000` (admin/admin) — дашборд `pii_requests_total`,
`pii_latency_seconds`, `pii_detected_total`.

**5. Готовый сценарий:** `./scripts/curl_demo.sh http://localhost:5173`
```

- [ ] **Step 4: Update the quick-start and API contract URLs**

In `README.md`, update the quick-start curl examples and the API contract section to use the single external port `5173` instead of `8080`. Replace `http://localhost:8080` with `http://localhost:5173` throughout the README (search and replace all occurrences).

- [ ] **Step 5: Verify README renders**

Run: `cd /Users/mikha/Desktop/github/ai/prc-data-challenge-2026/pii-module/version2 && grep -c "localhost:5173" README.md`
Expected: a positive count (URLs updated).

- [ ] **Step 6: Commit**

```bash
cd /Users/mikha/Desktop/github/ai/prc-data-challenge-2026/pii-module/version2
git add README.md
git -c user.name="col3name" -c user.email="mmikushov@instaloper.com" commit -m "docs: single-port URLs, architecture scheme, demo artifacts"
```

---

### Task 4: End-to-end verification

**Files:**
- Verify only (no new files unless a fix is needed).

- [ ] **Step 1: Build and start services**

Run: `cd /Users/mikha/Desktop/github/ai/prc-data-challenge-2026/pii-module/version2 && docker compose up -d --build`
Expected: all services start.

- [ ] **Step 2: Verify single-port routing**

Run:
```bash
curl -s http://localhost:5173/health          # → ok (proxied to Go API)
curl -s http://localhost:5173/                # → SPA (admin UI)
curl -s -o /dev/null -w "%{http_code}" http://localhost:5173/docs   # → 200 (Swagger UI)
curl -s -o /dev/null -w "%{http_code}" http://localhost:5173/metrics # → 200 (Prometheus)
curl -s -X POST http://localhost:5173/process -H 'Content-Type: application/json' \
  -d '{"payload":"паспорт 4509 123456","payload_id":"v-1"}'          # → masked result
```
Expected: health returns `ok`, `/` returns the SPA, `/docs` and `/metrics` return 200, `/process` masks.

- [ ] **Step 3: Verify the Go API port is not externally exposed**

Run: `curl -s -o /dev/null -w "%{http_code}" http://localhost:8080/health`
Expected: connection refused (port not mapped externally).

- [ ] **Step 4: Final commit (if any fixes)**

```bash
cd /Users/mikha/Desktop/github/ai/prc-data-challenge-2026/pii-module/version2
git add -A
git -c user.name="col3name" -c user.email="mmikushov@instaloper.com" commit -m "chore: single-port fixes from verification"
```