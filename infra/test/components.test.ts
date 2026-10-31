import { describe, expect, it } from "vitest";
import { MANAGED_ENV, SECRET_ENV, parseSettings, type Settings } from "../config";
import { appConfigData, inClusterDatabaseUrl } from "../components/base";
import { BACKUP_SCRIPT } from "../components/backup";
import { DB_CLIENT_LABEL, type Manifest, type PodSpec, type PodTemplate } from "../components/common";
import { CONFIG_HASH_ANNOTATION, migrateJobName } from "../components/app";
import { type Phases, allManifests, buildPhases } from "../components/stack";
import { BACKEND, WEB, inClusterSecrets, localRaw, localSettings, prodRaw } from "./fixtures";

const secrets = {
  databaseUrl: "postgres://crawler:pw@db:5432/crawler?sslmode=disable",
  dbPassword: "pw-pw-pw-pw-pw-pw",
};

type DeploymentSpec = { replicas: number; strategy: { type: string } };

function find(p: Phases, kind: string, name: string): Manifest {
  const m = allManifests(p).find((x) => x.kind === kind && x.metadata.name === name);
  if (!m) throw new Error(`missing ${kind}/${name}`);
  return m;
}

// pod template of any workload kind.
function podTemplate(m: Manifest): PodTemplate | undefined {
  const spec = m.spec as Record<string, unknown> | undefined;
  if (!spec) return undefined;
  if (m.kind === "CronJob") {
    const jt = spec.jobTemplate as { spec: { template: PodTemplate } };
    return jt.spec.template;
  }
  return spec.template as PodTemplate | undefined;
}

function pod(p: Phases, kind: string, name: string): PodSpec {
  const t = podTemplate(find(p, kind, name));
  if (!t) throw new Error("no template");
  return t.spec;
}

function configData(p: Phases): Record<string, string> {
  return (find(p, "ConfigMap", "crawler-config") as Manifest & { data: Record<string, string> }).data;
}

