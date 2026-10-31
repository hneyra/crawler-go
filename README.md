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
crawler discover CATEGORY_ID   discovery de una categoría en primer plano (debug)
crawler extract SITE_ID        extrae todas las URLs pendientes de un sitio en primer plano (debug)
crawler healthcheck [url]
crawler version
```

## Discovery

Recorre las páginas de listado de cada categoría y guarda las URLs de detalle (deduplicadas por sha256 de la URL normalizada):

| Paginación | Cómo |
|---|---|
| `PAGE_PARAM`, `CURSOR` | GET HTTP; siguiente página por `next_page_selector`, `a[rel=next]` o texto «Siguiente»/«Next»/›/». Si el enlace apunta a la misma página (paginación JS) se hace click con Chrome. |
| `SCROLL_AJAX` | Chrome headless con scroll infinito. |

Si el sitio responde 403/429/503 o una página de challenge de Cloudflare, se reintenta con Chrome headless (chromedp).
Chrome solo se arranca cuando hace falta; ruta con `CRAWLER_CHROME_PATH` (vacío = buscar en PATH).

## Extracción

Toma lotes de URLs `PENDING` (`FOR UPDATE SKIP LOCKED`, varios workers no se pisan), guarda el HTML crudo en
`CRAWLER_RAW_DATA_DIR/{sitio}/{fecha}/{sha256}.html` y extrae propiedades:

| Estrategia | Fuente |
|---|---|
| `HTML` | `field_selectors` (CSS, texto del primer match) |
| `SCRIPT_JSON` | `field_selectors` + regex `script_patterns` sobre `<script>` (grupo 1) + `json_paths` |
| `AJAX` | Chrome headless; respuestas XHR que coinciden con `intercept_patterns` + `json_paths`; JSON crudo en `ajax_<status>_<url>` |

JSON-LD (`<script type="application/ld+json">`) se extrae siempre como `jsonLd`. El título es la propiedad `title`
o, si falta, el `<title>` de la página. Un item por URL: re-extraer lo actualiza.
Fallo de una URL → `FAILED` + `retry_count`; fallo propio (disco/BD) o apagado → las URLs vuelven a `PENDING`.

## Desarrollo

- `make generate`: regenera `internal/httpapi/api.gen.go` desde `api/openapi.yaml`.
- `make check`: gofmt, vet, golangci-lint, govulncheck.
- `make test`: tests con Postgres real (levanta `db` con compose). Los tests del renderer usan Chrome
  (`CRAWLER_TEST_CHROME=/ruta/a/chrome`; sin Chrome se omiten).

Plan de trabajo: issues `[Mx.y]` agrupados por milestone (`milestone:m0` … `milestone:m7`), un PR por milestone.
