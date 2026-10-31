# ADR-0003: ejecuciones sobre una cola en Postgres

**Estado:** aceptado

**Contexto.** Java usaba Spring Batch (tablas propias, ejecución en el proceso de la API) y un scheduler para
reintentos. Se necesitan ejecuciones asíncronas, reintentos, varios workers y sobrevivir a reinicios, sin sumar
Redis/RabbitMQ.

**Decisión.** Cola `jobs` en Postgres (diseño de costea-backend): `FOR UPDATE SKIP LOCKED`, lease + heartbeat,
backoff exponencial, dedupe por clave, crash guard. Cada pedido crea un `crawl_run` (estado visible en la API) y
un job en la misma transacción. Las URLs usan el mismo patrón (`SKIP LOCKED`) para repartir la extracción. Jobs
periódicos que se re-encolan por ventana para mantenimiento.

**Consecuencias.** Una sola dependencia de infraestructura. Throughput suficiente para un crawler de catálogo
(decenas de jobs/s); si creciera mucho habría que mover a un broker dedicado.
