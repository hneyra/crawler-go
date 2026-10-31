// session state for spa areas. backend = source of truth; this is only a cache for ui.
import type { Session } from '#lib/api/types.js';
import { ApiError } from '#lib/api/problem.js';
import { api, CSRF, csrfFetch, unwrap } from './api.js';

class SessionStore {
  current = $state<Session | null>(null);
  loaded = $state(false);
  error = $state<string | null>(null);

  async load(force = false): Promise<Session | null> {
    if (this.loaded && !force) return this.current;
    try {
      const s = await unwrap(api().GET('/api/v1/auth/session'));
      this.current = s;
      csrfFetch.setToken(s.csrf_token);
      this.error = null;
    } catch (e) {
      this.current = null;
      if (!(e instanceof ApiError && e.status === 401)) {
        this.error = 'No pudimos comprobar tu sesión.';
      }
    }
    this.loaded = true;
    return this.current;
  }

  set(s: Session) {
    this.current = s;
    this.loaded = true;
    csrfFetch.setToken(s.csrf_token);
  }

  async logout() {
    try {
      await api().POST('/api/v1/auth/logout', { params: { header: CSRF } });
    } finally {
      this.current = null;
      csrfFetch.reset();
    }
  }
}

export const session = new SessionStore();
