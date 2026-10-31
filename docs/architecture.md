# Arquitectura

Port en Go del crawler Java ([hneyra/crawler](https://github.com/hneyra/crawler)) con el stack de
`costea-backend` / `costea-frontend`. Monolito modular, un solo binario (`crawler`) con subcomandos.

```
            navegador (admin)
                  │ https
          ┌───────▼────────┐        /api/*         ┌──────────────┐
          │ web (SvelteKit)│ ────────────────────▶ │ api (serve)  │──┐
          └────────────────┘   (proxy o ingress)   └──────────────┘  │ encola jobs
                                                                       ▼
                                   ┌─────────────────────────── PostgreSQL ───────────┐
                                   │ site, category, discovered_url, extracted_item,  │
                                   │ crawl_run, jobs, users, sessions, login_throttle │
                                   └───────────────────────────▲──────────────────────┘
                                                               │ claim (SKIP LOCKED)
                         ┌────────────────────────┐   ┌────────┴────────┐
                         │ sitios crawleados      │◀──│ worker          │── snapshots ─▶ /data/raw
                         │ (HTTP / Chromium CDP)  │   │ (+ Chromium)    │
                         └────────────────────────┘   └─────────────────┘
```

| Proceso | Comando | Responsabilidad |
|---|---|---|
| API | `crawler serve` | REST (`api/openapi.yaml`), auth por sesión, encola ejecuciones, métricas |
| Worker | `crawler worker` | Ejecuta jobs: discovery, extracción, mantenimiento periódico |
| Migraciones | `crawler migrate up` | goose, migraciones embebidas (job previo al rollout) |
| Web | `node build` (`web/`) | SPA de administración; proxy `/api` al backend |

## Capas (hexagonal)

- **Dominio** (sin BD ni HTTP de transporte; lo vigila `internal/archtest`):
  `crawl` (entidades, validación, casos de uso de configuración), `discovery`, `extraction`, `pipeline`
  (ejecuciones), `jobs` (cola + runner), `identity`, `urlx`, `storage`, `sitesconfig`, `fetch` (HTTP saliente).
- **Adaptadores**: `postgres` (SQL a mano con pgx), `httpapi` (servidor strict generado con oapi-codegen),
  `render/headless` (chromedp), `metrics` (Prometheus).
- **Plataforma**: `platform/{config,db,dbtest,httpx}`.
- **Wiring** manual en `cmd/crawler/wire*.go` (sin contenedor de DI).

## Decisiones

Ver [ADRs](adr/): stack, renderer en Go, cola en Postgres.
Diferencias funcionales con el crawler Java: [data-flow.md](data-flow.md#diferencias-con-el-crawler-java).
