// nightly pg_dump -Fc to backups pvc + retention. inCluster only. pure.
// restore: docs/guides/deploy-k3s.md
import type { Settings } from "../config";
import { DB_NAME } from "./base";
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
} from "./common";
import { POSTGRES_IMAGE } from "./postgres";

// dump to .partial, verify toc readable, rename atomic, prune old.
export const BACKUP_SCRIPT = [
  "set -eu",
  'ts="$(date -u +%Y%m%dT%H%M%SZ)"',
  'f="/backups/crawler-${ts}.dump"',
  'pg_dump -Fc --no-owner --no-privileges -f "${f}.partial"',
  'pg_restore --list "${f}.partial" > /dev/null',
  'mv "${f}.partial" "${f}"',
  'echo "backup ok: ${f} $(wc -c < "${f}") bytes"',
  'find /backups -maxdepth 1 -name "crawler-*.dump" -mtime +"${RETENTION_DAYS}" -print -delete',
  'find /backups -maxdepth 1 -name "*.partial" -mmin +180 -print -delete',
].join("\n");

export function backupPvc(s: Settings): Manifest {
  return pvc(s, NAMES.backups, "backup", s.database.backup.storageSize, s.database.storageClass);
}

export function backupCronJob(s: Settings): Manifest {
  const b = s.database.backup;
  return {
    apiVersion: "batch/v1",
    kind: "CronJob",
    metadata: meta(s, NAMES.backup, "backup"),
    spec: {
      schedule: b.schedule,
      timeZone: "Etc/UTC",
      concurrencyPolicy: "Forbid",
      startingDeadlineSeconds: 3600,
      successfulJobsHistoryLimit: 3,
      failedJobsHistoryLimit: 3,
      jobTemplate: {
        spec: {
          backoffLimit: 1,
          activeDeadlineSeconds: 3600,
          template: {
            metadata: { labels: labels("backup", { [DB_CLIENT_LABEL]: "true" }) },
            spec: podSpec(
              s,
              { uid: POSTGRES_UID, fsGroup: POSTGRES_UID },
              [
                {
                  name: "pg-dump",
                  image: POSTGRES_IMAGE,
                  imagePullPolicy: "IfNotPresent",
                  command: ["/bin/sh", "-c", BACKUP_SCRIPT],
                  env: [
                    { name: "PGHOST", value: NAMES.db },
                    { name: "PGDATABASE", value: DB_NAME },
                    { name: "PGUSER", valueFrom: { secretKeyRef: { name: NAMES.dbSecret, key: "POSTGRES_USER" } } },
                    {
                      name: "PGPASSWORD",
                      valueFrom: { secretKeyRef: { name: NAMES.dbSecret, key: "POSTGRES_PASSWORD" } },
                    },
                    { name: "RETENTION_DAYS", value: String(b.retentionDays) },
                  ],
                  resources: resources(s, "backup"),
                  securityContext: containerSecurity(true),
                  volumeMounts: [{ name: "backups", mountPath: "/backups" }, TMP_MOUNT],
                },
              ],
              {
                restartPolicy: "Never",
                volumes: [{ name: "backups", persistentVolumeClaim: { claimName: NAMES.backups } }, TMP_VOLUME],
              },
            ),
          },
        },
      },
    },
  };
}
