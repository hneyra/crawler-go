// test fixtures. fake values only.
import { type RawConfig, type SecretPresence, type Settings, parseSettings } from "../config";

export const BACKEND = "ghcr.io/hneyra/crawler-go";
export const WEB = "ghcr.io/hneyra/crawler-web";

export function localRaw(): RawConfig {
  return {
    env: "dev",
    namespace: "crawler-local",
    images: { backend: BACKEND, web: WEB, tag: "0.1.0" },
    database: { mode: "inCluster" },
    ingress: { host: "crawler.localhost" },
  };
}

export function prodRaw(): RawConfig {
  return {
    ...localRaw(),
    env: "prod",
    namespace: "crawler",
    images: { backend: BACKEND, web: WEB, tag: "1.2.3" },
    ingress: { host: "crawler.example", tls: { certResolver: "letsencrypt" } },
  };
}

export const inClusterSecrets: SecretPresence = {
  dbPassword: true,
  databaseUrl: false,
  kubeconfig: true,
};

export function localSettings(): Settings {
  return parseSettings("local", localRaw(), inClusterSecrets);
}
