// browser fetch for portal/admin. same-origin cookies + X-CSRF-Token on unsafe methods.
// token from GET /api/v1/auth/session. 401 -> onUnauthorized (login redirect).

const SAFE = new Set(['GET', 'HEAD', 'OPTIONS']);
export const SESSION_PATH = '/api/v1/auth/session';

export interface CsrfFetchOptions {
  fetch?: (req: Request) => Promise<Response>;
  // called on 401 for non-auth endpoints.
  onUnauthorized?: () => void;
}

export interface CsrfFetch {
  (req: Request): Promise<Response>;
  setToken(token: string | null): void;
  reset(): void;
}

export function createCsrfFetch(opts: CsrfFetchOptions = {}): CsrfFetch {
  const doFetch = opts.fetch ?? ((r: Request) => globalThis.fetch(r));
  let token: string | null = null;
  let inflight: Promise<string | null> | null = null;

  async function loadToken(origin: string): Promise<string | null> {
    const res = await doFetch(
      new Request(new URL(SESSION_PATH, origin), {
        method: 'GET',
        credentials: 'same-origin',
        headers: { accept: 'application/json' }
      })
    );
    if (!res.ok) return null;
    try {
      const body = (await res.json()) as { csrf_token?: unknown };
      return typeof body.csrf_token === 'string' ? body.csrf_token : null;
    } catch {
      return null;
    }
  }

  function getToken(origin: string): Promise<string | null> {
    if (token) return Promise.resolve(token);
    inflight ??= loadToken(origin).then(
      (t) => {
        token = t;
        inflight = null;
        return t;
      },
      (e) => {
        inflight = null;
        throw e;
      }
    );
    return inflight;
  }

  const f = (async (input: Request) => {
    const method = input.method.toUpperCase();
    const url = new URL(input.url);
    const headers = new Headers(input.headers);
    if (!SAFE.has(method)) {
      const t = await getToken(url.origin);
      if (t) headers.set('X-CSRF-Token', t);
    }
    const body = SAFE.has(method) ? undefined : await input.arrayBuffer();
    // body-less unsafe call (POST /crawl, DELETE): no content-type would look like a
    // form post to kit's csrf guard. api is json-only anyway.
    if (!SAFE.has(method) && !headers.has('content-type'))
      headers.set('content-type', 'application/json');
    const req = new Request(input.url, {
      method,
      headers,
      body: body && body.byteLength > 0 ? body : undefined,
      credentials: 'same-origin',
      redirect: input.redirect,
      signal: input.signal
    });
    const res = await doFetch(req);
    if (res.status === 401) {
      token = null;
      if (!url.pathname.startsWith('/api/v1/auth/')) opts.onUnauthorized?.();
    } else if (res.status === 403 && !SAFE.has(method)) {
      // maybe stale token (session rotated). refetch next time.
      token = null;
    }
    return res;
  }) as CsrfFetch;

  f.setToken = (t) => {
    token = t;
  };
  f.reset = () => {
    token = null;
    inflight = null;
  };
  return f;
}
