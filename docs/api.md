# API

Contrato completo: [`api/openapi.yaml`](../api/openapi.yaml) (spec-first; el servidor y los tipos del panel se
generan desde ahí). Errores en `application/problem+json` (RFC 9457) con `code` estable y `errors[]` por campo.

## Autenticación

1. `POST /api/v1/auth/login` `{email, password}` → cookie `crawler_session` (HttpOnly, SameSite=Lax) + sesión.
2. `GET /api/v1/auth/session` → `{user, csrf_token, expires_at}`.
3. POST/PATCH/DELETE requieren `X-CSRF-Token` y, desde navegador, `Origin == CRAWLER_PUBLIC_URL`.
4. `POST /api/v1/auth/logout`.

Todo lo que no es `auth`/`ops` requiere un usuario admin (`crawler admin create|promote`).
Login: 5 intentos fallidos por email → bloqueo exponencial (30 s … 15 min); además límite por IP.

## Endpoints

| Método | Ruta | Descripción |
|---|---|---|
| GET | `/healthz`, `/readyz` | vida / BD lista (públicos) |
| GET/POST | `/api/v1/sites` | listar / crear sitio |
| GET/PATCH/DELETE | `/api/v1/sites/{id}` | detalle / actualización parcial / borrado en cascada |
| GET/POST | `/api/v1/sites/{id}/categories` | categorías del sitio |
| GET/PATCH/DELETE | `/api/v1/categories/{id}` | categoría |
| POST | `/api/v1/sites/{id}/crawl` | ejecución `CRAWL_SITE` (202) |
| POST | `/api/v1/sites/{id}/discover` | `DISCOVER_SITE` (202) |
| POST | `/api/v1/sites/{id}/extract` | `EXTRACT_SITE` (202) |
| POST | `/api/v1/categories/{id}/discover` | `DISCOVER_CATEGORY` (202) |
| GET | `/api/v1/runs?site_id=&page=&per_page=` | ejecuciones (más recientes primero) |
| GET | `/api/v1/runs/{id}` | ejecución con `stats` y `error` |
| GET | `/api/v1/sites/{id}/stats` | URLs por estado, items, último crawl |
| GET | `/api/v1/items?site_id=&page=&per_page=` | items extraídos |
| GET | `/api/v1/items/{id}` | item |

## Ejemplo con curl

```bash
curl -c cj -H 'content-type: application/json' \
  -d '{"email":"admin@ejemplo.com","password":"..."}' http://localhost:8080/api/v1/auth/login
CSRF=$(curl -s -b cj http://localhost:8080/api/v1/auth/session | jq -r .csrf_token)
curl -b cj -X POST -H "X-CSRF-Token: $CSRF" http://localhost:8080/api/v1/sites/1/crawl
curl -b cj 'http://localhost:8080/api/v1/items?site_id=1&per_page=5' | jq .
```
