# Problemas frecuentes

| Síntoma | Causa probable | Qué hacer |
|---|---|---|
| Login responde 403 `bad_origin` | `CRAWLER_PUBLIC_URL` ≠ origen del panel | Igualar (incluye puerto y esquema) |
| POST desde el panel responde 403 sin JSON | `ORIGIN` de adapter-node ≠ origen real | Fijar `ORIGIN`/`CRAWLER_WEB_ORIGIN` |
| Login 429 | 5 fallos seguidos (bloqueo exponencial) o límite por IP | Esperar; `crawler admin set-password` limpia el bloqueo |
| Ejecución queda `QUEUED` | No hay worker corriendo | `crawler worker`; revisar `/healthz` del worker |
| `headless renderer unavailable` | Chrome no instalado o no arranca | Usar la imagen `worker` o `CRAWLER_CHROME_PATH`; en contenedores, `shm_size` ≥ 256 MB |
| Discovery con 0 URLs | Selector de items no coincide, o el listado se arma con JS | Probar el selector en el navegador; usar `SCROLL_AJAX` |
| `errors: 1` en discovery | El listado devolvió error (4xx/5xx) o se bloqueó | Ver `last_error` en la ejecución |
| URLs `FAILED` | La página de detalle falló | `last_error` en la URL; se reintentan solas hasta `CRAWLER_RETRY_MAX` |
| Regla de extracción ignorada | JSONPath/regex inválido guardado antes de la validación | Ver logs `extraction rule skipped`; corregir en el sitio |
| Snapshots no se escriben | `CRAWLER_RAW_DATA_DIR` no escribible (rootfs read-only) | Montar un volumen en `/data` |
