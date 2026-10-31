# ADR-0001: mismo stack que costea

**Estado:** aceptado

**Contexto.** El crawler Java (Spring Boot + renderer Node + React) se reescribe. El equipo mantiene
`costea-backend` (Go) y `costea-frontend` (SvelteKit) con convenciones probadas: OpenAPI spec-first, SQL a mano,
cola en Postgres, auth por sesión, CI y despliegue k3s con Pulumi.

**Decisión.** Reutilizar el stack y las convenciones de costea: Go 1.27 + `net/http` + oapi-codegen strict +
pgx + goose + slog + Prometheus; SvelteKit 3 + Svelte 5 + openapi-fetch en `web/`; monorepo (backend en la raíz,
panel en `web/`) para que el contrato y su cliente cambien en el mismo PR.

**Consecuencias.** Código y plataforma compartibles con costea (se portaron `platform/*`, `jobs`, `identity`,
componentes del panel, infra). Monorepo en lugar de dos repos: un PR por milestone toca ambos lados.