describe("buildPhases inCluster (local)", () => {
  const s = localSettings();
  const p = buildPhases(s, secrets);
  const all = allManifests(p);

  it("emits expected objects", () => {
    expect(all.map((m) => `${m.kind}/${m.metadata.name}`)).toEqual([
      "Namespace/crawler-local",
      "ConfigMap/crawler-config",
      "Secret/crawler-app",
      "Secret/crawler-db",
      "PersistentVolumeClaim/db-data",
      "NetworkPolicy/db-ingress",
      "Service/db",
      "Deployment/db",
      "PersistentVolumeClaim/db-backups",
      "CronJob/db-backup",
      "PersistentVolumeClaim/raw-data",
      `Job/${migrateJobName(s)}`,
      "Service/api",
      "Deployment/api",
      "Deployment/worker",
      "Service/web",
      "Deployment/web",
      "Middleware/api-ratelimit",
      "IngressRoute/crawler",
    ]);
  });

  it("every namespaced object lives in stack namespace", () => {
    for (const m of all.filter((x) => x.kind !== "Namespace")) expect(m.metadata.namespace).toBe("crawler-local");
  });

  it("namespace enforces restricted pod security", () => {
    expect(p.namespace.metadata.labels?.["pod-security.kubernetes.io/enforce"]).toBe("restricted");
  });

  it("all containers hardened with requests + limits", () => {
    const pods = all.map(podTemplate).filter((t): t is PodTemplate => t !== undefined);
    expect(pods.length).toBe(6); // db, backup, migrate, api, worker, web
    for (const t of pods) {
      expect(t.spec.automountServiceAccountToken).toBe(false);
      expect(t.spec.securityContext.runAsNonRoot).toBe(true);
      expect(t.spec.securityContext.runAsUser).toBeGreaterThan(0);
      expect(t.spec.securityContext.seccompProfile.type).toBe("RuntimeDefault");
      for (const c of t.spec.containers) {
        expect(c.securityContext).toEqual({
          runAsNonRoot: true,
          allowPrivilegeEscalation: false,
          readOnlyRootFilesystem: true,
          capabilities: { drop: ["ALL"] },
          seccompProfile: { type: "RuntimeDefault" },
        });
        expect(c.resources.requests.cpu).toBeTruthy();
        expect(c.resources.requests.memory).toBeTruthy();
        expect(c.resources.limits.cpu).toBeTruthy();
        expect(c.resources.limits.memory).toBeTruthy();
        // rofs -> writable /tmp
        expect(c.volumeMounts?.some((v) => v.mountPath === "/tmp")).toBe(true);
        expect(c.image).not.toMatch(/:latest$/);
      }
    }
  });

  it("every Deployment mounting a pvc uses Recreate (rwo)", () => {
    const withPvc = all.filter(
      (m) => m.kind === "Deployment" && podTemplate(m)?.spec.volumes?.some((v) => v.persistentVolumeClaim),
    );
    expect(withPvc.map((m) => m.metadata.name).sort()).toEqual(["db", "worker"]);
    for (const m of withPvc) expect((m.spec as DeploymentSpec).strategy.type).toBe("Recreate");
    const pvcs = all.filter((m) => m.kind === "PersistentVolumeClaim");
    for (const m of pvcs) expect((m.spec as { accessModes: string[] }).accessModes).toEqual(["ReadWriteOnce"]);
  });

  it("app secret holds secrets only; configmap holds none", () => {
    const sec = find(p, "Secret", "crawler-app") as Manifest & { stringData: Record<string, string> };
    expect(Object.keys(sec.stringData)).toEqual(["CRAWLER_DATABASE_URL"]);
    const cm = configData(p);
    for (const k of SECRET_ENV) expect(cm[k]).toBeUndefined();
    expect(Object.values(cm).some((v) => v.includes("pw-pw"))).toBe(false);
    expect(cm.CRAWLER_PUBLIC_URL).toBe("http://crawler.localhost");
    expect(cm.CRAWLER_HTTP_ADDR).toBe(":8080");
    expect(cm.CRAWLER_METRICS_ADDR).toBe(":9091");
    expect(cm.CRAWLER_TRUSTED_PROXIES).toBe("");
  });

  it("configmap: crawl keys + worker paths", () => {
    expect(configData(p)).toMatchObject({
      CRAWLER_ENV: "dev",
      CRAWLER_RAW_DATA_DIR: "/data/raw",
      CRAWLER_CHROME_PATH: "/usr/bin/chromium",
      CRAWLER_BROWSER_PROFILE_DIR: "/data/profile",
      CRAWLER_WORKER_HEALTH_ADDR: "127.0.0.1:8081",
      CRAWLER_DISCOVERY_MAX_PAGES: "50",
      CRAWLER_EXTRACTION_BATCH_SIZE: "10",
      CRAWLER_RETRY_MAX: "3",
      CRAWLER_RETRY_INTERVAL: "10m",
      CRAWLER_STUCK_AFTER: "30m",
      CRAWLER_WORKER_CONCURRENCY: "2",
    });
    // every managed key is actually set by infra.
    for (const k of MANAGED_ENV) expect(configData(p)[k], k).toBeDefined();
  });

  it("configmap: crawl overrides + appEnv passthrough", () => {
    const raw = localRaw();
    raw.crawl = { discoveryMaxPages: 200, workerConcurrency: 6, stuckAfter: "1h" };
    raw.appEnv = { CRAWLER_FETCH_TIMEOUT: "20s" };
    const d = appConfigData(parseSettings("local", raw, inClusterSecrets));
    expect(d).toMatchObject({
      CRAWLER_DISCOVERY_MAX_PAGES: "200",
      CRAWLER_WORKER_CONCURRENCY: "6",
      CRAWLER_STUCK_AFTER: "1h",
      CRAWLER_RETRY_INTERVAL: "10m",
      CRAWLER_FETCH_TIMEOUT: "20s",
    });
  });

  it("postgres: Recreate, 1 replica, pinned image, pvc, pg_isready probes", () => {
    const d = find(p, "Deployment", "db") as Manifest & { spec: DeploymentSpec };
    expect(d.spec.replicas).toBe(1);
    expect(d.spec.strategy.type).toBe("Recreate");
    const ps = pod(p, "Deployment", "db");
    const c = ps.containers[0]!;
    expect(c.image).toBe("postgres:18-trixie");
    expect(c.readinessProbe?.exec?.command[0]).toBe("pg_isready");
    expect(c.livenessProbe?.exec?.command[0]).toBe("pg_isready");
    expect(ps.volumes?.find((v) => v.name === "data")?.persistentVolumeClaim?.claimName).toBe("db-data");
    expect(ps.securityContext.fsGroup).toBe(999);
    expect(ps.volumes?.find((v) => v.name === "shm")?.emptyDir).toEqual({ medium: "Memory", sizeLimit: "256Mi" });
    expect(c.volumeMounts?.find((m) => m.name === "shm")?.mountPath).toBe("/dev/shm");
    const svc = find(p, "Service", "db") as Manifest & { spec: { type: string } };
    expect(svc.spec.type).toBe("ClusterIP");
    const sec = find(p, "Secret", "crawler-db") as Manifest & { stringData: Record<string, string> };
    expect(sec.stringData).toMatchObject({ POSTGRES_USER: "crawler", POSTGRES_DB: "crawler" });
  });

  it("db network policy admits only db-client pods; clients labelled", () => {
    const np = find(p, "NetworkPolicy", "db-ingress") as Manifest & {
      spec: { ingress: { from: { podSelector: { matchLabels: Record<string, string> } }[] }[] };
    };
    expect(np.spec.ingress[0]!.from[0]!.podSelector.matchLabels).toEqual({ [DB_CLIENT_LABEL]: "true" });
    expect(DB_CLIENT_LABEL).toBe("crawler.io/db-client");
    const clients = all
      .filter((m) => podTemplate(m)?.metadata.labels[DB_CLIENT_LABEL] === "true")
      .map((m) => m.metadata.name);
    expect(clients.sort()).toEqual(["api", "db-backup", migrateJobName(s), "worker"].sort());
  });

  it("backup cronjob: pg_dump -Fc, retention, forbid overlap", () => {
    const cj = find(p, "CronJob", "db-backup") as Manifest & {
      spec: { schedule: string; concurrencyPolicy: string };
    };
    expect(cj.spec.concurrencyPolicy).toBe("Forbid");
    expect(cj.spec.schedule).toBe("17 3 * * *");
    expect(BACKUP_SCRIPT).toContain("pg_dump -Fc");
    expect(BACKUP_SCRIPT).toContain("/backups/crawler-${ts}.dump");
    expect(BACKUP_SCRIPT).toContain('-mtime +"${RETENTION_DAYS}"');
    const c = pod(p, "CronJob", "db-backup").containers[0]!;
    expect(c.env?.find((e) => e.name === "RETENTION_DAYS")?.value).toBe("7");
    expect(c.env?.find((e) => e.name === "PGPASSWORD")?.valueFrom?.secretKeyRef).toEqual({
      name: "crawler-db",
      key: "POSTGRES_PASSWORD",
    });
  });

  it("migrate job: name from image hash, migrate up, bounded retries + ttl", () => {
    const name = migrateJobName(s);
    expect(name).toMatch(/^migrate-[0-9a-f]{10}$/);
    const j = find(p, "Job", name) as Manifest & {
      spec: { backoffLimit: number; ttlSecondsAfterFinished: number };
    };
    expect(j.spec.backoffLimit).toBe(2);
    expect(j.spec.ttlSecondsAfterFinished).toBeGreaterThan(0);
    const ps = pod(p, "Job", name);
    expect(ps.restartPolicy).toBe("Never");
    expect(ps.containers[0]!.args).toEqual(["migrate", "up"]);
    expect(ps.containers[0]!.image).toBe(`${BACKEND}:0.1.0`);
  });

  it("migrate job name changes with tag", () => {
    const raw = localRaw();
    raw.images = { backend: BACKEND, web: "w", tag: "0.1.1" };
    const s2 = parseSettings("local", raw, inClusterSecrets);
    expect(migrateJobName(s2)).not.toBe(migrateJobName(s));
  });

  it("api: probes with 3s timeout, metrics not in service, no /data mount", () => {
    const ps = pod(p, "Deployment", "api");
    const c = ps.containers[0]!;
    expect(c.image).toBe(`${BACKEND}:0.1.0`);
    expect(c.args).toEqual(["serve"]);
    expect(c.livenessProbe).toMatchObject({ httpGet: { path: "/healthz" }, timeoutSeconds: 3 });
    expect(c.readinessProbe).toMatchObject({ httpGet: { path: "/readyz" }, timeoutSeconds: 3 });
    expect(c.ports?.map((x) => x.containerPort)).toEqual([8080, 9091]);
    expect(c.volumeMounts?.map((m) => m.mountPath)).toEqual(["/tmp"]);
    expect(ps.volumes?.some((v) => v.persistentVolumeClaim)).toBe(false);
    const svc = find(p, "Service", "api") as Manifest & { spec: { ports: { port: number }[] } };
    expect(svc.spec.ports.map((x) => x.port)).toEqual([8080]);
  });

  it("worker: chromium image, worker arg, exec probes on loopback, no port exposed", () => {
    const c = pod(p, "Deployment", "worker").containers[0]!;
    expect(c.image).toBe(`${BACKEND}-worker:0.1.0`);
    expect(c.args).toEqual(["worker"]);
    const cmd = ["/crawler", "healthcheck", "http://127.0.0.1:8081/healthz"];
    expect(c.livenessProbe).toMatchObject({ exec: { command: cmd } });
    expect(c.startupProbe).toMatchObject({ exec: { command: cmd } });
    expect(c.livenessProbe?.httpGet).toBeUndefined();
    expect(c.livenessProbe!.timeoutSeconds).toBeGreaterThanOrEqual(4); // healthcheck cmd has 3s client timeout
    expect(c.ports).toBeUndefined();
    expect(c.resources).toEqual({ requests: { cpu: "250m", memory: "512Mi" }, limits: { cpu: "2", memory: "2Gi" } });
  });

  it("worker: Recreate strategy, 1 replica", () => {
    const d = find(p, "Deployment", "worker") as Manifest & { spec: DeploymentSpec };
    expect(d.spec.strategy).toEqual({ type: "Recreate" });
    expect(d.spec.replicas).toBe(1);
  });

  it("worker volumes: /data on raw-data pvc, /dev/shm memory, /tmp emptyDir", () => {
    const ps = pod(p, "Deployment", "worker");
    const c = ps.containers[0]!;
    const mountOf = (path: string) => c.volumeMounts?.find((m) => m.mountPath === path)?.name;
    const vol = (name: string | undefined) => ps.volumes?.find((v) => v.name === name);
    expect(vol(mountOf("/data"))?.persistentVolumeClaim).toEqual({ claimName: "raw-data" });
    expect(vol(mountOf("/dev/shm"))?.emptyDir).toEqual({ medium: "Memory", sizeLimit: "512Mi" });
    expect(vol(mountOf("/tmp"))?.emptyDir).toBeDefined();
    expect(vol(mountOf("/tmp"))?.emptyDir?.medium).toBeUndefined();
    // pvc writable by nonroot uid.
    expect(ps.securityContext).toMatchObject({ runAsUser: 65532, fsGroup: 65532 });
  });

  it("raw-data pvc: size + storage class from storage.*", () => {
    const pv = find(p, "PersistentVolumeClaim", "raw-data") as Manifest & {
      spec: { storageClassName?: string; resources: { requests: { storage: string } } };
    };
    expect(pv.spec.resources.requests.storage).toBe("10Gi");
    expect(pv.spec.storageClassName).toBeUndefined();
    const raw = localRaw();
    raw.storage = { rawDataSize: "40Gi", storageClass: "fast" };
    const p2 = buildPhases(parseSettings("local", raw, inClusterSecrets), secrets);
    const pv2 = find(p2, "PersistentVolumeClaim", "raw-data") as Manifest & {
      spec: { storageClassName?: string; resources: { requests: { storage: string } } };
    };
    expect(pv2.spec).toMatchObject({ storageClassName: "fast", resources: { requests: { storage: "40Gi" } } });
    // db pvc keeps its own class.
    expect((find(p2, "PersistentVolumeClaim", "db-data").spec as { storageClassName?: string }).storageClassName).toBeUndefined();
  });

  it("web: env + healthz probes + numeric uid", () => {
    const ps = pod(p, "Deployment", "web");
    const c = ps.containers[0]!;
    expect(c.image).toBe(`${WEB}:0.1.0`);
    const env = Object.fromEntries((c.env ?? []).map((e) => [e.name, e.value]));
    expect(env).toMatchObject({
      API_INTERNAL_URL: "http://api:8080",
      ORIGIN: "http://crawler.localhost",
      PUBLIC_ORIGIN: "http://crawler.localhost",
      PORT: "3000",
      ADDRESS_HEADER: "X-Forwarded-For",
      XFF_DEPTH: "1",
    });
    expect(c.readinessProbe?.httpGet?.path).toBe("/healthz");
    expect(ps.securityContext.runAsUser).toBe(1000);
  });

  it("config change rolls api + worker", () => {
    const raw = localRaw();
    raw.crawl = { workerConcurrency: 4 };
    const p2 = buildPhases(parseSettings("local", raw, inClusterSecrets), secrets);
    const hash = (x: Phases, n: string) => podTemplate(find(x, "Deployment", n))?.metadata.annotations?.[CONFIG_HASH_ANNOTATION];
    expect(CONFIG_HASH_ANNOTATION).toBe("crawler.io/config-hash");
    expect(hash(p2, "api")).not.toBe(hash(p, "api"));
    expect(hash(p2, "worker")).toBe(hash(p2, "api"));
  });

  it("ingress: /api -> api with rate limit, rest -> web, web entrypoint, no tls", () => {
    const r = find(p, "IngressRoute", "crawler") as Manifest & {
      apiVersion: string;
      spec: {
        entryPoints: string[];
        tls?: unknown;
        routes: { match: string; middlewares?: { name: string }[]; services: { name: string; port: number }[] }[];
      };
    };
    expect(r.apiVersion).toBe("traefik.io/v1alpha1");
    expect(r.spec.entryPoints).toEqual(["web"]);
    expect(r.spec.tls).toBeUndefined();
    expect(r.spec.routes[0]).toEqual({
      kind: "Rule",
      match: "Host(`crawler.localhost`) && (Path(`/api`) || PathPrefix(`/api/`))",
      middlewares: [{ name: "api-ratelimit" }],
      services: [{ name: "api", port: 8080 }],
    });
    expect(r.spec.routes[1]).toEqual({
      kind: "Rule",
      match: "Host(`crawler.localhost`)",
      services: [{ name: "web", port: 3000 }],
    });
    const mw = find(p, "Middleware", "api-ratelimit") as Manifest & { spec: { rateLimit: { average: number } } };
    expect(mw.spec.rateLimit.average).toBe(20);
  });
});

