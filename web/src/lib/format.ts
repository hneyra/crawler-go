// display helpers. es-PE, local time. pure -> unit tested.
const dtf = new Intl.DateTimeFormat('es-PE', { dateStyle: 'short', timeStyle: 'short' });
const nf = new Intl.NumberFormat('es-PE');

export function fmtDate(iso: string | null | undefined): string {
  if (!iso) return '—';
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? '—' : dtf.format(d);
}

export function fmtNum(n: number | null | undefined): string {
  return n == null ? '—' : nf.format(n);
}

// run duration "1 min 05 s" / "12 s". null while not started/finished.
export function fmtDuration(start?: string | null, end?: string | null): string {
  if (!start || !end) return '—';
  const ms = new Date(end).getTime() - new Date(start).getTime();
  if (!Number.isFinite(ms) || ms < 0) return '—';
  const s = Math.round(ms / 1000);
  if (s < 60) return `${s} s`;
  const m = Math.floor(s / 60);
  return `${m} min ${String(s % 60).padStart(2, '0')} s`;
}

export const RUN_KIND_LABEL: Record<string, string> = {
  DISCOVER_CATEGORY: 'Discovery de categoría',
  DISCOVER_SITE: 'Discovery del sitio',
  EXTRACT_SITE: 'Extracción',
  CRAWL_SITE: 'Crawl completo'
};

export const RUN_STATUS_LABEL: Record<string, string> = {
  QUEUED: 'En cola',
  RUNNING: 'En curso',
  COMPLETED: 'Completada',
  FAILED: 'Fallida'
};

export type Tone = 'verified' | 'neutral' | 'warn' | 'danger' | 'info';

export function runTone(status: string): Tone {
  switch (status) {
    case 'COMPLETED':
      return 'verified';
    case 'FAILED':
      return 'danger';
    case 'RUNNING':
      return 'info';
    default:
      return 'neutral';
  }
}

// one-line summary of run stats. stats shape owned by backend; read defensively.
export function runSummary(stats: Record<string, unknown> | undefined): string {
  const out: string[] = [];
  const d = stats?.discovery as Record<string, unknown> | undefined;
  const x = stats?.extraction as Record<string, unknown> | undefined;
  const n = (v: unknown) => (typeof v === 'number' ? v : 0);
  if (d)
    out.push(`${n(d.pages)} pág., ${n(d.new_urls)} URLs nuevas, ${n(d.duplicate_urls)} repetidas`);
  if (x) out.push(`${n(x.succeeded)} extraídas, ${n(x.failed)} fallidas`);
  return out.join(' · ') || '—';
}

// text lines <-> list (patterns editor). blank lines dropped.
export function linesToList(s: string): string[] {
  return s
    .split('\n')
    .map((l) => l.trim())
    .filter(Boolean);
}
