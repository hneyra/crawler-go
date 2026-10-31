# ADR-0002: renderer headless dentro del worker (chromedp)

**Estado:** aceptado

**Contexto.** El crawler Java usaba un servicio Node (Express + patchright) aparte para renderizar páginas con
JavaScript y pasar challenges de Cloudflare. Era un segundo lenguaje, otra imagen y otro servicio que operar, y
su intercepción de XHR estaba rota (abortaba las peticiones en vez de capturarlas).

**Decisión.** Renderizar desde Go con chromedp (CDP) contra Chromium en la imagen `worker`. Un navegador por
proceso, una pestaña por render (comparten cookies → la autorización de Cloudflare se reutiliza), arranque
perezoso y reinicio si muere. Se fija chromedp v0.11.2 (la API de v0.21 cambió a genérica).

**Consecuencias.** Un solo lenguaje e imagen de app (la del worker añade Chromium). Menos evasión anti-bot que
patchright: sitios con protecciones fuertes pueden requerir más trabajo (perfil persistente, headful). La API no
necesita Chromium.
