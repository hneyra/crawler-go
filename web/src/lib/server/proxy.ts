// same-origin passthrough for /api -> backend. used when no ingress routes it.
// streams body both ways. backend decides auth; we only forward.
import type { RequestEvent } from '@sveltejs/kit';
import { clientAddress } from './api.js';

const HOP = [
  'connection',
  'keep-alive',
  'proxy-authenticate',
  'proxy-authorization',
  'te',
  'trailer',
  'transfer-encoding',
  'upgrade',
  'host'
];

export function isProxied(pathname: string): boolean {
  return pathname === '/api' || pathname.startsWith('/api/');
}

export async function proxyToBackend(event: RequestEvent, backend: string): Promise<Response> {
  const target = new URL(event.url.pathname + event.url.search, backend + '/');
  const headers = new Headers(event.request.headers);
  for (const h of HOP) headers.delete(h);
  const ip = clientAddress(event);
  if (ip) headers.set('x-forwarded-for', ip);
  else headers.delete('x-forwarded-for');
  headers.set('x-forwarded-proto', event.url.protocol.replace(':', ''));
  headers.set('x-forwarded-host', event.url.host);

  const method = event.request.method;
  const hasBody = method !== 'GET' && method !== 'HEAD';
  let res: Response;
  try {
    res = await fetch(target, {
      method,
      headers,
      body: hasBody ? event.request.body : undefined,
      redirect: 'manual',
      // node fetch needs duplex for streamed bodies.
      ...(hasBody ? { duplex: 'half' } : {})
    } as RequestInit);
  } catch {
    return Response.json(
      { type: 'about:blank', title: 'Bad Gateway', status: 502, detail: 'Backend no disponible' },
      { status: 502, headers: { 'content-type': 'application/problem+json' } }
    );
  }
  const out = new Headers(res.headers);
  // fetch already decoded body -> drop encoding/length.
  out.delete('content-encoding');
  out.delete('content-length');
  for (const h of HOP) out.delete(h);
  return new Response(res.body, { status: res.status, statusText: res.statusText, headers: out });
}
