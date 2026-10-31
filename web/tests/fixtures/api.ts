// test fixtures typed from generated api schema. tests only.
import type { Run, Session } from '#lib/api/types.js';

export const session: Session = {
  user: { id: 1, email: 'admin@crawler.example', is_admin: true },
  csrf_token: 'csrf-abc',
  expires_at: '2026-11-28T00:00:00Z'
};

export const run: Run = {
  id: 7,
  site_id: 1,
  kind: 'CRAWL_SITE',
  status: 'COMPLETED',
  stats: {
    discovery: { pages: 2, new_urls: 5, duplicate_urls: 1, errors: 0 },
    extraction: { claimed: 5, succeeded: 4, failed: 1 }
  },
  created_at: '2026-10-31T10:00:00Z',
  started_at: '2026-10-31T10:00:01Z',
  finished_at: '2026-10-31T10:01:06Z'
};

export function jsonResponse(body: unknown, status = 200, type = 'application/json') {
  return new Response(JSON.stringify(body), { status, headers: { 'content-type': type } });
}
