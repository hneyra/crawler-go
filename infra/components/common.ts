// shared manifest helpers. pure. no pulumi resources here.
import { createHash } from "node:crypto";
import type { ResourceSpec, Settings, Workload } from "../config";

// minimal k8s shapes. enough for our manifests + tests.
export interface Meta {
  name: string;
  namespace?: string;
  labels?: Record<string, string>;
  annotations?: Record<string, string>;
}

export interface Manifest {
  apiVersion: string;
  kind: string;
  metadata: Meta;
  [k: string]: unknown;
}

export interface EnvVar<S = string> {
  name: string;
  value?: string | S;
  valueFrom?: { secretKeyRef?: { name: string; key: string }; configMapKeyRef?: { name: string; key: string } };
}

export interface Probe {
  httpGet?: { path: string; port: number | string };
  exec?: { command: string[] };
  initialDelaySeconds?: number;
  periodSeconds?: number;
  timeoutSeconds?: number;
  failureThreshold?: number;
}

export interface Container {
  name: string;
  image: string;
  imagePullPolicy?: string;
  command?: string[];
  args?: string[];
  env?: EnvVar[];
  envFrom?: { configMapRef?: { name: string }; secretRef?: { name: string } }[];
  ports?: { name: string; containerPort: number; protocol?: string }[];
  resources: ResourceSpec;
  securityContext: ContainerSecurityContext;
  livenessProbe?: Probe;
  readinessProbe?: Probe;
  startupProbe?: Probe;
  volumeMounts?: { name: string; mountPath: string; readOnly?: boolean }[];
}

export interface ContainerSecurityContext {
  runAsNonRoot: true;
  allowPrivilegeEscalation: false;
  readOnlyRootFilesystem: boolean;
  capabilities: { drop: ["ALL"] };
  seccompProfile: { type: "RuntimeDefault" };
}

export interface PodSecurityContext {
  runAsNonRoot: true;
  runAsUser: number;
  runAsGroup: number;
  fsGroup?: number;
  seccompProfile: { type: "RuntimeDefault" };
}

export interface Volume {
  name: string;
  emptyDir?: { medium?: string; sizeLimit?: string };
  persistentVolumeClaim?: { claimName: string };
}

export interface PodSpec {
  automountServiceAccountToken: false;
  enableServiceLinks: false;
  securityContext: PodSecurityContext;
  imagePullSecrets?: { name: string }[];
  restartPolicy?: string;
  terminationGracePeriodSeconds?: number;
  containers: Container[];
  volumes?: Volume[];
}

export interface PodTemplate {
  metadata: { labels: Record<string, string>; annotations?: Record<string, string> };
  spec: PodSpec;
}

export const PART_OF = "crawler";
export const DB_CLIENT_LABEL = "crawler.io/db-client";
export const NONROOT_UID = 65532; // distroless nonroot. worker image: same uid
export const POSTGRES_UID = 999; // postgres user in official image

export const NAMES = {
  appSecret: "crawler-app",
  dbSecret: "crawler-db",
  config: "crawler-config",
  api: "api",
  worker: "worker",
  web: "web",
  db: "db",
  dbData: "db-data",
  backups: "db-backups",
  backup: "db-backup",
  rawData: "raw-data",
  rateLimit: "api-ratelimit",
  route: "crawler",
  dbPolicy: "db-ingress",
} as const;

export function selector(component: string): Record<string, string> {
  return { "app.kubernetes.io/name": PART_OF, "app.kubernetes.io/component": component };
}

export function labels(component: string, extra: Record<string, string> = {}): Record<string, string> {
  return {
    ...selector(component),
    "app.kubernetes.io/part-of": PART_OF,
    "app.kubernetes.io/managed-by": "pulumi",
    ...extra,
  };
}

export function meta(s: Settings, name: string, component: string, annotations?: Record<string, string>): Meta {
  return {
    name,
    namespace: s.namespace,
    labels: labels(component),
    ...(annotations ? { annotations } : {}),
  };
}

export function backendImage(s: Settings): string {
  return `${s.images.backend}:${s.images.tag}`;
}

export function workerImage(s: Settings): string {
  return `${s.images.worker}:${s.images.workerTag}`;
}

export function webImage(s: Settings): string {
  return `${s.images.web}:${s.images.webTag}`;
}

export function shortHash(input: string, n = 10): string {
  return createHash("sha256").update(input).digest("hex").slice(0, n);
}

// hardened container ctx. rofs default on; caller mounts emptyDir for writes.
export function containerSecurity(readOnlyRootFilesystem = true): ContainerSecurityContext {
  return {
    runAsNonRoot: true,
    allowPrivilegeEscalation: false,
    readOnlyRootFilesystem,
    capabilities: { drop: ["ALL"] },
    seccompProfile: { type: "RuntimeDefault" },
  };
}

export function podSecurity(uid: number, fsGroup?: number): PodSecurityContext {
  return {
    runAsNonRoot: true,
    runAsUser: uid,
    runAsGroup: uid,
    ...(fsGroup !== undefined ? { fsGroup } : {}),
    seccompProfile: { type: "RuntimeDefault" },
  };
}

export function resources(s: Settings, w: Workload): ResourceSpec {
  const r = s.resources[w];
  return { requests: { ...r.requests }, limits: { ...r.limits } };
}

// base pod spec. no sa token, no service env links, hardened.
export function podSpec(
  s: Settings,
  id: { uid: number; fsGroup?: number },
  containers: Container[],
  extra: Pick<Partial<PodSpec>, "restartPolicy" | "terminationGracePeriodSeconds" | "volumes"> = {},
): PodSpec {
  return {
    automountServiceAccountToken: false,
    enableServiceLinks: false,
    securityContext: podSecurity(id.uid, id.fsGroup),
    ...(s.images.pullSecret ? { imagePullSecrets: [{ name: s.images.pullSecret }] } : {}),
    ...extra,
    containers,
  };
}

export const TMP_VOLUME: Volume = { name: "tmp", emptyDir: { sizeLimit: "256Mi" } };
export const TMP_MOUNT = { name: "tmp", mountPath: "/tmp" };

// app env: non-secret configmap + secret.
export const APP_ENV_FROM = [{ configMapRef: { name: NAMES.config } }, { secretRef: { name: NAMES.appSecret } }];

// pvc. skipAwait: local-path binds on first consumer (backups pvc waits for cron).
// always RWO -> any Deployment mounting one must use Recreate.
export function pvc(s: Settings, name: string, component: string, size: string, storageClass?: string): Manifest {
  return {
    apiVersion: "v1",
    kind: "PersistentVolumeClaim",
    metadata: meta(s, name, component, { "pulumi.com/skipAwait": "true" }),
    spec: {
      accessModes: ["ReadWriteOnce"],
      ...(storageClass ? { storageClassName: storageClass } : {}),
      resources: { requests: { storage: size } },
    },
  };
}
