// config.ts = ONLY pulumi.Config reader. parse + invariants are pure (tested).
import * as pulumi from "@pulumi/pulumi";

export type Env = "dev" | "prod";
export type DbMode = "inCluster" | "external";
export type Workload = "api" | "worker" | "web" | "migrate" | "db" | "backup";

export interface ResourceSpec {
  requests: { cpu: string; memory: string };
  limits: { cpu: string; memory: string };
}

export interface Settings {
  stack: string;
  env: Env;
  namespace: string;
  images: {
    backend: string; // repo, no tag. api + migrate (distroless)
    worker: string; // repo, no tag. binary + chromium. default = `${backend}-worker`
    web: string; // repo, no tag
    tag: string; // backend tag
    workerTag: string; // default = tag
    webTag: string; // default = tag
    webRunAsUser: number; // web image uid. numeric needed for runAsNonRoot.
    pullSecret?: string; // pre-created dockerconfigjson secret (user action)
  };
  database: {
    mode: DbMode;
    storageSize: string;
    storageClass?: string;
    backup: { enabled: boolean; schedule: string; retentionDays: number; storageSize: string };
  };
  // worker data pvc: snapshots (/data/raw) + chromium profile (/data/profile).
  storage: { rawDataSize: string; storageClass?: string };
  replicas: { api: number; worker: number; web: number };
  resources: Record<Workload, ResourceSpec>;
  ingress: {
    host: string;
    tls?: { certResolver?: string; secretName?: string };
    rateLimit: { average: number; burst: number };
  };
  trustedProxies: string[];
  // crawl tuning. defaults = internal/platform/config/config.go.
  crawl: {
    discoveryMaxPages: number;
    extractionBatchSize: number;
    retryMax: number;
    retryInterval: string; // go duration
    stuckAfter: string; // go duration
    workerConcurrency: number;
  };
  appEnv: Record<string, string>; // extra non-secret CRAWLER_* vars
}

// secret values. T = string in tests, pulumi.Output<string> at deploy.
export interface Secrets<T> {
  dbPassword?: T; // inCluster
  databaseUrl?: T; // external
  kubeconfig: T;
}

export type SecretPresence = { [K in keyof Secrets<unknown>]-?: boolean };

// raw = shape of Pulumi.<stack>.yaml values. untrusted until parsed.
export interface RawConfig {
  env?: unknown;
  namespace?: unknown;
  images?: unknown;
  database?: unknown;
  storage?: unknown;
  replicas?: unknown;
  resources?: unknown;
  ingress?: unknown;
  trustedProxies?: unknown;
  crawl?: unknown;
  appEnv?: unknown;
}

export class ConfigError extends Error {
  constructor(public readonly problems: string[]) {
    super(`invalid crawler config:\n  - ${problems.join("\n  - ")}`);
    this.name = "ConfigError";
  }
}

export const DEFAULT_RESOURCES: Record<Workload, ResourceSpec> = {
  api: { requests: { cpu: "100m", memory: "128Mi" }, limits: { cpu: "1", memory: "512Mi" } },
  // chromium. /dev/shm (memory emptyDir) counts against memory limit.
  worker: { requests: { cpu: "250m", memory: "512Mi" }, limits: { cpu: "2", memory: "2Gi" } },
  web: { requests: { cpu: "100m", memory: "128Mi" }, limits: { cpu: "500m", memory: "512Mi" } },
  migrate: { requests: { cpu: "50m", memory: "64Mi" }, limits: { cpu: "500m", memory: "256Mi" } },
  db: { requests: { cpu: "250m", memory: "512Mi" }, limits: { cpu: "2", memory: "2Gi" } },
  backup: { requests: { cpu: "50m", memory: "128Mi" }, limits: { cpu: "1", memory: "512Mi" } },
};

export const DEFAULT_CRAWL: Settings["crawl"] = {
  discoveryMaxPages: 50,
  extractionBatchSize: 10,
  retryMax: 3,
  retryInterval: "10m",
  stuckAfter: "30m",
  workerConcurrency: 2,
};

// keys set by components. appEnv must not shadow them.
export const MANAGED_ENV = new Set([
  "CRAWLER_ENV",
  "CRAWLER_HTTP_ADDR",
  "CRAWLER_METRICS_ADDR",
  "CRAWLER_PUBLIC_URL",
  "CRAWLER_TRUSTED_PROXIES",
  "CRAWLER_RAW_DATA_DIR",
  "CRAWLER_CHROME_PATH",
  "CRAWLER_BROWSER_PROFILE_DIR",
  "CRAWLER_WORKER_HEALTH_ADDR",
  "CRAWLER_DISCOVERY_MAX_PAGES",
  "CRAWLER_EXTRACTION_BATCH_SIZE",
  "CRAWLER_RETRY_MAX",
  "CRAWLER_RETRY_INTERVAL",
  "CRAWLER_STUCK_AFTER",
  "CRAWLER_WORKER_CONCURRENCY",
]);

