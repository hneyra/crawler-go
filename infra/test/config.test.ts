import { describe, expect, it } from "vitest";
import {
  ConfigError,
  DEFAULT_CRAWL,
  DEFAULT_RESOURCES,
  type RawConfig,
  assertLoopbackKubeconfig,
  checkDatabaseUrl,
  checkDbPassword,
  isValidCidr,
  kubeconfigServers,
  parseSettings,
} from "../config";
import { BACKEND, WEB, inClusterSecrets, localRaw, prodRaw } from "./fixtures";

function problems(raw: RawConfig, present = inClusterSecrets, stack = "local"): string[] {
  try {
    parseSettings(stack, raw, present);
  } catch (e) {
    if (e instanceof ConfigError) return e.problems;
    throw e;
  }
  return [];
}

describe("parseSettings", () => {
  it("accepts local stack with defaults", () => {
    const s = parseSettings("local", localRaw(), inClusterSecrets);
    expect(s.env).toBe("dev");
    expect(s.database.mode).toBe("inCluster");
    expect(s.database.backup.enabled).toBe(true);
    expect(s.images.webTag).toBe("0.1.0");
    expect(s.replicas).toEqual({ api: 1, worker: 1, web: 1 });
    expect(s.resources).toEqual(DEFAULT_RESOURCES);
    expect(s.ingress.tls).toBeUndefined();
    expect(s.storage).toEqual({ rawDataSize: "10Gi" });
    expect(s.crawl).toEqual(DEFAULT_CRAWL);
  });

  it("crawl defaults mirror config.go", () => {
    expect(DEFAULT_CRAWL).toEqual({
      discoveryMaxPages: 50,
      extractionBatchSize: 10,
      retryMax: 3,
      retryInterval: "10m",
      stuckAfter: "30m",
      workerConcurrency: 2,
    });
  });

  it("worker defaults: bigger resources, image = backend-worker, same tag", () => {
    const s = parseSettings("local", localRaw(), inClusterSecrets);
    expect(s.resources.worker).toEqual({ requests: { cpu: "250m", memory: "512Mi" }, limits: { cpu: "2", memory: "2Gi" } });
    expect(s.images.worker).toBe(`${BACKEND}-worker`);
    expect(s.images.workerTag).toBe("0.1.0");
  });

  it("worker image + tag overridable", () => {
    const raw = localRaw();
    raw.images = { backend: BACKEND, worker: "r/chromium-worker", web: WEB, tag: "0.1.0", workerTag: "0.1.0-c1" };
    const s = parseSettings("local", raw, inClusterSecrets);
    expect(s.images.worker).toBe("r/chromium-worker");
    expect(s.images.workerTag).toBe("0.1.0-c1");
  });

  it("accepts prod example", () => {
    const s = parseSettings("prod", prodRaw(), inClusterSecrets);
    expect(s.replicas.api).toBe(2);
    expect(s.replicas.worker).toBe(1);
    expect(s.ingress.tls).toEqual({ certResolver: "letsencrypt", secretName: undefined });
  });

  it("reports all problems at once", () => {
    const p = problems({});
    expect(p).toEqual(
      expect.arrayContaining([
        "namespace: required",
        "images.backend: required",
        "images.web: required",
        "images.tag: required",
        "ingress.host: required",
      ]),
    );
  });

  it("forbids latest tag everywhere", () => {
    const raw = localRaw();
    raw.images = { backend: "r/b", web: "r/w", tag: "1.0", workerTag: "latest" };
    expect(problems(raw)).toContain('images.workerTag: "latest" forbidden. pin immutable tag');
    raw.images = { backend: "r/b", web: "r/w", tag: "latest" };
    expect(problems(raw)).toEqual(
      expect.arrayContaining([
        'images.tag: "latest" forbidden. pin immutable tag',
        'images.workerTag: "latest" forbidden. pin immutable tag',
        'images.webTag: "latest" forbidden. pin immutable tag',
      ]),
    );
  });

  it("forbids tag inside repo", () => {
    const raw = localRaw();
    raw.images = { backend: "r/b:1.0", worker: "r/wk@sha256:abc", web: "localhost:5000/w", tag: "1.0" };
    const p = problems(raw);
    expect(p).toContain("images.backend: repo without tag/digest (use images.tag)");
    expect(p).toContain("images.worker: repo without tag/digest (use images.tag)");
    expect(p.some((x) => x.startsWith("images.web"))).toBe(false);
  });

  it("external mode requires databaseUrl, rejects dbPassword and backups", () => {
    const raw = localRaw();
    raw.database = { mode: "external", backup: { enabled: true } };
    const p = problems(raw);
    expect(p).toContain("database.mode=external requires secret databaseUrl");
    expect(p).toContain("database.mode=external: dbPassword unused, remove it");
    expect(p).toContain("database.backup.enabled: external db backups = provider responsibility. set false");
  });

  it("external mode ok with url; backups default off", () => {
    const raw = localRaw();
    raw.database = { mode: "external" };
    const s = parseSettings("local", raw, { ...inClusterSecrets, dbPassword: false, databaseUrl: true });
    expect(s.database.mode).toBe("external");
    expect(s.database.backup.enabled).toBe(false);
  });

  it("inCluster requires dbPassword, rejects databaseUrl", () => {
    expect(problems(localRaw(), { ...inClusterSecrets, dbPassword: false })).toContain(
      "database.mode=inCluster requires secret dbPassword",
    );
    expect(problems(localRaw(), { ...inClusterSecrets, databaseUrl: true })).toContain(
      "database.mode=inCluster: databaseUrl must not be set (built from dbPassword)",
    );
  });

  it("prod requires tls + release tag", () => {
    const raw = prodRaw();
    raw.ingress = { host: "crawler.example" };
    raw.images = { backend: "r/b", web: "r/w", tag: "dev" };
    const p = problems(raw, inClusterSecrets, "prod");
    expect(p).toEqual(
      expect.arrayContaining([
        "ingress.tls: required in prod",
        'images.tag: "dev" forbidden in prod. pin release tag',
        'images.workerTag: "dev" forbidden in prod. pin release tag',
      ]),
    );
  });

  it("prod inCluster requires backups", () => {
    const raw = prodRaw();
    raw.database = { mode: "inCluster", backup: { enabled: false } };
    expect(problems(raw)).toContain("database.backup.enabled: prod inCluster db requires backups");
  });

  it("tls needs exactly one of certResolver/secretName", () => {
    const raw = localRaw();
    raw.ingress = { host: "crawler.localhost", tls: {} };
    expect(problems(raw)).toContain("ingress.tls: set certResolver or secretName");
    raw.ingress = { host: "crawler.localhost", tls: { certResolver: "le", secretName: "x" } };
    expect(problems(raw)).toContain("ingress.tls: certResolver and secretName are exclusive");
  });

  it("validates namespace, host, cron, quantities, cidrs", () => {
    const raw: RawConfig = {
      ...localRaw(),
      namespace: "Crawler_Local",
      ingress: { host: "bad host" },
      trustedProxies: "10.42.0.0/16, 300.1.1.1/8",
      database: { mode: "inCluster", storageSize: "ten", backup: { schedule: "daily" } },
      storage: { rawDataSize: "big" },
      resources: { api: { limits: { memory: "lots" } }, nope: {} },
    };
    const p = problems(raw);
    expect(p).toEqual(
      expect.arrayContaining([
        'namespace: not dns label "Crawler_Local"',
        'ingress.host: bad hostname "bad host"',
        'trustedProxies: bad cidr "300.1.1.1/8"',
        'database.storageSize: bad quantity "ten"',
        'database.backup.schedule: want 5-field cron, got "daily"',
        'storage.rawDataSize: bad quantity "big"',
        'resources.api.limits.memory: bad quantity "lots"',
        "resources.nope: unknown workload",
      ]),
    );
  });

  it("storage: size + optional class", () => {
    const raw = localRaw();
    raw.storage = { rawDataSize: "50Gi", storageClass: "local-path" };
    expect(parseSettings("local", raw, inClusterSecrets).storage).toEqual({ rawDataSize: "50Gi", storageClass: "local-path" });
  });

  it("worker replicas capped at 1 (rwo pvc, one chromium profile)", () => {
    const raw = localRaw();
    raw.replicas = { worker: 2 };
    expect(problems(raw)).toContain("replicas.worker: must be integer 0..1");
    raw.replicas = { worker: 0 };
    expect(parseSettings("local", raw, inClusterSecrets).replicas.worker).toBe(0);
  });

  it("validates crawl block ranges + durations", () => {
    const raw = localRaw();
    raw.crawl = {
      discoveryMaxPages: 0,
      extractionBatchSize: 1001,
      retryMax: -1,
      retryInterval: "10 minutes",
      stuckAfter: "0s",
      workerConcurrency: 65,
    };
    expect(problems(raw)).toEqual([
      "crawl.discoveryMaxPages: must be integer 1..10000",
      "crawl.extractionBatchSize: must be integer 1..1000",
      "crawl.retryMax: must be integer 0..100",
      'crawl.retryInterval: bad go duration "10 minutes" (e.g. 10m, 1h30m)',
      'crawl.stuckAfter: bad go duration "0s" (e.g. 10m, 1h30m)',
      "crawl.workerConcurrency: must be integer 1..64",
    ]);
  });

  it("crawl accepts overrides incl. compound durations + string ints", () => {
    const raw = localRaw();
    raw.crawl = { retryInterval: "1h30m", stuckAfter: "45m", workerConcurrency: "8", retryMax: 0 };
    const s = parseSettings("local", raw, inClusterSecrets);
    expect(s.crawl).toMatchObject({ retryInterval: "1h30m", stuckAfter: "45m", workerConcurrency: 8, retryMax: 0 });
  });

  it("appEnv: only non-secret, non-managed CRAWLER_* keys", () => {
    const raw = localRaw();
    raw.appEnv = {
      CRAWLER_FETCH_TIMEOUT: "20s",
      CRAWLER_DB_MAX_CONNS: 8,
      CRAWLER_DATABASE_URL: "x",
      CRAWLER_ADMIN_PASSWORD: "x",
      CRAWLER_HTTP_ADDR: ":1",
      CRAWLER_WORKER_CONCURRENCY: 4,
      CRAWLER_RAW_DATA_DIR: "/tmp/raw",
      PATH: "/bin",
    };
    const p = problems(raw);
    expect(p).toEqual([
      "appEnv.CRAWLER_DATABASE_URL: secret. use pulumi secret config",
      "appEnv.CRAWLER_ADMIN_PASSWORD: secret. use pulumi secret config",
      "appEnv.CRAWLER_HTTP_ADDR: managed by infra config, set the typed key",
      "appEnv.CRAWLER_WORKER_CONCURRENCY: managed by infra config, set the typed key",
      "appEnv.CRAWLER_RAW_DATA_DIR: managed by infra config, set the typed key",
      "appEnv.PATH: only CRAWLER_* keys",
    ]);
  });

  it("headful chromium forbidden; prod forbids insecure cookie + rate limit off", () => {
    const local = localRaw();
    local.appEnv = { CRAWLER_HEADLESS: false };
    expect(problems(local)).toContain("appEnv.CRAWLER_HEADLESS: must stay true (no display in pod)");
    const raw = prodRaw();
    raw.appEnv = { CRAWLER_COOKIE_SECURE: "false", CRAWLER_RATE_LIMIT: "off" };
    const p = problems(raw);
    expect(p).toContain("appEnv.CRAWLER_COOKIE_SECURE: must be true in prod");
    expect(p).toContain("appEnv.CRAWLER_RATE_LIMIT: off forbidden in prod");
  });

  it("requires kubeconfig secret", () => {
    const p = problems(localRaw(), { ...inClusterSecrets, kubeconfig: false });
    expect(p).toEqual(["secret kubeconfig: required (explicit provider, never ambient context)"]);
  });

  it("accepts string numbers and booleans from cli config", () => {
    const raw = localRaw();
    raw.replicas = { api: "3" };
    raw.database = { mode: "inCluster", backup: { enabled: "false", retentionDays: "14" } };
    const s = parseSettings("local", raw, inClusterSecrets);
    expect(s.replicas.api).toBe(3);
    expect(s.database.backup).toMatchObject({ enabled: false, retentionDays: 14 });
  });
});

