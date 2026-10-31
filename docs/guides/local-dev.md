# Desarrollo local

## Requisitos

- Go 1.27, Docker (Postgres vía compose), Node ≥ 22.17 y Yarn 1.22 (panel web).
- Opcional: Chrome/Chromium (sitios con Cloudflare, `SCROLL_AJAX`, estrategia `AJAX`).

## Backend

```bash
cp .env.example .env
make db-up                                   # postgres en 127.0.0.1:5441
go run ./cmd/crawler migrate up
echo 'una contraseña larga' | go run ./cmd/crawler admin create admin@ejemplo.com
go run ./cmd/crawler serve                   # API :8080, métricas :9091
go run ./cmd/crawler worker                  # jobs; /healthz y /metrics en :8081
```

`CRAWLER_PUBLIC_URL` debe ser el origen del panel (`http://localhost:5173` con `yarn dev`).

## Panel

```bash
cd web && yarn install && yarn dev           # http://localhost:5173 (proxy /api -> :8080)
```

## Todo en Docker

```bash
make up-full                                 # db, migrate, api, worker (con Chromium), web en :3000
docker compose exec api /crawler admin create admin@ejemplo.com   # pide la contraseña por stdin
```

## Probar un crawl sin panel

```bash
cp sites-config.example.yml sites-config.yml   # edita sitios/categorías
go run ./cmd/crawler sync-sites
go run ./cmd/crawler discover CATEGORY_ID      # en primer plano, imprime el resultado
go run ./cmd/crawler extract SITE_ID
```

## Tests

```bash
make check            # gofmt, vet, golangci-lint, govulncheck
make test             # go test -race con Postgres real (compose)
CRAWLER_TEST_CHROME=/ruta/a/chrome make test   # incluye los tests del renderer
make web-check        # api:check, svelte-check, lint, vitest, build
```

E2E del panel contra un stack levantado:

```bash
cd web
BASE_URL=http://localhost:3000 E2E_ADMIN_EMAIL=admin@ejemplo.com E2E_ADMIN_PASSWORD=... yarn e2e
```
