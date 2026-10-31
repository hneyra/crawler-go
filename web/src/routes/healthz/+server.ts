import type { RequestHandler } from './$types';

// liveness for k8s. no backend call: process alive = ok.
export const GET: RequestHandler = () =>
  new Response('ok', { headers: { 'content-type': 'text/plain', 'cache-control': 'no-store' } });
