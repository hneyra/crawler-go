// ssr api client. talks to backend directly (private url).
// forwards visitor ip as X-Forwarded-For -> backend rate limit per visitor, not per web pod.
import type { RequestEvent } from '@sveltejs/kit';
import { API_INTERNAL_URL } from '$app/env/private';
import { createApi, type ApiClient } from '#lib/api/client.js';

export type ApiEvent = Pick<RequestEvent, 'fetch' | 'getClientAddress'>;

// adapter-node throws if ADDRESS_HEADER set but header absent (direct hit, no proxy).
// -> undefined, never 500 for that.
export function clientAddress(event: Pick<RequestEvent, 'getClientAddress'>): string | undefined {
  try {
    const ip = event.getClientAddress().trim();
    return ip || undefined;
  } catch {
    return undefined;
  }
}

export function serverApi(event: ApiEvent): ApiClient {
  return createApi(API_INTERNAL_URL, (req) => {
    const ip = clientAddress(event);
    // own value only. never pass through what caller put there.
    if (ip) req.headers.set('x-forwarded-for', ip);
    else req.headers.delete('x-forwarded-for');
    return event.fetch(req);
  });
}
