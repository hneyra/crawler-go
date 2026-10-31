# Configurar sitios

Mismo formato YAML que el crawler Java (`sites-config.example.yml`). `crawler sync-sites [archivo]` hace upsert
por nombre de sitio y de categoría en una transacción: si algo es inválido no se guarda nada. Lo que no está en
el archivo no se borra. Las claves desconocidas son error (evita typos silenciosos).

```yaml
sites:
  - name: MiTienda                 # único
    baseUrl: https://mitienda.pe
    enabled: true                  # default true
    politenessDelayMs: 1000        # pausa entre peticiones al sitio
    extractionConfig:
      detailStrategy: HTML         # HTML | SCRIPT_JSON | AJAX
      fieldSelectors:              # campo -> selector CSS (texto del primer match)
        title: h1.product-title
        price: span.price
      scriptPatterns:              # SCRIPT_JSON: regex sobre <script>, grupo 1 = JSON
        - 'window\.__DATA__\s*=\s*(\{.*?\});'
      jsonPaths:                   # campo -> JSONPath (sobre scripts, XHR interceptados y JSON-LD)
        sku: $.sku
      interceptPatterns:           # AJAX: glob de URLs XHR a capturar
        - '*/api/product/*'
    categories:
      - name: celulares
        url: https://mitienda.pe/celulares
        paginationType: PAGE_PARAM # PAGE_PARAM | CURSOR | SCROLL_AJAX
        itemLinkSelector: a.product-link
        nextPageSelector: a.next   # opcional; vacío = rel=next / «Siguiente» / › / »
        linkExtractionType: HREF   # HREF | ONCLICK | DATA_ATTRIBUTE
        linkAttribute: data-url    # solo DATA_ATTRIBUTE
        enabled: true
```

## Elegir estrategia

| Cómo vienen los datos en la página de detalle | Estrategia |
|---|---|
| En el HTML visible | `HTML` + `fieldSelectors` |
| En un `<script>` como objeto JS/JSON | `SCRIPT_JSON` + `scriptPatterns` + `jsonPaths` |
| Los carga el navegador por XHR/fetch | `AJAX` + `interceptPatterns` + `jsonPaths` (necesita Chromium) |

JSON-LD (`application/ld+json`) se guarda siempre en `jsonLd` y también se le aplican los `jsonPaths`.
Rutas indefinidas (`$..x`, `[*]`, filtros) devuelven lista. No hay funciones de Jayway (`.length()`).

## Regex

Sintaxis RE2 de Go (sin backreferences ni lookaround). `$` es fin de texto, igual que en Java sin flags.
