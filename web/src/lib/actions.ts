// queue runs. backend dedupes: same active run returned.
import type { Run } from '#lib/api/types.js';
import { api, CSRF, unwrap } from './api.js';
import { RUN_KIND_LABEL } from './format.js';

export type SiteAction = 'crawl' | 'discover' | 'extract';

export function runSite(siteId: number, action: SiteAction): Promise<Run> {
  const opts = { params: { path: { siteId }, header: CSRF } };
  switch (action) {
    case 'crawl':
      return unwrap(api().POST('/api/v1/sites/{siteId}/crawl', opts));
    case 'discover':
      return unwrap(api().POST('/api/v1/sites/{siteId}/discover', opts));
    case 'extract':
      return unwrap(api().POST('/api/v1/sites/{siteId}/extract', opts));
  }
}

export function runCategory(categoryId: number): Promise<Run> {
  return unwrap(
    api().POST('/api/v1/categories/{categoryId}/discover', {
      params: { path: { categoryId }, header: CSRF }
    })
  );
}

export function queuedMessage(r: Run): string {
  const what = RUN_KIND_LABEL[r.kind] ?? r.kind;
  return r.status === 'QUEUED'
    ? `${what} en cola (ejecución #${r.id}).`
    : `${what} ya estaba en curso (ejecución #${r.id}).`;
}
