# CDDM Dashboard

Minimal Stage 1 foundation for the CDDM Dashboard roadmap. The repository is organized as a small monorepo:

```text
backend/   Go HTTP API and SQLite migrations
web/       React and TypeScript frontend
.github/   GitHub Actions verification
```

A future Chrome extension can be added as a separate top-level workspace without changing the backend or web layout.

## Requirements

- Go 1.23+
- Node.js 20.19+ and npm
- Docker with Docker Compose (optional)

## Local development

Copy the environment template when you need local overrides:

```bash
cp .env.example .env
```

Start the backend:

```bash
cd backend
APP_DATABASE_PATH=./data/cddm.db go run ./cmd/server
```

The API listens on `http://localhost:8080` by default. Its health endpoint is:

```bash
curl http://localhost:8080/api/health
```

Start the frontend in another terminal:

```bash
cd web
npm ci
npm run dev
```

Open `http://localhost:5173`. The lightweight Node development server proxies `/api` requests to the backend. Set `API_PROXY_TARGET` to override the default `http://localhost:8080` target.

## Docker Compose

Build and start both services:

```bash
docker compose up --build
```

Open `http://localhost:3000`. The web container proxies health requests to the API container. SQLite data is persisted in the named `cddm_data` volume.

Ports can be overridden through `.env` using `API_PORT` and `WEB_PORT`.

## Configuration

| Variable | Default | Purpose |
| --- | --- | --- |
| `APP_ADDR` | `:8080` | Backend listen address for local runs |
| `APP_DATABASE_PATH` | `data/cddm.db` | SQLite database path for local runs |
| `APP_SHUTDOWN_TIMEOUT` | `10s` | Graceful shutdown deadline |
| `API_PORT` | `8080` | Host API port in Docker Compose |
| `WEB_PORT` | `3000` | Host web port in Docker Compose |

## Verification

Backend formatting and tests:

```bash
cd backend
test -z "$(gofmt -l .)"
go test ./...
```

Frontend clean install and production build:

```bash
cd web
npm ci
npm run build
```

Compose configuration:

```bash
docker compose config --quiet
```
