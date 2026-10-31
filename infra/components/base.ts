// namespace, secrets, configmap. pure.
import type { Settings } from "../config";
import { type Manifest, NAMES, labels, meta } from "./common";

export function namespace(s: Settings): Manifest {
  return {
    apiVersion: "v1",
    kind: "Namespace",
    metadata: {
      name: s.namespace,
      labels: {
        ...labels("namespace"),
        // pod security admission: all pods must be restricted-compliant.
        "pod-security.kubernetes.io/enforce": "restricted",
        "pod-security.kubernetes.io/enforce-version": "latest",
        "pod-security.kubernetes.io/warn": "restricted",
      },
    },
  };
}

export const DB_USER = "crawler";
export const DB_NAME = "crawler";
export const DB_SERVICE_PORT = 5432;

// worker data pvc mount. snapshots + chromium profile live here.
export const DATA_DIR = "/data";
export const WORKER_HEALTH_ADDR = "127.0.0.1:8081";

// in-cluster url. password url-encoded. plain tcp inside cluster network.
export function inClusterDatabaseUrl(password: string): string {
  return `postgres://${DB_USER}:${encodeURIComponent(password)}@${NAMES.db}:${DB_SERVICE_PORT}/${DB_NAME}?sslmode=disable`;
}

export interface AppSecretValues<S> {
  databaseUrl: S;
}

// app secret. S = string (tests) or Output<string> (deploy).
export function appSecret<S>(s: Settings, v: AppSecretValues<S>): Manifest {
  const data: Record<string, S> = { CRAWLER_DATABASE_URL: v.databaseUrl };
  return {
    apiVersion: "v1",
    kind: "Secret",
    metadata: meta(s, NAMES.appSecret, "app"),
    type: "Opaque",
    stringData: data,
  };
}

// postgres bootstrap creds. inCluster only.
export function dbSecret<S>(s: Settings, password: S): Manifest {
  return {
    apiVersion: "v1",
    kind: "Secret",
    metadata: meta(s, NAMES.dbSecret, "db"),
    type: "Opaque",
    stringData: { POSTGRES_USER: DB_USER, POSTGRES_DB: DB_NAME, POSTGRES_PASSWORD: password },
  };
}

// web ui + api share one host. csrf origin check uses it.
export function publicUrl(s: Settings): string {
  return `${s.ingress.tls ? "https" : "http"}://${s.ingress.host}`;
}

// non-secret env. mirrors internal/platform/config/config.go keys.
export function appConfigData(s: Settings): Record<string, string> {
  const d: Record<string, string> = {
    CRAWLER_ENV: s.env,
    CRAWLER_HTTP_ADDR: ":8080",
    // metrics on pod ip. Service does not expose it; ingress never routes it.
    CRAWLER_METRICS_ADDR: ":9091",
    CRAWLER_PUBLIC_URL: publicUrl(s),
    CRAWLER_TRUSTED_PROXIES: s.trustedProxies.join(","),
    // worker exec probe hits this. pinned here, not left to binary default.
    CRAWLER_WORKER_HEALTH_ADDR: WORKER_HEALTH_ADDR,
    // rofs: writes only under the worker pvc. api never reads snapshots.
    CRAWLER_RAW_DATA_DIR: `${DATA_DIR}/raw`,
    CRAWLER_CHROME_PATH: "/usr/bin/chromium",
    // persistent profile: cloudflare cookies survive restarts.
    CRAWLER_BROWSER_PROFILE_DIR: `${DATA_DIR}/profile`,
    CRAWLER_DISCOVERY_MAX_PAGES: String(s.crawl.discoveryMaxPages),
    CRAWLER_EXTRACTION_BATCH_SIZE: String(s.crawl.extractionBatchSize),
    CRAWLER_RETRY_MAX: String(s.crawl.retryMax),
    CRAWLER_RETRY_INTERVAL: s.crawl.retryInterval,
    CRAWLER_STUCK_AFTER: s.crawl.stuckAfter,
    CRAWLER_WORKER_CONCURRENCY: String(s.crawl.workerConcurrency),
  };
  return { ...d, ...s.appEnv };
}

export function appConfigMap(s: Settings): Manifest {
  return {
    apiVersion: "v1",
    kind: "ConfigMap",
    metadata: meta(s, NAMES.config, "app"),
    data: appConfigData(s),
  };
}
