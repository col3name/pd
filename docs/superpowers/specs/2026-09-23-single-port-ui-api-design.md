# Single-Port UI + API via web nginx

## Problem

The PII Gateway exposes its services on multiple ports: the Go API on `8080`
(`/process`, `/v1/*`, `/health`, `/metrics`, `/docs`, `/openapi.yaml`) and the
web admin UI on `5173` (SPA). The web UI nginx currently proxies only `/v1/`
and `/process` to the Go API; the other API routes (`/health`, `/metrics`,
`/docs`, `/openapi.yaml`) are not reachable through the web UI port. This
fragments the external surface and complicates the demo and the load test.

## Goal

Expose the web UI and the Go API through a single external port. The web nginx
becomes the single entry point: it proxies all API routes to the Go API and
serves the SPA for everything else. Update the README with the collected URLs,
the architecture scheme, and the demo artifacts.

## Non-goals

- Grafana and Prometheus stay on their own ports (not consolidated).
- Postgres stays internal-only.
- No changes to the Go API itself.

## Design

### 1. `web/nginx.conf` — proxy all API routes

Add proxy locations for every API route the Go API serves, and keep the SPA
fallback for everything else:

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

### 2. `docker-compose.yml` — single external port

- Keep `admin` (web UI) exposed on port `5173` externally — this is the single
  entry point for both UI and API.
- Make `pii-module-v2` internal-only: remove its `ports` mapping so it is only
  reachable through the web nginx proxy. (The load tester and demo hit the web
  UI port, which proxies `/process`.)

### 3. README.md — update

- Document the single-port architecture: one external port serves both the SPA
  and the API.
- Collect all URLs (UI, API, docs, metrics, Grafana, Prometheus).
- Add the 1–2 minute explanation of the principle and architecture.
- Add the scheme: Consumer → Module (identification → masking → LLM →
  unmasking) → Consumer.
- Add the required demo artifacts (how to send a test text, how to get
  masked/unmasked result, where to see logs and metrics).

## Data flow

```
External client (single port, e.g. 5173)
  → web nginx
    ├── /v1/*, /process, /health, /metrics, /docs, /openapi.yaml → Go API (8080, internal)
    └── / (everything else) → SPA (index.html)
```

## Verification

- `docker compose up -d --build` starts all services.
- `curl http://localhost:5173/health` → `ok` (proxied to Go API).
- `curl http://localhost:5173/process` → masks (proxied to Go API).
- `curl http://localhost:5173/` → SPA (admin UI).
- `curl http://localhost:5173/docs` → Swagger UI (proxied to Go API).
- `curl http://localhost:5173/metrics` → Prometheus metrics (proxied to Go API).