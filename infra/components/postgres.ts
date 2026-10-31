// in-cluster postgres. single replica, Recreate, pvc. pure.
import type { Settings } from "../config";
import { DB_NAME, DB_SERVICE_PORT, DB_USER } from "./base";
import {
  DB_CLIENT_LABEL,
  type Manifest,
  NAMES,
  POSTGRES_UID,
  TMP_MOUNT,
  TMP_VOLUME,
  containerSecurity,
  labels,
  meta,
  podSpec,
  pvc,
  resources,
  selector,
} from "./common";

// debian variant: uid 999 (alpine = 70). no pgvector needed.
export const POSTGRES_IMAGE = "postgres:18-trixie";

const isReady = ["pg_isready", "-h", "127.0.0.1", "-U", DB_USER, "-d", DB_NAME];

export function dbPvc(s: Settings): Manifest {
  return pvc(s, NAMES.dbData, "db", s.database.storageSize, s.database.storageClass);
}

export function dbDeployment(s: Settings): Manifest {
  return {
    apiVersion: "apps/v1",
    kind: "Deployment",
    metadata: meta(s, NAMES.db, "db"),
    spec: {
      replicas: 1,
      // rwo volume + single writer. never two postgres on same data.
      strategy: { type: "Recreate" },
      selector: { matchLabels: selector("db") },
      template: {
        metadata: { labels: labels("db") },
        spec: podSpec(
          s,
          { uid: POSTGRES_UID, fsGroup: POSTGRES_UID },
          [
            {
              name: "postgres",
              image: POSTGRES_IMAGE,
              imagePullPolicy: "IfNotPresent",
              envFrom: [{ secretRef: { name: NAMES.dbSecret } }],
              ports: [{ name: "postgres", containerPort: 5432, protocol: "TCP" }],
              resources: resources(s, "db"),
              // rofs ok: data on pvc, socket + tmp on emptyDir.
              securityContext: containerSecurity(true),
              startupProbe: { exec: { command: isReady }, periodSeconds: 5, timeoutSeconds: 3, failureThreshold: 60 },
              readinessProbe: { exec: { command: isReady }, periodSeconds: 5, timeoutSeconds: 3, failureThreshold: 3 },
              livenessProbe: { exec: { command: isReady }, periodSeconds: 15, timeoutSeconds: 5, failureThreshold: 6 },
              volumeMounts: [
                { name: "data", mountPath: "/var/lib/postgresql" },
                { name: "run", mountPath: "/var/run/postgresql" },
                { name: "shm", mountPath: "/dev/shm" },
                TMP_MOUNT,
              ],
            },
          ],
          {
            terminationGracePeriodSeconds: 60,
            volumes: [
              { name: "data", persistentVolumeClaim: { claimName: NAMES.dbData } },
              { name: "run", emptyDir: { sizeLimit: "16Mi" } },
              // parallel hash joins live in /dev/shm. runtime default 64MB -> "no space left".
              // ram-backed, counts against pod memory limit.
              { name: "shm", emptyDir: { medium: "Memory", sizeLimit: "256Mi" } },
              TMP_VOLUME,
            ],
          },
        ),
      },
    },
  };
}

export function dbService(s: Settings): Manifest {
  return {
    apiVersion: "v1",
    kind: "Service",
    metadata: meta(s, NAMES.db, "db"),
    spec: {
      type: "ClusterIP",
      selector: selector("db"),
      ports: [{ name: "postgres", port: DB_SERVICE_PORT, targetPort: "postgres", protocol: "TCP" }],
    },
  };
}

// db accepts only pods labelled db-client (api, worker, migrate, backup).
export function dbNetworkPolicy(s: Settings): Manifest {
  return {
    apiVersion: "networking.k8s.io/v1",
    kind: "NetworkPolicy",
    metadata: meta(s, NAMES.dbPolicy, "db"),
    spec: {
      podSelector: { matchLabels: selector("db") },
      policyTypes: ["Ingress"],
      ingress: [
        {
          from: [{ podSelector: { matchLabels: { [DB_CLIENT_LABEL]: "true" } } }],
          ports: [{ protocol: "TCP", port: 5432 }],
        },
      ],
    },
  };
}
