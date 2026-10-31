// pulumi entry. config -> pure manifests -> resources on EXPLICIT provider.
import * as k8s from "@pulumi/kubernetes";
import * as pulumi from "@pulumi/pulumi";
import { loadConfig } from "./config";
import { inClusterDatabaseUrl } from "./components/base";
import type { Manifest } from "./components/common";
import { buildPhases } from "./components/stack";

const { settings, secrets } = loadConfig();

// never ambient kubeconfig/context. kubeconfig = secret config.
const provider = new k8s.Provider("k3s", {
  kubeconfig: secrets.kubeconfig,
  enableServerSideApply: true,
  // no namespace default: every manifest sets its own.
});

const databaseUrl =
  settings.database.mode === "inCluster"
    ? pulumi.secret(secrets.dbPassword!.apply(inClusterDatabaseUrl))
    : secrets.databaseUrl!;

const phases = buildPhases<pulumi.Output<string>>(settings, {
  databaseUrl,
  ...(secrets.dbPassword ? { dbPassword: secrets.dbPassword } : {}),
});

// manifest -> typed resource. args are plain objects; cast at this single boundary.
function create(m: Manifest, dependsOn: pulumi.Resource[]): pulumi.CustomResource {
  const name = `${m.kind.toLowerCase()}-${m.metadata.name}`;
  // fixed metadata.name -> immutable field change needs delete first.
  const opts: pulumi.CustomResourceOptions = { provider, dependsOn, deleteBeforeReplace: true };
  const a = <T>(): T => m as unknown as T;
  switch (`${m.apiVersion}/${m.kind}`) {
    case "v1/Namespace":
      return new k8s.core.v1.Namespace(name, a<k8s.core.v1.NamespaceArgs>(), { ...opts, deleteBeforeReplace: false });
    case "v1/Secret":
      return new k8s.core.v1.Secret(name, a<k8s.core.v1.SecretArgs>(), opts);
    case "v1/ConfigMap":
      return new k8s.core.v1.ConfigMap(name, a<k8s.core.v1.ConfigMapArgs>(), opts);
    case "v1/PersistentVolumeClaim":
      // data lives here (db, backups, worker raw-data). never delete on replace.
      return new k8s.core.v1.PersistentVolumeClaim(name, a<k8s.core.v1.PersistentVolumeClaimArgs>(), { ...opts, protect: settings.env === "prod" });
    case "v1/Service":
      return new k8s.core.v1.Service(name, a<k8s.core.v1.ServiceArgs>(), opts);
    case "apps/v1/Deployment":
      return new k8s.apps.v1.Deployment(name, a<k8s.apps.v1.DeploymentArgs>(), opts);
    case "batch/v1/Job":
      return new k8s.batch.v1.Job(name, a<k8s.batch.v1.JobArgs>(), opts);
    case "batch/v1/CronJob":
      return new k8s.batch.v1.CronJob(name, a<k8s.batch.v1.CronJobArgs>(), opts);
    case "networking.k8s.io/v1/NetworkPolicy":
      return new k8s.networking.v1.NetworkPolicy(name, a<k8s.networking.v1.NetworkPolicyArgs>(), opts);
    default:
      // traefik crds (Middleware, IngressRoute)
      return new k8s.apiextensions.CustomResource(name, a<k8s.apiextensions.CustomResourceArgs>(), opts);
  }
}

const ns = create(phases.namespace, []);
const config = phases.config.map((m) => create(m, [ns]));
const database = phases.database.map((m) => create(m, [ns, ...config]));
// job awaited to completion -> apps roll only after schema up to date.
const migrate = create(phases.migrate, [ns, ...config, ...database]);
const apps = phases.apps.map((m) => create(m, [migrate]));
phases.edge.map((m) => create(m, [ns, ...apps]));

export const namespace = settings.namespace;
export const url = `${settings.ingress.tls ? "https" : "http"}://${settings.ingress.host}`;
export const backendImage = `${settings.images.backend}:${settings.images.tag}`;
export const workerImage = `${settings.images.worker}:${settings.images.workerTag}`;
export const webImage = `${settings.images.web}:${settings.images.webTag}`;
export const databaseMode = settings.database.mode;
export const migrateJob = phases.migrate.metadata.name;
