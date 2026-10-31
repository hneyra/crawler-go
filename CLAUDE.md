# crawler-go

Crawler configurable por sitio (port en Go de [hneyra/crawler](https://github.com/hneyra/crawler)).
Stack y convenciones = `costeape/costea-backend` (Go) + `costeape/costea-frontend` (SvelteKit, en `web/`).

## Reglas
- Comentarios en código: inglés corto estilo "caveman" (`// fragment dropped. query kept.`). Docs y UI en español. Identificadores en inglés.
- Hexagonal. Dominio (`internal/crawl`, `discovery`, `extraction`, `urlx`, `storage`, `jobs`, `identity`...) no importa pgx, net/http (salvo `fetch`), chromedp ni adaptadores. Lo vigila `internal/archtest`.
- Adaptadores: `internal/postgres` (SQL a mano, pgx, `db.Q(ctx, pool)`), `internal/httpapi` (strict server generado), `internal/render/chromedp`.
- Spec-first: cambia `api/openapi.yaml`, luego `make generate`. Nunca editar `*.gen.go`.
- Migraciones goose en `migrations/`: nunca editar una aplicada; versión siempre creciente (goose rechaza fuera de orden); rangos 00001-09 plataforma, 00010-19 crawl, 00020-29 identidad, 00030-39 jobs.
- Errores: `apperr` tipados, envueltos con contexto (`fmt.Errorf("load site %d: %w", id, err)`); HTTP → problem+json.
- Contenido de terceros (HTML, JSON) = no confiable. Nunca ejecutar ni renderizar sin escapar.
- Sin goroutines sin fin definido; sin estado global mutable; ctx propagado.
- Config solo por env (`CRAWLER_*`, ver `.env.example`).

## Web (`web/`)
- Convenciones de costea-frontend: SvelteKit 3 (config en `vite.config.ts`), runes, alias `#lib/*`, `$app/state`.
- Toda la app es SPA privada (`ssr=false`, noindex). Nunca `{@html}` con datos de sitios crawleados.
- Tipos desde `api/openapi.yaml` (`yarn api:sync && yarn api:gen`); nunca editar `schema.d.ts` a mano.
- Verificar: `yarn api:check && yarn run check && yarn lint && yarn test && yarn build` (`make web-check`).

## Verificar antes de decir "listo"
- `make check` (gofmt, vet, golangci-lint, govulncheck) y `make test` (Postgres real vía compose; `CRAWLER_TEST_DATABASE_URL`).
- Tests de integración contra Postgres real con `dbtest.New(t)`; sin mocks de BD. Nunca debilitar un test.

## Git
- Un issue por tarea (`[Mx.y]`), un PR por milestone, commits convencionales. Nunca commitear secretos.