// secret keys never allowed in configmap.
export const SECRET_ENV = new Set(["CRAWLER_DATABASE_URL", "CRAWLER_ADMIN_PASSWORD"]);

const DNS_LABEL = /^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$/;
const HOSTNAME = /^(?=.{1,253}$)([a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?)(\.[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?)*$/;
const QUANTITY = /^[0-9]+(\.[0-9]+)?(m|Ki|Mi|Gi|Ti|k|M|G|T)?$/;
const TAG = /^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$/;
const CRON_FIELD = /^[0-9*/,-]+$/;
// go time.ParseDuration subset. positive, units required.
const DURATION = /^([0-9]+(\.[0-9]+)?(ns|us|µs|ms|s|m|h))+$/;

type Obj = Record<string, unknown>;

function isObj(v: unknown): v is Obj {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

// small typed accessors. record problem, return default.
class Reader {
  problems: string[] = [];
  fail(msg: string): void {
    this.problems.push(msg);
  }
  obj(v: unknown, path: string): Obj {
    if (v === undefined) return {};
    if (!isObj(v)) {
      this.fail(`${path}: must be object`);
      return {};
    }
    return v;
  }
  str(v: unknown, path: string, def?: string): string {
    if (v === undefined || v === "") {
      if (def === undefined) this.fail(`${path}: required`);
      return def ?? "";
    }
    if (typeof v !== "string") {
      this.fail(`${path}: must be string`);
      return def ?? "";
    }
    return v.trim();
  }
  optStr(v: unknown, path: string): string | undefined {
    if (v === undefined || v === "") return undefined;
    return this.str(v, path);
  }
  int(v: unknown, path: string, def: number, min: number, max: number): number {
    if (v === undefined) return def;
    const n = typeof v === "string" && v.trim() !== "" ? Number(v) : v;
    if (typeof n !== "number" || !Number.isInteger(n) || n < min || n > max) {
      this.fail(`${path}: must be integer ${min}..${max}`);
      return def;
    }
    return n;
  }
  bool(v: unknown, path: string, def: boolean): boolean {
    if (v === undefined) return def;
    if (typeof v === "boolean") return v;
    if (v === "true" || v === "false") return v === "true";
    this.fail(`${path}: must be boolean`);
    return def;
  }
  duration(v: unknown, path: string, def: string): string {
    const s = this.str(v, path, def);
    if (!DURATION.test(s) || !/[1-9]/.test(s)) this.fail(`${path}: bad go duration "${s}" (e.g. 10m, 1h30m)`);
    return s;
  }
}

export function isValidCidr(s: string): boolean {
  const [ip, bits, extra] = s.split("/");
  if (ip === undefined || bits === undefined || extra !== undefined || !/^[0-9]{1,3}$/.test(bits)) return false;
  const b = Number(bits);
  if (ip.includes(":")) return /^[0-9a-fA-F:.]+$/.test(ip) && ip.split("::").length <= 2 && b <= 128;
  const parts = ip.split(".");
  return parts.length === 4 && parts.every((p) => /^[0-9]{1,3}$/.test(p) && Number(p) <= 255) && b <= 32;
}

function parseResources(r: Reader, raw: unknown): Record<Workload, ResourceSpec> {
  const o = r.obj(raw, "resources");
  const out = { ...DEFAULT_RESOURCES };
  for (const [k, v] of Object.entries(o)) {
    if (!(k in DEFAULT_RESOURCES)) {
      r.fail(`resources.${k}: unknown workload`);
      continue;
    }
    const w = k as Workload;
    const spec = r.obj(v, `resources.${w}`);
    const req = r.obj(spec.requests, `resources.${w}.requests`);
    const lim = r.obj(spec.limits, `resources.${w}.limits`);
    const d = DEFAULT_RESOURCES[w];
    const q = (val: unknown, p: string, def: string): string => {
      const s = r.str(val, p, def);
      if (!QUANTITY.test(s)) r.fail(`${p}: bad quantity "${s}"`);
      return s;
    };
    out[w] = {
      requests: {
        cpu: q(req.cpu, `resources.${w}.requests.cpu`, d.requests.cpu),
        memory: q(req.memory, `resources.${w}.requests.memory`, d.requests.memory),
      },
      limits: {
        cpu: q(lim.cpu, `resources.${w}.limits.cpu`, d.limits.cpu),
        memory: q(lim.memory, `resources.${w}.limits.memory`, d.limits.memory),
      },
    };
  }
  return out;
}

function parseCrawl(r: Reader, raw: unknown): Settings["crawl"] {
  const c = r.obj(raw, "crawl");
  const d = DEFAULT_CRAWL;
  // ranges = config.go validate().
  return {
    discoveryMaxPages: r.int(c.discoveryMaxPages, "crawl.discoveryMaxPages", d.discoveryMaxPages, 1, 10000),
    extractionBatchSize: r.int(c.extractionBatchSize, "crawl.extractionBatchSize", d.extractionBatchSize, 1, 1000),
    retryMax: r.int(c.retryMax, "crawl.retryMax", d.retryMax, 0, 100),
    retryInterval: r.duration(c.retryInterval, "crawl.retryInterval", d.retryInterval),
    stuckAfter: r.duration(c.stuckAfter, "crawl.stuckAfter", d.stuckAfter),
    workerConcurrency: r.int(c.workerConcurrency, "crawl.workerConcurrency", d.workerConcurrency, 1, 64),
  };
}

// parseSettings: pure. all problems at once -> ConfigError.
export function parseSettings(stack: string, raw: RawConfig, present: SecretPresence): Settings {
  const r = new Reader();

  const env = r.str(raw.env, "env", "dev");
  if (env !== "dev" && env !== "prod") r.fail(`env: must be dev|prod, got "${env}"`);
  const prod = env === "prod";

  const namespace = r.str(raw.namespace, "namespace");
  if (namespace && !DNS_LABEL.test(namespace)) r.fail(`namespace: not dns label "${namespace}"`);

  const img = r.obj(raw.images, "images");
  const backend = r.str(img.backend, "images.backend");
  const worker = r.str(img.worker, "images.worker", backend ? `${backend}-worker` : "");
  const web = r.str(img.web, "images.web");
  const tag = r.str(img.tag, "images.tag");
  const workerTag = r.str(img.workerTag, "images.workerTag", tag);
  const webTag = r.str(img.webTag, "images.webTag", tag);
  for (const [p, repo] of [["images.backend", backend], ["images.worker", worker], ["images.web", web]] as const) {
    // repo only. tag lives in images.*tag. port in registry host is fine.
    const last = repo.split("/").pop() ?? "";
    if (repo && (last.includes(":") || repo.includes("@"))) r.fail(`${p}: repo without tag/digest (use images.tag)`);
  }
  for (const [p, t] of [["images.tag", tag], ["images.workerTag", workerTag], ["images.webTag", webTag]] as const) {
    if (!t) continue;
    if (!TAG.test(t)) r.fail(`${p}: bad tag "${t}"`);
    if (t === "latest") r.fail(`${p}: "latest" forbidden. pin immutable tag`);
    if (prod && t === "dev") r.fail(`${p}: "dev" forbidden in prod. pin release tag`);
  }
  const webRunAsUser = r.int(img.webRunAsUser, "images.webRunAsUser", 1000, 1, 2147483647);
  const pullSecret = r.optStr(img.pullSecret, "images.pullSecret");

  const db = r.obj(raw.database, "database");
  const mode = r.str(db.mode, "database.mode", "inCluster");
  if (mode !== "inCluster" && mode !== "external") r.fail(`database.mode: must be inCluster|external, got "${mode}"`);
  const storageSize = r.str(db.storageSize, "database.storageSize", "10Gi");
  if (!QUANTITY.test(storageSize)) r.fail(`database.storageSize: bad quantity "${storageSize}"`);
  const storageClass = r.optStr(db.storageClass, "database.storageClass");
  const bk = r.obj(db.backup, "database.backup");
  const backup = {
    enabled: r.bool(bk.enabled, "database.backup.enabled", mode === "inCluster"),
    schedule: r.str(bk.schedule, "database.backup.schedule", "17 3 * * *"),
    retentionDays: r.int(bk.retentionDays, "database.backup.retentionDays", 7, 1, 3650),
    storageSize: r.str(bk.storageSize, "database.backup.storageSize", "10Gi"),
  };
  const fields = backup.schedule.split(/\s+/);
  if (fields.length !== 5 || !fields.every((f) => CRON_FIELD.test(f))) {
    r.fail(`database.backup.schedule: want 5-field cron, got "${backup.schedule}"`);
  }
  if (!QUANTITY.test(backup.storageSize)) r.fail(`database.backup.storageSize: bad quantity "${backup.storageSize}"`);
  if (mode === "external") {
    if (!present.databaseUrl) r.fail("database.mode=external requires secret databaseUrl");
    if (present.dbPassword) r.fail("database.mode=external: dbPassword unused, remove it");
    if (backup.enabled) r.fail("database.backup.enabled: external db backups = provider responsibility. set false");
  }
  if (mode === "inCluster") {
    if (!present.dbPassword) r.fail("database.mode=inCluster requires secret dbPassword");
    if (present.databaseUrl) r.fail("database.mode=inCluster: databaseUrl must not be set (built from dbPassword)");
    if (prod && !backup.enabled) r.fail("database.backup.enabled: prod inCluster db requires backups");
  }

  const st = r.obj(raw.storage, "storage");
  const rawDataSize = r.str(st.rawDataSize, "storage.rawDataSize", "10Gi");
  if (!QUANTITY.test(rawDataSize)) r.fail(`storage.rawDataSize: bad quantity "${rawDataSize}"`);
  const rawDataClass = r.optStr(st.storageClass, "storage.storageClass");

  const rep = r.obj(raw.replicas, "replicas");
  const replicas = {
    api: r.int(rep.api, "replicas.api", prod ? 2 : 1, 1, 20),
    // raw-data pvc is RWO + one chromium profile dir -> single worker. scale via crawl.workerConcurrency.
    worker: r.int(rep.worker, "replicas.worker", 1, 0, 1),
    web: r.int(rep.web, "replicas.web", prod ? 2 : 1, 1, 20),
  };

  const ing = r.obj(raw.ingress, "ingress");
  const host = r.str(ing.host, "ingress.host");
  if (host && !HOSTNAME.test(host)) r.fail(`ingress.host: bad hostname "${host}"`);
  let tls: Settings["ingress"]["tls"];
  if (ing.tls !== undefined) {
    const t = r.obj(ing.tls, "ingress.tls");
    const certResolver = r.optStr(t.certResolver, "ingress.tls.certResolver");
    const secretName = r.optStr(t.secretName, "ingress.tls.secretName");
    if (!certResolver && !secretName) r.fail("ingress.tls: set certResolver or secretName");
    if (certResolver && secretName) r.fail("ingress.tls: certResolver and secretName are exclusive");
    tls = { certResolver, secretName };
  }
  if (prod && !tls) r.fail("ingress.tls: required in prod");
  const rl = r.obj(ing.rateLimit, "ingress.rateLimit");
  const rateLimit = {
    average: r.int(rl.average, "ingress.rateLimit.average", 20, 1, 100000),
    burst: r.int(rl.burst, "ingress.rateLimit.burst", 40, 1, 100000),
  };

  const tp = r.str(raw.trustedProxies, "trustedProxies", "");
  const trustedProxies = tp.split(",").map((s) => s.trim()).filter(Boolean);
  for (const c of trustedProxies) if (!isValidCidr(c)) r.fail(`trustedProxies: bad cidr "${c}"`);

  const crawl = parseCrawl(r, raw.crawl);

  const ae = r.obj(raw.appEnv, "appEnv");
  const appEnv: Record<string, string> = {};
  for (const [k, v] of Object.entries(ae)) {
    if (!/^CRAWLER_[A-Z0-9_]+$/.test(k)) r.fail(`appEnv.${k}: only CRAWLER_* keys`);
    else if (SECRET_ENV.has(k)) r.fail(`appEnv.${k}: secret. use pulumi secret config`);
    else if (MANAGED_ENV.has(k)) r.fail(`appEnv.${k}: managed by infra config, set the typed key`);
    else if (typeof v !== "string" && typeof v !== "number" && typeof v !== "boolean") r.fail(`appEnv.${k}: scalar only`);
    else appEnv[k] = String(v);
  }
  // no display in pod. headful chromium cannot start.
  if (appEnv.CRAWLER_HEADLESS === "false") r.fail("appEnv.CRAWLER_HEADLESS: must stay true (no display in pod)");
  if (prod && appEnv.CRAWLER_COOKIE_SECURE === "false") r.fail("appEnv.CRAWLER_COOKIE_SECURE: must be true in prod");
  if (prod && appEnv.CRAWLER_RATE_LIMIT === "off") r.fail("appEnv.CRAWLER_RATE_LIMIT: off forbidden in prod");

  if (!present.kubeconfig) r.fail("secret kubeconfig: required (explicit provider, never ambient context)");

  const resources = parseResources(r, raw.resources);

  if (r.problems.length > 0) throw new ConfigError(r.problems);
  return {
    stack,
    env: env as Env,
    namespace,
    images: { backend, worker, web, tag, workerTag, webTag, webRunAsUser, ...(pullSecret ? { pullSecret } : {}) },
    database: { mode: mode as DbMode, storageSize, ...(storageClass ? { storageClass } : {}), backup },
    storage: { rawDataSize, ...(rawDataClass ? { storageClass: rawDataClass } : {}) },
    replicas,
    resources,
    ingress: { host, ...(tls ? { tls } : {}), rateLimit },
    trustedProxies,
    crawl,
    appEnv,
  };
}

// --- secret value checks. run inside Output.apply at deploy, plain in tests.

export function checkDatabaseUrl(v: string, env: Env): string {
  const s = v.trim();
  if (!/^postgres(ql)?:\/\/[^\s]+$/.test(s)) throw new ConfigError(["secret databaseUrl: want postgres://..."]);
  if (env === "prod" && /[?&]sslmode=disable\b/.test(s)) {
    throw new ConfigError(["secret databaseUrl: sslmode=disable forbidden in prod (external db over network)"]);
  }
  return s;
}

export function checkDbPassword(v: string): string {
  if (v.length < 16) throw new ConfigError(["secret dbPassword: min 16 chars (openssl rand -base64 24)"]);
  return v;
}

// kubeconfigServers: server urls in kubeconfig text. no yaml dep, no creds touched.
export function kubeconfigServers(text: string): string[] {
  const out: string[] = [];
  for (const line of text.split(/\r?\n/)) {
    const m = /^\s*server:\s*["']?([^"'\s]+)["']?\s*$/.exec(line);
    if (m?.[1]) out.push(m[1]);
  }
  return out;
}

// local stack guard: every server must be loopback. blocks prod kubeconfig by mistake.
export function assertLoopbackKubeconfig(text: string): string {
  const servers = kubeconfigServers(text);
  if (servers.length === 0) throw new ConfigError(["secret kubeconfig: no server entry"]);
  for (const s of servers) {
    let host: string;
    try {
      host = new URL(s).hostname;
    } catch {
      throw new ConfigError(["secret kubeconfig: bad server url"]);
    }
    if (!(host === "127.0.0.1" || host === "localhost" || host === "[::1]" || host === "::1")) {
      throw new ConfigError([`secret kubeconfig: local stack needs loopback server, got host "${host}"`]);
    }
  }
  return text;
}

// --- pulumi reader. only place touching pulumi.Config.

export function loadConfig(): { settings: Settings; secrets: Secrets<pulumi.Output<string>> } {
  const cfg = new pulumi.Config("crawler");
  const raw: RawConfig = {
    env: cfg.get("env"),
    namespace: cfg.get("namespace"),
    images: cfg.getObject("images"),
    database: cfg.getObject("database"),
    storage: cfg.getObject("storage"),
    replicas: cfg.getObject("replicas"),
    resources: cfg.getObject("resources"),
    ingress: cfg.getObject("ingress"),
    trustedProxies: cfg.get("trustedProxies"),
    crawl: cfg.getObject("crawl"),
    appEnv: cfg.getObject("appEnv"),
  };
  const s = {
    dbPassword: cfg.getSecret("dbPassword"),
    databaseUrl: cfg.getSecret("databaseUrl"),
    kubeconfig: cfg.getSecret("kubeconfig"),
  };
  const present: SecretPresence = {
    dbPassword: s.dbPassword !== undefined,
    databaseUrl: s.databaseUrl !== undefined,
    kubeconfig: s.kubeconfig !== undefined,
  };
  const settings = parseSettings(pulumi.getStack(), raw, present);
  // presence checked above -> non-null safe.
  const secrets: Secrets<pulumi.Output<string>> = {
    kubeconfig: s.kubeconfig!.apply((v) => (settings.stack === "local" ? assertLoopbackKubeconfig(v) : v)),
    ...(s.dbPassword ? { dbPassword: s.dbPassword.apply(checkDbPassword) } : {}),
    ...(s.databaseUrl ? { databaseUrl: s.databaseUrl.apply((v) => checkDatabaseUrl(v, settings.env)) } : {}),
  };
  return { settings, secrets };
}