describe("buildPhases prod / external", () => {
  it("tls cert resolver + websecure + https public url", () => {
    const s: Settings = parseSettings("prod", prodRaw(), inClusterSecrets);
    const p = buildPhases(s, secrets);
    const r = find(p, "IngressRoute", "crawler") as Manifest & { spec: { entryPoints: string[]; tls: unknown } };
    expect(r.spec.entryPoints).toEqual(["websecure"]);
    expect(r.spec.tls).toEqual({ certResolver: "letsencrypt" });
    expect(appConfigData(s).CRAWLER_PUBLIC_URL).toBe("https://crawler.example");
    expect(appConfigData(s).CRAWLER_ENV).toBe("prod");
    const env = Object.fromEntries((pod(p, "Deployment", "web").containers[0]!.env ?? []).map((e) => [e.name, e.value]));
    expect(env.ORIGIN).toBe("https://crawler.example");
    // worker stays single + Recreate in prod.
    expect((find(p, "Deployment", "worker").spec as DeploymentSpec).strategy.type).toBe("Recreate");
  });

  it("external db: no postgres objects, raw-data pvc kept, url passed through", () => {
    const raw = localRaw();
    raw.database = { mode: "external", backup: { enabled: false } };
    const s = parseSettings("local", raw, { ...inClusterSecrets, dbPassword: false, databaseUrl: true });
    const url = "postgres://u:p@db.provider.example:5432/crawler?sslmode=require";
    const p = buildPhases(s, { databaseUrl: url });
    expect(p.database.map((m) => `${m.kind}/${m.metadata.name}`)).toEqual(["PersistentVolumeClaim/raw-data"]);
    expect(allManifests(p).some((m) => m.metadata.name === "crawler-db")).toBe(false);
    const sec = find(p, "Secret", "crawler-app") as Manifest & { stringData: Record<string, string> };
    expect(sec.stringData.CRAWLER_DATABASE_URL).toBe(url);
  });

  it("pull secret propagates to pods", () => {
    const raw = localRaw();
    raw.images = { backend: BACKEND, web: WEB, tag: "0.1.0", pullSecret: "ghcr" };
    const p = buildPhases(parseSettings("local", raw, inClusterSecrets), secrets);
    expect(pod(p, "Deployment", "api").imagePullSecrets).toEqual([{ name: "ghcr" }]);
    expect(pod(p, "Deployment", "worker").imagePullSecrets).toEqual([{ name: "ghcr" }]);
  });
});

describe("inClusterDatabaseUrl", () => {
  it("encodes password and targets db service", () => {
    expect(inClusterDatabaseUrl("a/b@c:d")).toBe("postgres://crawler:a%2Fb%40c%3Ad@db:5432/crawler?sslmode=disable");
  });
});
