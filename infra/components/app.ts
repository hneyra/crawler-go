// migrate job, api, worker, web, worker data pvc. pure.
import type { Settings } from "../config";
import { DATA_DIR, WORKER_HEALTH_ADDR, publicUrl } from "./base";
import {
  APP_ENV_FROM,
  type Container,
  DB_CLIENT_LABEL,
  type Manifest,
  NAMES,
  NONROOT_UID,
  type PodSpec,
  TMP_MOUNT,
  TMP_VOLUME,
  type Volume,
  backendImage,
  containerSecurity,
  labels,
  meta,
  podSpec,
  pvc,
  resources,
  selector,
  shortHash,
  webImage,
  workerImage,
} from "./common";

const API_PORT = 8080;
const METRICS_PORT = 9091;
const WEB_PORT = 3000;

export const CONFIG_HASH_ANNOTATION = "crawler.io/config-hash";

function backendContainer(s: Settings, name: string, args: string[], w: "api" | "worker" | "migrate"): Container {
  return {
    name,
    image: backendImage(s),
    imagePullPolicy: "IfNotPresent",
    args,
    envFrom: APP_ENV_FROM,
    resources: resources(s, w),
    securityContext: containerSecurity(true),
    volumeMounts: [TMP_MOUNT],
  };
}

function versionLabel(tag: string): Record<string, string> {
  // label value max 63, [A-Za-z0-9_.-]. tags already match TAG regex.
  return { "app.kubernetes.io/version": tag.slice(0, 63) };
}

// job name = image hash -> new tag = new job. same tag = no rerun.
export function migrateJobName(s: Settings): string {
  return `migrate-${shortHash(backendImage(s))}`;
}

export function migrateJob(s: Settings): Manifest {
  return {
    apiVersion: "batch/v1",
    kind: "Job",
    metadata: { ...meta(s, migrateJobName(s), "migrate"), labels: labels("migrate", versionLabel(s.images.tag)) },
    spec: {
      backoffLimit: 2,
      activeDeadlineSeconds: 900,
      ttlSecondsAfterFinished: 86400,
      template: {
        metadata: { labels: labels("migrate", { [DB_CLIENT_LABEL]: "true", ...versionLabel(s.images.tag) }) },
        spec: podSpec(s, { uid: NONROOT_UID }, [backendContainer(s, "migrate", ["migrate", "up"], "migrate")], {
          restartPolicy: "Never",
          volumes: [TMP_VOLUME],
        }),
      },
    },
  };
}

// RollingUpdate: zero downtime. Recreate: rwo pvc, never two writers.
type Strategy = { type: "RollingUpdate"; rollingUpdate: { maxUnavailable: number; maxSurge: number } } | { type: "Recreate" };

const ROLLING: Strategy = { type: "RollingUpdate", rollingUpdate: { maxUnavailable: 0, maxSurge: 1 } };

function deployment(
  s: Settings,
  component: string,
  replicas: number,
  podLabels: Record<string, string>,
  spec: PodSpec,
  annotations: Record<string, string> = {},
  strategy: Strategy = ROLLING,
): Manifest {
  return {
    apiVersion: "apps/v1",
    kind: "Deployment",
    metadata: meta(s, component, component),
    spec: {
      replicas,
      revisionHistoryLimit: 5,
      strategy,
      selector: { matchLabels: selector(component) },
      template: { metadata: { labels: podLabels, annotations }, spec },
    },
  };
}

// configHash: pod annotation. configmap change -> rollout. secrets: see deploy-k3s.md.
export function apiDeployment(s: Settings, configHash: string): Manifest {
  const c: Container = {
    ...backendContainer(s, "api", ["serve"], "api"),
    ports: [
      { name: "http", containerPort: API_PORT, protocol: "TCP" },
      { name: "metrics", containerPort: METRICS_PORT, protocol: "TCP" },
    ],
    livenessProbe: { httpGet: { path: "/healthz", port: "http" }, periodSeconds: 10, timeoutSeconds: 3, failureThreshold: 3 },
    readinessProbe: { httpGet: { path: "/readyz", port: "http" }, periodSeconds: 5, timeoutSeconds: 3, failureThreshold: 3 },
    startupProbe: { httpGet: { path: "/healthz", port: "http" }, periodSeconds: 2, timeoutSeconds: 3, failureThreshold: 30 },
  };
  // no /data mount: api never reads snapshots.
  return deployment(
    s,
    NAMES.api,
    s.replicas.api,
    labels("api", { [DB_CLIENT_LABEL]: "true", ...versionLabel(s.images.tag) }),
    podSpec(s, { uid: NONROOT_UID }, [c], { terminationGracePeriodSeconds: 30, volumes: [TMP_VOLUME] }),
    { [CONFIG_HASH_ANNOTATION]: configHash },
  );
}