describe("secret checks", () => {
  it("checkDatabaseUrl", () => {
    expect(checkDatabaseUrl("postgres://u:p@h:5432/d?sslmode=disable", "dev")).toContain("postgres://");
    expect(() => checkDatabaseUrl("mysql://x", "dev")).toThrow(ConfigError);
    expect(() => checkDatabaseUrl("postgres://u:p@h/d?sslmode=disable", "prod")).toThrow(/sslmode=disable/);
    expect(checkDatabaseUrl("postgresql://u:p@h/d?sslmode=verify-full", "prod")).toBeTruthy();
  });

  it("checkDbPassword min length", () => {
    expect(() => checkDbPassword("short")).toThrow(ConfigError);
    expect(checkDbPassword("x".repeat(16))).toHaveLength(16);
  });
});

describe("kubeconfig guard", () => {
  const kc = (server: string) => `apiVersion: v1\nclusters:\n- cluster:\n    certificate-authority-data: AAA\n    server: ${server}\n  name: default\n`;

  it("extracts servers", () => {
    expect(kubeconfigServers(kc("https://127.0.0.1:6443"))).toEqual(["https://127.0.0.1:6443"]);
    expect(kubeconfigServers(kc('"https://localhost:6443"'))).toEqual(["https://localhost:6443"]);
  });

  it("accepts loopback only", () => {
    expect(() => assertLoopbackKubeconfig(kc("https://127.0.0.1:6443"))).not.toThrow();
    expect(() => assertLoopbackKubeconfig(kc("https://localhost:6443"))).not.toThrow();
    expect(() => assertLoopbackKubeconfig(kc("https://k8s.prod.example:6443"))).toThrow(/loopback/);
    expect(() => assertLoopbackKubeconfig(kc("https://10.0.0.5:6443"))).toThrow(/loopback/);
    expect(() => assertLoopbackKubeconfig("apiVersion: v1\n")).toThrow(/no server/);
  });

  it("error never echoes kubeconfig content", () => {
    try {
      assertLoopbackKubeconfig(kc("https://k8s.prod.example:6443"));
    } catch (e) {
      expect(String(e)).not.toContain("certificate-authority-data");
    }
  });
});

describe("isValidCidr", () => {
  it.each([
    ["10.42.0.0/16", true],
    ["0.0.0.0/0", true],
    ["fd00::/8", true],
    ["10.0.0.0", false],
    ["10.0.0.0/33", false],
    ["1.2.3/8", false],
  ])("%s -> %s", (c, ok) => {
    expect(isValidCidr(c)).toBe(ok);
  });
});
