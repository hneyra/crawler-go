// compose all manifests in dependency phases. pure.
import type { Settings } from "../config";
import { type AppSecretValues, appConfigData, appConfigMap, appSecret, dbSecret, namespace } from "./base";
import { backupCronJob, backupPvc } from "./backup";
import { type Manifest, shortHash } from "./common";
import {
  apiDeployment,
  apiService,
  migrateJob,
  rawDataPvc,
  webDeployment,
  webService,
  workerDeployment,
} from "./app";
import { rateLimitMiddleware, ingressRoute } from "./ingress";
import { dbDeployment, dbNetworkPolicy, dbPvc, dbService } from "./postgres";

// phases applied in order: each waits for previous (index.ts dependsOn).
export interface Phases {
  namespace: Manifest;
  config: Manifest[]; // secrets, configmap
  database: Manifest[]; // data phase: postgres + backups (inCluster) + worker raw-data pvc (always)
  migrate: Manifest; // job. must succeed before apps roll.
  apps: Manifest[];
  edge: Manifest[]; // traefik crds
}

export interface StackSecrets<S> extends AppSecretValues<S> {
  // databaseUrl built by caller (inCluster: inClusterDatabaseUrl(dbPassword))
  dbPassword?: S; // inCluster only
}

export function buildPhases<S>(s: Settings, v: StackSecrets<S>): Phases {
  const configHash = shortHash(JSON.stringify(appConfigData(s)), 16);
  const config: Manifest[] = [appConfigMap(s), appSecret(s, { databaseUrl: v.databaseUrl })];
  const database: Manifest[] = [];
  if (s.database.mode === "inCluster") {
    if (v.dbPassword === undefined) throw new Error("inCluster db needs dbPassword");
    config.push(dbSecret(s, v.dbPassword));
    database.push(dbPvc(s), dbNetworkPolicy(s), dbService(s), dbDeployment(s));
    if (s.database.backup.enabled) database.push(backupPvc(s), backupCronJob(s));
  }
  database.push(rawDataPvc(s));
  return {
    namespace: namespace(s),
    config,
    database,
    migrate: migrateJob(s),
    apps: [
      apiService(s),
      apiDeployment(s, configHash),
      workerDeployment(s, configHash),
      webService(s),
      webDeployment(s),
    ],
    edge: [rateLimitMiddleware(s), ingressRoute(s)],
  };
}

export function allManifests(p: Phases): Manifest[] {
  return [p.namespace, ...p.config, ...p.database, p.migrate, ...p.apps, ...p.edge];
}