// metrics port deliberately absent. scrape via pod ip only.
export function apiService(s: Settings): Manifest {
  return {
    apiVersion: "v1",
    kind: "Service",
    metadata: meta(s, NAMES.api, "api"),
    spec: {
      type: "ClusterIP",
      selector: selector("api"),
      ports: [{ name: "http", port: API_PORT, targetPort: "http", protocol: "TCP" }],
    },
  };
}

// worker data: snapshots + chromium profile. rwo. protected in prod (index.ts).
export function rawDataPvc(s: Settings): Manifest {
  return pvc(s, NAMES.rawData, "worker", s.storage.rawDataSize, s.storage.storageClass);
}

// worker: /healthz on loopback only (CRAWLER_WORKER_HEALTH_ADDR). kubelet httpGet hits pod ip,
// so probe = exec of the binary's own loopback-only healthcheck. 503 when runner loop stuck.
export const WORKER_HEALTH_CMD = ["/crawler", "healthcheck", `http://${WORKER_HEALTH_ADDR}/healthz`];

// chromium: big tmp + /dev/shm (runtime default 64MB -> renderer crashes). ram-backed, counts vs memory limit.
const WORKER_TMP: Volume = { name: "tmp", emptyDir: { sizeLimit: "1Gi" } };
const WORKER_SHM: Volume = { name: "shm", emptyDir: { medium: "Memory", sizeLimit: "512Mi" } };

export function workerDeployment(s: Settings, configHash: string): Manifest {
  const c: Container = {
    ...backendContainer(s, "worker", ["worker"], "worker"),
    // binary + chromium. image HOME=/data -> chromium caches land on pvc, not rofs.
    image: workerImage(s),
    startupProbe: { exec: { command: WORKER_HEALTH_CMD }, periodSeconds: 5, timeoutSeconds: 5, failureThreshold: 24 },
    livenessProbe: { exec: { command: WORKER_HEALTH_CMD }, periodSeconds: 15, timeoutSeconds: 5, failureThreshold: 4 },
    volumeMounts: [TMP_MOUNT, { name: "data", mountPath: DATA_DIR }, { name: "shm", mountPath: "/dev/shm" }],
  };
  return deployment(
    s,
    NAMES.worker,
    s.replicas.worker,
    labels("worker", { [DB_CLIENT_LABEL]: "true", ...versionLabel(s.images.workerTag) }),
    // fsGroup: pvc writable by uid 65532.
    podSpec(s, { uid: NONROOT_UID, fsGroup: NONROOT_UID }, [c], {
      terminationGracePeriodSeconds: 30,
      volumes: [WORKER_TMP, { name: "data", persistentVolumeClaim: { claimName: NAMES.rawData } }, WORKER_SHM],
    }),
    { [CONFIG_HASH_ANNOTATION]: configHash },
    // rwo pvc + single chromium profile. old pod must stop before new one mounts.
    { type: "Recreate" },
  );
}

export function webDeployment(s: Settings): Manifest {
  const origin = publicUrl(s);
  const c: Container = {
    name: "web",
    image: webImage(s),
    imagePullPolicy: "IfNotPresent",
    env: [
      { name: "API_INTERNAL_URL", value: `http://${NAMES.api}:${API_PORT}` },
      // kit csrf check. must equal CRAWLER_PUBLIC_URL.
      { name: "ORIGIN", value: origin },
      { name: "PUBLIC_ORIGIN", value: origin },
      { name: "PORT", value: String(WEB_PORT) },
      // traefik in front: client ip = last xff hop.
      { name: "ADDRESS_HEADER", value: "X-Forwarded-For" },
      { name: "XFF_DEPTH", value: "1" },
      { name: "NODE_ENV", value: "production" },
    ],
    ports: [{ name: "http", containerPort: WEB_PORT, protocol: "TCP" }],
    resources: resources(s, "web"),
    securityContext: containerSecurity(true),
    livenessProbe: { httpGet: { path: "/healthz", port: "http" }, periodSeconds: 10, timeoutSeconds: 3, failureThreshold: 3 },
    readinessProbe: { httpGet: { path: "/healthz", port: "http" }, periodSeconds: 5, timeoutSeconds: 3, failureThreshold: 3 },
    volumeMounts: [TMP_MOUNT],
  };
  return deployment(
    s,
    NAMES.web,
    s.replicas.web,
    labels("web", versionLabel(s.images.webTag)),
    podSpec(s, { uid: s.images.webRunAsUser }, [c], { terminationGracePeriodSeconds: 30, volumes: [TMP_VOLUME] }),
  );
}

export function webService(s: Settings): Manifest {
  return {
    apiVersion: "v1",
    kind: "Service",
    metadata: meta(s, NAMES.web, "web"),
    spec: {
      type: "ClusterIP",
      selector: selector("web"),
      ports: [{ name: "http", port: WEB_PORT, targetPort: "http", protocol: "TCP" }],
    },
  };
}

export const PORTS = { api: API_PORT, metrics: METRICS_PORT, web: WEB_PORT } as const;
