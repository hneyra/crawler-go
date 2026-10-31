import { describe, expect, it } from 'vitest';
import { fmtDuration, linesToList, runSummary, runTone } from '#lib/format.js';
import { toMap, toRows } from '#lib/kv.js';
import { run } from '../fixtures/api.js';

describe('format', () => {
  it('run duration', () => {
    expect(fmtDuration(run.started_at, run.finished_at)).toBe('1 min 05 s');
    expect(fmtDuration('2026-01-01T00:00:00Z', '2026-01-01T00:00:12Z')).toBe('12 s');
    expect(fmtDuration(run.started_at, undefined)).toBe('—');
    expect(fmtDuration('x', 'y')).toBe('—');
  });
  it('run summary reads stats defensively', () => {
    expect(runSummary(run.stats)).toBe(
      '2 pág., 5 URLs nuevas, 1 repetidas · 4 extraídas, 1 fallidas'
    );
    expect(runSummary({})).toBe('—');
    expect(runSummary({ discovery: { pages: 'x' } })).toBe('0 pág., 0 URLs nuevas, 0 repetidas');
  });
  it('run tone', () => {
    expect(runTone('FAILED')).toBe('danger');
    expect(runTone('COMPLETED')).toBe('verified');
    expect(runTone('QUEUED')).toBe('neutral');
  });
  it('lines to list drops blanks', () => {
    expect(linesToList(' a \n\n b\n')).toEqual(['a', 'b']);
  });
});

describe('key/value rows', () => {
  it('round trip, trims, drops incomplete rows', () => {
    expect(toRows({ title: 'h1' })).toEqual([{ key: 'title', value: 'h1' }]);
    expect(
      toMap([
        { key: ' title ', value: ' h1 ' },
        { key: '', value: 'x' },
        { key: 'price', value: '' }
      ])
    ).toEqual({ title: 'h1' });
    expect(toRows(undefined)).toEqual([]);
  });
});
