# Despliegue en k3s con Pulumi

Proyecto en `infra/` (Pulumi TypeScript + Yarn classic). Es un port del despliegue de costea-backend con las mismas convenciones. Este documento distingue entre:

- **Validación local** (cualquiera puede ejecutarla): `cd infra && yarn verify` (eslint + `tsc --noEmit` + vitest) y `pulumi preview` del stack `local`.
- **Acciones del usuario** (marcadas con ⚠️): `pulumi up`, `pulumi destroy`, importar imágenes en k3s, crear stacks de producción, crear el administrador, restaurar backups. Ningún agente las ejecuta.

## Qué se despliega

| Objeto | Notas |
|---|---|
| `Namespace` | Pod Security Admission `restricted` (todos los pods deben cumplirlo). |
| `ConfigMap crawler-config` | Variables `CRAWLER_*` no secretas, generadas desde la config tipada (ver tabla de claves). |
| `Secret crawler-app` | Solo `CRAWLER_DATABASE_URL`. |
| PostgreSQL (`database.mode=inCluster`) | `Secret crawler-db`, PVC `db-data`, `Deployment db` (estrategia `Recreate`, 1 réplica, `postgres:18-trixie`, uid 999, sondas `pg_isready`), `Service db` (ClusterIP), `NetworkPolicy db-ingress`. |
| Backups (`inCluster`) | PVC `db-backups` + `CronJob db-backup` (`pg_dump -Fc` diario, retención en días). Ver [Backups y restauración](#backups-y-restauración). |
| PVC `raw-data` | Datos del worker: snapshots HTML (`/data/raw`) y perfil de Chromium (`/data/profile`, conserva las cookies de Cloudflare). Siempre se crea, también con BD externa. |
| `Job migrate-<hash>` | `crawler migrate up`. El nombre incluye un hash de la imagen: una etiqueta nueva crea un Job nuevo. Pulumi espera a que termine antes de desplegar las apps. |
| `Deployment/Service api` | `crawler serve` (imagen distroless). Liveness `/healthz`, readiness `/readyz`, `timeoutSeconds: 3`. El puerto de métricas (9091) no está en el Service. No monta `/data`: la API no lee snapshots. |
| `Deployment worker` | `crawler worker` con la imagen **worker** (binario + Chromium, `deploy/Dockerfile --target worker`, uid 65532). Estrategia `Recreate` y como máximo 1 réplica (PVC `ReadWriteOnce` y un único perfil de Chromium). Monta `raw-data` en `/data`, `emptyDir` en `/tmp` y un `emptyDir` en memoria (512Mi) en `/dev/shm`. Sondas `exec`: `/crawler healthcheck http://127.0.0.1:8081/healthz` (listener solo loopback, ningún puerto expuesto). |
| `Deployment/Service web` | Panel SvelteKit (uid 1000). Sondas `/healthz`, `API_INTERNAL_URL=http://api:8080`, `ORIGIN`/`PUBLIC_ORIGIN` = URL pública, `ADDRESS_HEADER=X-Forwarded-For`, `XFF_DEPTH=1`. |
| `Middleware api-ratelimit` + `IngressRoute crawler` | Traefik (`traefik.io/v1alpha1`), un solo host: `Host(h) && PathPrefix(/api)` → `api:8080` con rate limit; `Host(h)` → `web:3000`. `/readyz` y `/metrics` de la API no se enrutan. |

Todos los contenedores: `requests` + `limits`, `runAsNonRoot`, `readOnlyRootFilesystem` (con `emptyDir` en `/tmp`), `allowPrivilegeEscalation: false`, `capabilities.drop: [ALL]`, `seccompProfile: RuntimeDefault`, sin token de service account. Chromium funciona así porque la app lo lanza con `--no-sandbox`.

La `NetworkPolicy` de la BD solo admite pods con la etiqueta `crawler.io/db-client: "true"` (api, worker, migrate y backup). k3s aplica NetworkPolicy con su controlador integrado.

## Estructura del proyecto

```text
infra/
├── Pulumi.yaml          # runtime nodejs, packagemanager yarn
├── Pulumi.local.yaml    # config NO secreta del stack local
├── config.ts            # único lector de pulumi.Config + invariantes puras
├── components/*.ts      # funciones puras que devuelven manifiestos
├── index.ts             # provider explícito + creación de recursos
└── test/*.test.ts       # vitest, sin clúster
```

## Requisitos

- k3s con Traefik incluido (CRDs `traefik.io/v1alpha1`) y el provisionador `local-path` (o una `storageClass` propia).
- Pulumi CLI 3.272, Node 26, Yarn 1.22.
- Un kubeconfig **del clúster local** (por ejemplo `~/.kube/k3s.yaml`). Compruébalo antes (solo la línea `server`):

```sh
grep -E '^\s*server:' ~/.kube/k3s.yaml     # debe ser https://127.0.0.1:6443
```

El stack `local` rechaza (al hacer preview/up) cualquier kubeconfig cuyo `server` no sea loopback. El provider siempre recibe el kubeconfig como secreto: nunca se usa el contexto por defecto de `kubectl`.

## Backend de estado: fichero local

No se usa Pulumi Cloud ni se cambia el `pulumi login` global. El backend se fija por comando con variables de entorno:

```sh
cd infra
yarn install --frozen-lockfile
export PULUMI_BACKEND_URL="file://$PWD/.pulumi-state"   # ignorado por git
export PULUMI_CONFIG_PASSPHRASE='solo-dev-local'          # cifra los secretos del stack local
```

`PULUMI_CONFIG_PASSPHRASE` es una solución **solo para desarrollo**. Para producción se usa un proveedor de secretos real (KMS, Vault).

## Configurar el stack local

`Pulumi.local.yaml` (versionado) solo contiene valores no secretos. Los secretos se guardan en una copia fuera de git para que el fichero versionado no reciba ni `encryptionsalt` ni valores cifrados:

```sh
pulumi stack init local --secrets-provider passphrase --no-select
# stack init añade "encryptionsalt" a Pulumi.local.yaml: muévelo a la copia privada
CFG=.pulumi-state/Pulumi.local.yaml
{ grep '^encryptionsalt:' Pulumi.local.yaml; grep -v '^encryptionsalt:' Pulumi.local.yaml; } > "$CFG"
sed -i '/^encryptionsalt:/d' Pulumi.local.yaml

S="pulumi config set --secret --stack local --config-file $CFG"
openssl rand -base64 24 | tr -d '\n' | $S crawler:dbPassword
$S crawler:kubeconfig < ~/.kube/k3s.yaml
```

Si cambias `Pulumi.local.yaml`, vuelve a copiar las claves no secretas a `$CFG` (o edita directamente `$CFG`).

### Claves de configuración (`crawler:`)

| Clave | Tipo | Notas |
|---|---|---|
| `env` | `dev`\|`prod` | `prod` activa invariantes extra. |
| `namespace` | string | Etiqueta DNS. |
| `images` | `{backend, worker?, web, tag, workerTag?, webTag?, webRunAsUser?, pullSecret?}` | Repos sin etiqueta. `worker` por defecto `<backend>-worker`; `workerTag` y `webTag` por defecto `tag`. `latest` prohibido; `dev` prohibido en prod. |
| `database` | `{mode, storageSize, storageClass?, backup: {enabled, schedule, retentionDays, storageSize}}` | `inCluster` exige el secreto `dbPassword`; `external` exige `databaseUrl` y backups desactivados. |
| `storage` | `{rawDataSize, storageClass?}` | PVC `raw-data` del worker. Por defecto `10Gi`. |
| `replicas` | `{api, worker, web}` | `worker` solo `0` o `1`: para más paralelismo sube `crawl.workerConcurrency`. |
| `resources` | `{api\|worker\|web\|migrate\|db\|backup: {requests, limits}}` | Sobrescribe los valores por defecto parcialmente. Worker por defecto: requests `250m/512Mi`, limits `2/2Gi` (el `/dev/shm` en memoria cuenta contra el límite). |
| `ingress` | `{host, tls?: {certResolver \| secretName}, rateLimit: {average, burst}}` | TLS obligatorio en prod. El host define `CRAWLER_PUBLIC_URL` y el `ORIGIN` del panel. |
| `trustedProxies` | string | CIDRs separados por comas (`CRAWLER_TRUSTED_PROXIES`). En k3s, la red de pods (`10.42.0.0/16`). |
| `crawl` | `{discoveryMaxPages, extractionBatchSize, retryMax, retryInterval, stuckAfter, workerConcurrency}` | Por defecto `50`, `10`, `3`, `10m`, `30m`, `2` (los mismos que `config.go`). Duraciones en formato Go. |
| `appEnv` | mapa | Otras `CRAWLER_*` no secretas y no gestionadas (p. ej. `CRAWLER_FETCH_TIMEOUT`, `CRAWLER_DB_MAX_CONNS`). `CRAWLER_HEADLESS=false` prohibido; en prod también `CRAWLER_COOKIE_SECURE=false` y `CRAWLER_RATE_LIMIT=off`. |
| secretos | `kubeconfig`, `dbPassword` \| `databaseUrl` | Siempre con `--secret`. |

Variables que fija la infra (no se pueden poner en `appEnv`): `CRAWLER_ENV`, `CRAWLER_HTTP_ADDR=:8080`, `CRAWLER_METRICS_ADDR=:9091`, `CRAWLER_PUBLIC_URL`, `CRAWLER_TRUSTED_PROXIES`, `CRAWLER_WORKER_HEALTH_ADDR=127.0.0.1:8081`, `CRAWLER_RAW_DATA_DIR=/data/raw`, `CRAWLER_CHROME_PATH=/usr/bin/chromium`, `CRAWLER_BROWSER_PROFILE_DIR=/data/profile` y las seis del bloque `crawl`. `CRAWLER_DATABASE_URL` y `CRAWLER_ADMIN_PASSWORD` son secretas y también están prohibidas en `appEnv`.

## Preview (validación local)

```sh
cd infra && pulumi preview --stack local --config-file $CFG
```

El preview resuelve la configuración, ejecuta las comprobaciones (contraseña de BD de al menos 16 caracteres, kubeconfig loopback, etc.) y consulta el clúster a través del provider. No crea nada.

## Cómo llegan las imágenes a k3s

Hay tres imágenes: `backend` (api + migrate, distroless), `worker` (mismo binario + Chromium) y `web`.

Opción A — registro (recomendada para cualquier entorno compartido): CI o el usuario publica `ghcr.io/hneyra/crawler-go:<tag>`, `ghcr.io/hneyra/crawler-go-worker:<tag>` y `ghcr.io/hneyra/crawler-web:<tag>`. Si los paquetes son privados, ⚠️ el usuario crea el secret de pull y lo referencia en `images.pullSecret`:

```sh
kubectl -n crawler-local create secret docker-registry ghcr --docker-server=ghcr.io \
  --docker-username=<usuario> --docker-password=<token-read-packages>
```

Opción B — ⚠️ importación manual en el containerd de k3s (solo clúster local):

```sh
TAG=$(git rev-parse --short HEAD)
docker build -f deploy/Dockerfile --build-arg VERSION=$TAG -t ghcr.io/hneyra/crawler-go:$TAG .
docker build -f deploy/Dockerfile --target worker --build-arg VERSION=$TAG -t ghcr.io/hneyra/crawler-go-worker:$TAG .
docker build -t ghcr.io/hneyra/crawler-web:$TAG web
for i in crawler-go crawler-go-worker crawler-web; do docker save ghcr.io/hneyra/$i:$TAG | sudo k3s ctr images import -; done
pulumi config set --stack local --config-file $CFG --path images.tag $TAG
```

Usa etiquetas inmutables (sha o semver). Con una etiqueta reutilizada (`dev`) `imagePullPolicy: IfNotPresent` no recoge cambios y el Job de migración no se vuelve a ejecutar.

## ⚠️ Desplegar y destruir (acciones del usuario)

```sh
cd infra
pulumi preview --stack local --config-file $CFG    # revisar siempre
pulumi up      --stack local --config-file $CFG    # ⚠️ usuario
pulumi destroy --stack local --config-file $CFG    # ⚠️ usuario; borra también los PVC (BD, backups y snapshots)
```

Orden que aplica Pulumi: namespace → config/secretos → datos (BD, backups, PVC `raw-data`) → Job de migración (espera a que termine) → api/worker/web → rutas de Traefik. Si la migración falla, las apps no se actualizan y la versión anterior sigue sirviendo.

El worker usa `Recreate`: en cada despliegue el pod anterior se detiene antes de arrancar el nuevo (unos segundos sin worker). Los trabajos que queden `IN_PROGRESS` vuelven a `PENDING` tras `crawl.stuckAfter`.

Cambiar el ConfigMap reinicia api y worker (anotación `crawler.io/config-hash`). Rotar un secreto no los reinicia: tras cambiarlo, ⚠️ `kubectl -n <ns> rollout restart deploy/api deploy/worker`.

## ⚠️ Primer administrador

La API y el panel no tienen registro público. Tras el primer `pulumi up` crea el administrador dentro del pod de la API. La imagen es distroless (sin shell), así que la contraseña se pasa por **stdin**; `CRAWLER_ADMIN_PASSWORD` solo sirve fuera del clúster (y en la infra es una clave secreta prohibida en `appEnv`). Nunca la pases como argumento.

```sh
read -rs PW    # escribe la contraseña (no queda en el historial)
printf '%s\n' "$PW" | kubectl -n crawler-local exec -i deploy/api -- /crawler admin create admin@ejemplo.com
unset PW
```

Igual para `admin set-password EMAIL` (revoca sus sesiones) y `admin promote EMAIL`. Después entra en `http://crawler.localhost` (o el host configurado).

## ⚠️ Cargar sitios

`crawler sync-sites [archivo]` necesita el YAML (formato de `sites-config.example.yml`) dentro del pod. Opciones:

1. **Por stdin** (sin copiar ficheros; funciona en la imagen distroless):

   ```sh
   kubectl -n crawler-local exec -i deploy/api -- /crawler sync-sites /dev/stdin < sites-config.yml
   ```

2. **`kubectl cp` al worker** (su imagen tiene `tar`; la distroless de la API no, por eso `kubectl cp` falla contra `api`):

   ```sh
   POD=$(kubectl -n crawler-local get pod -l app.kubernetes.io/component=worker -o name | head -1)
   kubectl -n crawler-local cp sites-config.yml "${POD#pod/}":/data/sites-config.yml
   kubectl -n crawler-local exec deploy/worker -- /crawler sync-sites /data/sites-config.yml
   ```

3. **Panel web**: alta y edición de sitios y categorías desde la UI (misma API `/api/v1/sites`).

`sync-sites` hace upsert por nombre; puede repetirse.

## Backups y restauración

Con `database.mode=inCluster` y `database.backup.enabled` (obligatorio en prod) el `CronJob db-backup` ejecuta cada noche (`17 3 * * *` UTC por defecto):

1. `pg_dump -Fc` a `/backups/crawler-<fecha>.dump.partial`.
2. `pg_restore --list` para comprobar que el dump es legible.
3. Renombrado atómico a `.dump` y borrado de dumps con más de `retentionDays` días.

Lanzar un backup a mano: ⚠️ `kubectl -n crawler-local create job --from=cronjob/db-backup db-backup-manual`.

El PVC `db-backups` está en el mismo clúster: copiarlo fuera es responsabilidad del operador. Los snapshots HTML del PVC `raw-data` **no** entran en el backup (se pueden regenerar volviendo a crawlear).

### ⚠️ Restaurar

1. Parar a quien escribe: `kubectl -n crawler-local scale deploy/api deploy/worker --replicas=0`.
2. Crear un pod temporal que cumple `restricted`, con el PVC de backups y la etiqueta de cliente de BD:

   ```sh
   kubectl -n crawler-local apply -f - <<'EOF'
   apiVersion: v1
   kind: Pod
   metadata:
     name: db-restore
     labels: { crawler.io/db-client: "true" }
   spec:
     restartPolicy: Never
     automountServiceAccountToken: false
     securityContext:
       runAsNonRoot: true
       runAsUser: 999
       runAsGroup: 999
       fsGroup: 999
       seccompProfile: { type: RuntimeDefault }
     containers:
       - name: restore
         image: postgres:18-trixie
         command: ["sleep", "3600"]
         env:
           - { name: PGHOST, value: db }
           - { name: PGDATABASE, value: crawler }
           - { name: PGUSER, valueFrom: { secretKeyRef: { name: crawler-db, key: POSTGRES_USER } } }
           - { name: PGPASSWORD, valueFrom: { secretKeyRef: { name: crawler-db, key: POSTGRES_PASSWORD } } }
         resources:
           requests: { cpu: 50m, memory: 128Mi }
           limits: { cpu: "1", memory: 512Mi }
         securityContext:
           allowPrivilegeEscalation: false
           readOnlyRootFilesystem: true
           capabilities: { drop: [ALL] }
         volumeMounts:
           - { name: backups, mountPath: /backups, readOnly: true }
           - { name: tmp, mountPath: /tmp }
     volumes:
       - { name: backups, persistentVolumeClaim: { claimName: db-backups } }
       - { name: tmp, emptyDir: { sizeLimit: 256Mi } }
   EOF
   kubectl -n crawler-local wait --for=condition=Ready pod/db-restore
   kubectl -n crawler-local exec db-restore -- ls -l /backups
   ```

3. Restaurar (sustituye la fecha):

   ```sh
   kubectl -n crawler-local exec db-restore -- \
     pg_restore --clean --if-exists --no-owner --no-privileges -d crawler /backups/crawler-20261031T031700Z.dump
   ```

   Para copiar un dump fuera del clúster: `kubectl -n crawler-local cp db-restore:/backups/<fichero>.dump ./<fichero>.dump`.

4. Limpiar y volver a levantar: `kubectl -n crawler-local delete pod db-restore` y ⚠️ `pulumi up` (restaura las réplicas configuradas).

Un dump de una versión de esquema anterior se puede restaurar: el Job de migración del siguiente `pulumi up` solo aplica migraciones pendientes si la etiqueta cambia; si no, ejecuta `kubectl -n crawler-local exec deploy/api -- /crawler migrate up`.

## Rollback

1. Volver a la etiqueta anterior: `pulumi config set --path images.tag <tag-anterior>` y ⚠️ `pulumi up`.
2. Esto **no** revierte el esquema. Las migraciones deben ser compatibles hacia atrás (la versión N-1 del código funciona con el esquema N), por lo que volver atrás la imagen es seguro.
3. Solo si una migración es destructiva y está mal, restaurar un backup o aplicar una migración correctiva nueva. `migrate down-to` fuera de `dev`/`test` exige `CRAWLER_ALLOW_DOWN=yes` y es último recurso.

## Ejemplo de producción (documentado, NO crear desde aquí)

```yaml
# infra/Pulumi.prod.yaml — ejemplo. Se crea con un backend de estado y secretos de producción.
config:
  crawler:env: prod
  crawler:namespace: crawler
  crawler:images:
    backend: ghcr.io/hneyra/crawler-go
    web: ghcr.io/hneyra/crawler-web
    tag: 1.4.2               # worker: ghcr.io/hneyra/crawler-go-worker:1.4.2
    pullSecret: ghcr
  crawler:database:
    mode: external            # proveedor gestionado: backups a su cargo
  crawler:storage: { rawDataSize: 50Gi }
  crawler:replicas: { api: 2, worker: 1, web: 2 }
  crawler:ingress:
    host: crawler.example
    tls:
      certResolver: letsencrypt   # debe existir en la config estática de Traefik (HelmChartConfig)
  crawler:trustedProxies: 10.42.0.0/16
  crawler:crawl: { workerConcurrency: 4 }
# secretos: databaseUrl (sslmode=require o verify-full), kubeconfig
```

Invariantes que la infra impone en `prod`: TLS en el ingress, etiquetas distintas de `latest`/`dev`, backups si la BD es `inCluster`, `databaseUrl` sin `sslmode=disable`, `CRAWLER_COOKIE_SECURE=false` y `CRAWLER_RATE_LIMIT=off` prohibidos, PVC protegidos contra borrado (`protect`: `db-data`, `db-backups` y `raw-data`). La redirección HTTP→HTTPS se configura en Traefik a nivel de clúster.

## Limitaciones conocidas

- Un solo worker (PVC `ReadWriteOnce` + un perfil de Chromium). El paralelismo se ajusta con `crawl.workerConcurrency`.
- El rate limit del API en proceso no se comparte entre réplicas; el middleware de Traefik limita por IP en el borde.
- El backup se guarda en un PVC del mismo clúster: copiarlo fuera es responsabilidad del operador.
- Con `ttlSecondsAfterFinished` el Job de migración desaparece a las 24 h; Pulumi no lo vuelve a crear salvo `refresh`, y si lo hace, `migrate up` es idempotente.
