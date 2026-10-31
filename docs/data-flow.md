# Flujo de datos

## 1. Configuración

`sites-config.yml` (`crawler sync-sites`) o la API/panel crean **sitios** (con su `extraction_config`) y
**categorías** (páginas de listado). Upsert por nombre; nada se borra por omisión.

## 2. Ejecución

`POST /api/v1/sites/{id}/crawl` (o `discover`, `extract`, `categories/{id}/discover`) crea un `crawl_run`
`QUEUED` y un job `crawl.run` en la misma transacción. Si ya hay una ejecución activa del mismo tipo y destino,
se devuelve esa (dedupe por `(kind, dedupe_key)` en `jobs`).

El worker reclama el job (`FOR UPDATE SKIP LOCKED`, lease 30 s con heartbeat) y ejecuta:

### Discovery (por categoría)

1. GET HTTP del listado (UA de Chrome, timeout 15 s). Si responde 403/429/503 o es un challenge de Cloudflare,
   se renderiza con Chromium (espera el selector de items).
2. Se extraen los enlaces de `item_link_selector` según `link_extraction_type` (`HREF`, `ONCLICK` por regex,
   `DATA_ATTRIBUTE`), se resuelven contra la URL final (tras redirects) o `<base href>`, se normalizan (sin
   fragmento) y se insertan como `PENDING` con `url_hash = sha256(url)` (dedupe global).
3. Siguiente página: `next_page_selector` → `a[rel=next]` → texto «Siguiente/Next/›/»». Si apunta a una página
   ya visitada (paginación JS) se hace click real con Chromium. Hasta `CRAWLER_DISCOVERY_MAX_PAGES`, con
   `politeness_delay_ms` entre páginas.
4. `SCROLL_AJAX`: un render con scroll infinito y extracción de enlaces sobre el DOM final.

### Extracción (por sitio, en lotes)

1. Reclama hasta `CRAWLER_EXTRACTION_BATCH_SIZE` URLs `PENDING` → `IN_PROGRESS`.
2. Por URL: fetch (o render con intercepción XHR si la estrategia es `AJAX`), snapshot en
   `CRAWLER_RAW_DATA_DIR/{site}/{fecha UTC}/{sha256}.html|json`, y extracción:
   `field_selectors` (CSS) → `script_patterns` + `json_paths` (`SCRIPT_JSON`) → respuestas interceptadas
   (`AJAX`) → JSON-LD (siempre, al final).
3. Upsert de `extracted_item` (uno por URL) → `COMPLETED`. Error de la página → `FAILED` + `retry_count`.
   Error propio (disco, BD, Chrome) o apagado → la URL vuelve a `PENDING` sin gastar reintento.
4. Repite lotes hasta vaciar la cola del sitio.

## 3. Mantenimiento (worker, periódico)

| Job | Qué hace |
|---|---|
| `maintenance.retry_failed` | `FAILED` con `retry_count < CRAWLER_RETRY_MAX` → `PENDING` cada `CRAWLER_RETRY_INTERVAL` |
| `maintenance.rescue_stuck` | `IN_PROGRESS` más viejas que `CRAWLER_STUCK_AFTER` → `PENDING` |
| `maintenance.purge` | jobs terminados > 7 días, throttle de login inactivo > 24 h |

## Diferencias con el crawler Java

| Tema | Java | Go |
|---|---|---|
| Renderer | servicio Node + patchright aparte | chromedp en el worker (Chrome solo si hace falta) |
| Intercepción AJAX | patrones **abortaban** las peticiones (nunca devolvía datos) | captura real de respuestas |
| Concurrencia | lotes sin bloqueo (dos procesos podían tomar la misma URL) | `SKIP LOCKED` |
| URLs atascadas | `IN_PROGRESS` para siempre tras un crash | rescate periódico |
| Items | varios por URL | uno por URL (upsert) |
| `properties` en API | string JSON | objeto JSON |
| Auth | ninguna | sesiones de admin + CSRF |
| `sites-config.yml` | solo inserta sitios nuevos | upsert transaccional, claves desconocidas = error |
| JSONPath | Jayway | ojg (sin funciones `.length()`/`.min()`; se rechazan al validar) |
| Fecha de snapshots | hora local | UTC |
| Hash de URLs no ASCII | `URI` decodificado | escapado RFC 3986 (no importar hashes Java tal cual) |
