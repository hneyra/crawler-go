// traefik (k3s bundled) IngressRoute + rate limit middleware. pure.
import type { Settings } from "../config";
import { type Manifest, NAMES, meta } from "./common";
import { PORTS } from "./app";

export const TRAEFIK_API = "traefik.io/v1alpha1";

export function rateLimitMiddleware(s: Settings): Manifest {
  return {
    apiVersion: TRAEFIK_API,
    kind: "Middleware",
    metadata: meta(s, NAMES.rateLimit, "ingress"),
    spec: {
      // per client ip (remote addr seen by traefik). backend also limits in-process.
      rateLimit: { average: s.ingress.rateLimit.average, burst: s.ingress.rateLimit.burst, period: "1s" },
    },
  };
}

// one host. /api -> api. rest -> web (spa calls /api same-origin). /healthz /readyz /metrics never routed to api.
export function ingressRoute(s: Settings): Manifest {
  const h = `Host(\`${s.ingress.host}\`)`;
  const tls = s.ingress.tls;
  return {
    apiVersion: TRAEFIK_API,
    kind: "IngressRoute",
    metadata: meta(s, NAMES.route, "ingress"),
    spec: {
      entryPoints: [tls ? "websecure" : "web"],
      routes: [
        {
          kind: "Rule",
          match: `${h} && PathPrefix(\`/api\`)`,
          middlewares: [{ name: NAMES.rateLimit }],
          services: [{ name: NAMES.api, port: PORTS.api }],
        },
        {
          kind: "Rule",
          match: h,
          services: [{ name: NAMES.web, port: PORTS.web }],
        },
      ],
      ...(tls
        ? { tls: tls.certResolver ? { certResolver: tls.certResolver } : { secretName: tls.secretName } }
        : {}),
    },
  };
}
