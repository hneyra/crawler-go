import { describe, expect, it, vi } from 'vitest';
import { createCsrfFetch } from '#lib/api/csrf-fetch.js';
import { jsonResponse, session } from '../fixtures/api.js';

const O = 'https://crawler.example';

function backend(handler?: (req: Request) => Response | undefined) {
  const calls: Request[] = [];
  const fetch = vi.fn(async (req: Request) => {
    calls.push(req);
    const custom = handler?.(req);
    if (custom) return custom;
    if (new URL(req.url).pathname === '/api/v1/auth/session') return jsonResponse(session);
    return new Response(null, { status: 204 });
  });
  return { fetch, calls };
}

describe('csrf fetch wrapper', () => {
  it('GET: no session fetch, no csrf header, same-origin credentials', async () => {
    const b = backend();
    const f = createCsrfFetch({ fetch: b.fetch });
    await f(new Request(`${O}/api/v1/merchant/stores`));
    expect(b.calls).toHaveLength(1);
    expect(b.calls[0]!.headers.get('X-CSRF-Token')).toBeNull();
    expect(b.calls[0]!.credentials).toBe('same-origin');
  });

  it('unsafe methods fetch token once and send X-CSRF-Token with body intact', async () => {
    const b = backend();
    const f = createCsrfFetch({ fetch: b.fetch });
    await f(
      new Request(`${O}/api/v1/merchant/stores`, {
        method: 'POST',
        body: '{"a":1}',
        headers: { 'content-type': 'application/json' }
      })
    );
    await f(new Request(`${O}/api/v1/merchant/stores/x`, { method: 'DELETE' }));
    const paths = b.calls.map((c) => `${c.method} ${new URL(c.url).pathname}`);
    expect(paths).toEqual([
      'GET /api/v1/auth/session',
      'POST /api/v1/merchant/stores',
      'DELETE /api/v1/merchant/stores/x'
    ]);
    expect(b.calls[1]!.headers.get('X-CSRF-Token')).toBe('csrf-abc');
    expect(await b.calls[1]!.text()).toBe('{"a":1}');
    expect(b.calls[1]!.headers.get('content-type')).toBe('application/json');
    expect(b.calls[2]!.headers.get('X-CSRF-Token')).toBe('csrf-abc');
  });

  it('setToken skips session fetch; 403 clears token', async () => {
    let first = true;
    const b = backend((req) => {
      if (req.method === 'PUT' && first) {
        first = false;
        return new Response(null, { status: 403 });
      }
      return undefined;
    });
    const f = createCsrfFetch({ fetch: b.fetch });
    f.setToken('t1');
    const r = await f(new Request(`${O}/api/v1/x`, { method: 'PUT', body: 'x' }));
    expect(r.status).toBe(403);
    expect(b.calls[0]!.headers.get('X-CSRF-Token')).toBe('t1');
    await f(new Request(`${O}/api/v1/x`, { method: 'PUT', body: 'x' }));
    // token refetched from session after 403
    expect(b.calls[1]!.url).toContain('/api/v1/auth/session');
    expect(b.calls[2]!.headers.get('X-CSRF-Token')).toBe('csrf-abc');
  });

  it('401 on app endpoint -> onUnauthorized; not for auth endpoints', async () => {
    const b = backend(() => new Response(null, { status: 401 }));
    const onUnauthorized = vi.fn();
    const f = createCsrfFetch({ fetch: b.fetch, onUnauthorized });
    await f(new Request(`${O}/api/v1/auth/login`, { method: 'GET' }));
    expect(onUnauthorized).not.toHaveBeenCalled();
    await f(new Request(`${O}/api/v1/merchant/stores`));
    expect(onUnauthorized).toHaveBeenCalledTimes(1);
  });

  it('concurrent unsafe calls share one session request', async () => {
    const b = backend();
    const f = createCsrfFetch({ fetch: b.fetch });
    await Promise.all(
      [1, 2, 3].map(() => f(new Request(`${O}/api/v1/x`, { method: 'POST', body: 'x' })))
    );
    expect(b.calls.filter((c) => c.url.endsWith('/api/v1/auth/session'))).toHaveLength(1);
  });
});

describe('body-less unsafe calls', () => {
  it('get json content-type (kit csrf guard treats bare POST as form)', async () => {
    const b = backend();
    const f = createCsrfFetch({ fetch: b.fetch });
    await f(new Request(`${O}/api/v1/sites/1/crawl`, { method: 'POST' }));
    const post = b.calls.find((r) => r.method === 'POST')!;
    expect(post.headers.get('content-type')).toBe('application/json');
    expect(post.headers.get('x-csrf-token')).toBe('csrf-abc');
  });
});
