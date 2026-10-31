// browser-only api for the admin spa. same-origin /api, csrf header, 401 -> login.
import { goto } from '$app/navigation';
import { createApi, unwrap, type ApiClient } from '#lib/api/client.js';
import { createCsrfFetch } from '#lib/api/csrf-fetch.js';

export const csrfFetch = createCsrfFetch({
  onUnauthorized: () => {
    if (location.pathname.startsWith('/login')) return;
    const here = location.pathname + location.search;
    void goto(`/login?next=${encodeURIComponent(here)}`);
  }
});

let client: ApiClient | null = null;
export function api(): ApiClient {
  client ??= createApi(location.origin, csrfFetch);
  return client;
}

export { unwrap };

// typed client requires the csrf header param. placeholder; csrfFetch fills the real token.
export const CSRF = { 'X-CSRF-Token': '' } as const;

export { safeNext } from './next.js';
