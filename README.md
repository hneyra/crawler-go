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
crawler worker             ejecuta los jobs (crawl runs + mantenimiento); /healthz y /metrics en CRAWLER_WORKER_HEALTH_ADDR
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
Fallo de una URL → `FAILED` + `retry_count`; fallo propio (disco/BD/Chrome que no arranca) o apagado → las URLs
vuelven a `PENDING` sin consumir reintentos. El HTML se decodifica según su charset (latin-1 → UTF-8); contenido
no almacenable (NUL, UTF-8 inválido, números infinitos) se limpia antes de guardar.
`json_paths` usa la sintaxis de [ojg](https://github.com/ohler55/ojg): las funciones de Jayway (`.length()`,
`.min()`) no están soportadas y se rechazan al validar; una ruta inválida en datos antiguos se omite con un aviso.
Paths indefinidos (`$..x`, `[*]`, filtros) siempre devuelven lista, como Jayway. Las fechas de los snapshots son UTC.

## Ejecuciones (jobs)

La API encola y el worker ejecuta (cola en Postgres con `SKIP LOCKED`, lease + heartbeat, reintentos con backoff):

| Endpoint | Ejecución |
|---|---|
| `POST /api/v1/categories/{id}/discover` | `DISCOVER_CATEGORY` |
| `POST /api/v1/sites/{id}/discover` | `DISCOVER_SITE` (todas las categorías habilitadas) |
| `POST /api/v1/sites/{id}/extract` | `EXTRACT_SITE` (lotes hasta vaciar `PENDING`) |
| `POST /api/v1/sites/{id}/crawl` | `CRAWL_SITE` (discovery + extracción) |

Responden `202` con la ejecución (`crawl_run`); pedir la misma ejecución mientras está activa devuelve la existente.
Estado en `GET /api/v1/runs/{id}` y `GET /api/v1/runs?site_id=`; conteos en `GET /api/v1/sites/{id}/stats`;
items en `GET /api/v1/items?site_id=&page=&per_page=` (propiedades como objeto JSON).

Jobs periódicos del worker:
- `maintenance.retry_failed` cada `CRAWLER_RETRY_INTERVAL`: `FAILED` con `retry_count < CRAWLER_RETRY_MAX` → `PENDING`.
- `maintenance.rescue_stuck`: `IN_PROGRESS` más viejas que `CRAWLER_STUCK_AFTER` (worker muerto) → `PENDING`.
- `maintenance.purge` cada hora: jobs terminados hace más de 7 días y filas viejas de throttle de login.

Métricas Prometheus: API (`/metrics` en `CRAWLER_METRICS_ADDR`) `crawler_sites_total`, `crawler_urls{status}`,
`crawler_items_total`, `crawler_http_*`; worker `crawler_discovery_*`, `crawler_fetch_*`, `crawler_extraction_*`.

## Desarrollo

- `make generate`: regenera `internal/httpapi/api.gen.go` desde `api/openapi.yaml`.
- `make check`: gofmt, vet, golangci-lint, govulncheck.
- `make test`: tests con Postgres real (levanta `db` con compose). Los tests del renderer usan Chrome
  (`CRAWLER_TEST_CHROME=/ruta/a/chrome`; sin Chrome se omiten).

Plan de trabajo: issues `[Mx.y]` agrupados por milestone (`milestone:m0` … `milestone:m7`), un PR por milestone.
