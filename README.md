# crawler-go

Crawler web configurable por sitio, con soporte para páginas estáticas y dinámicas (AJAX/SPA). Descubre URLs de productos desde páginas de listado y extrae datos estructurados de cada producto.

Port en Go de [hneyra/crawler](https://github.com/hneyra/crawler) con el stack de `costea-backend` / `costea-frontend`:

| Componente | Tecnología |
|---|---|
| API + worker | Go 1.27, `net/http`, OpenAPI spec-first (oapi-codegen strict), pgx, goose, slog, Prometheus |
| Render headless | chromedp (Chromium) |
| Cola de jobs | Postgres (`FOR UPDATE SKIP LOCKED`) |
| Base de datos | PostgreSQL 18 |
| Panel admin | SvelteKit 3 + Svelte 5 (`web/`) |

## Inicio rápido

```bash
cp .env.example .env
make db-up
go run ./cmd/crawler migrate up
go run ./cmd/crawler serve
curl localhost:8080/healthz
```

Stack completo en Docker: `make up`.

### Usuario administrador

La API y el panel requieren sesión de administrador (no hay registro público):

```bash
echo 'una contraseña larga' | go run ./cmd/crawler admin create admin@ejemplo.com
# o: CRAWLER_ADMIN_PASSWORD=... crawler admin create admin@ejemplo.com
```

Login: `POST /api/v1/auth/login` deja la cookie `crawler_session`; `GET /api/v1/auth/session` devuelve el
`csrf_token` que las operaciones POST/PATCH/DELETE deben enviar en `X-CSRF-Token`.

### Configurar sitios

Mismo formato YAML que el crawler Java (ver `sites-config.example.yml`):

```bash
cp sites-config.example.yml sites-config.yml
go run ./cmd/crawler sync-sites
```

También se gestionan por API: `GET/POST /api/v1/sites`, `GET/PATCH/DELETE /api/v1/sites/{id}`,
`GET/POST /api/v1/sites/{id}/categories`, `GET/PATCH/DELETE /api/v1/categories/{id}` (contrato en `api/openapi.yaml`).

## Comandos

```
crawler serve              API HTTP (+ métricas en CRAWLER_METRICS_ADDR)
crawler migrate up|status|down-to N
crawler sync-sites [archivo]   carga sites-config.yml (upsert por nombre)
crawler admin create|set-password|promote EMAIL
crawler healthcheck [url]
crawler version
```

## Desarrollo

- `make generate`: regenera `internal/httpapi/api.gen.go` desde `api/openapi.yaml`.
- `make check`: gofmt, vet, golangci-lint, govulncheck.
- `make test`: tests con Postgres real (levanta `db` con compose).

Plan de trabajo: issues `[Mx.y]` agrupados por milestone (`milestone:m0` … `milestone:m7`), un PR por milestone.
