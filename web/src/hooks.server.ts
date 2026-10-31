import type { Handle, HandleServerError } from '@sveltejs/kit/hooks';
import { API_INTERNAL_URL } from '$app/env/private';
import { isProxied, proxyToBackend } from '#lib/server/proxy.js';

// whole app = private admin spa. never indexed, never cached.
export const handle: Handle = async ({ event, resolve }) => {
  if (isProxied(event.url.pathname)) {
    return proxyToBackend(event, API_INTERNAL_URL);
  }
  const response = await resolve(event);
  // csp comes from kit config (nonce/hash). rest here.
  const h = response.headers;
  h.set('X-Content-Type-Options', 'nosniff');
  h.set('Referrer-Policy', 'strict-origin-when-cross-origin');
  h.set('X-Frame-Options', 'DENY');
  h.set('Cross-Origin-Opener-Policy', 'same-origin');
  h.set('Permissions-Policy', 'camera=(), microphone=(), geolocation=(), payment=()');
  h.set('X-Robots-Tag', 'noindex, nofollow');
  if (event.url.pathname !== '/healthz') h.set('Cache-Control', 'no-store');
  return response;
};

export const handleError: HandleServerError = (input) => {
  if (input.kind === 'unknown') {
    console.error(`[ssr] ${input.event.request.method} ${input.event.url.pathname}`, input.error);
    return { status: 500, message: 'Error interno' };
  }
};
