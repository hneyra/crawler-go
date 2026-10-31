// wcag contrast of design tokens, light + dark. tokens read from tokens.css (single source).
import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';

const tokensCss = readFileSync(new URL('../../src/lib/styles/tokens.css', import.meta.url), 'utf8');
const globalCss = readFileSync(new URL('../../src/lib/styles/global.css', import.meta.url), 'utf8');

const darkAt = tokensCss.indexOf('@media (prefers-color-scheme: dark)');
const lightBlock = tokensCss.slice(0, darkAt);
const darkBlock = tokensCss.slice(darkAt);

function tokenIn(block: string, name: string): string {
  const m = new RegExp(`--${name}:\\s*(#[0-9a-fA-F]{6})`).exec(block);
  if (!m) throw new Error(`token ${name} missing`);
  return m[1]!;
}

function lum(hex: string): number {
  const c = [1, 3, 5]
    .map((i) => parseInt(hex.slice(i, i + 2), 16) / 255)
    .map((v) => (v <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4));
  return 0.2126 * c[0]! + 0.7152 * c[1]! + 0.0722 * c[2]!;
}

function ratio(a: string, b: string): number {
  const [x, y] = [lum(a), lum(b)];
  return (Math.max(x, y) + 0.05) / (Math.min(x, y) + 0.05);
}

// [fg, bg, min]
const TEXT_PAIRS: [string, string, number][] = [
  ['text', 'bg', 4.5],
  ['text', 'surface', 4.5],
  ['text-muted', 'surface', 4.5],
  ['text-muted', 'surface-2', 4.5],
  ['text-subtle', 'surface', 4.5],
  ['text-subtle', 'surface-2', 4.5],
  ['brand', 'surface', 4.5],
  ['brand', 'bg', 4.5],
  ['brand-strong', 'brand-soft', 4.5],
  ['on-cta', 'cta', 4.5],
  ['on-cta', 'cta-hover', 4.5],
  ['best', 'surface', 4.5],
  ['best', 'bg', 4.5],
  ['best', 'best-soft', 4.5],
  ['warn', 'warn-soft', 4.5],
  ['warn', 'surface', 4.5],
  ['danger', 'danger-soft', 4.5],
  ['demo', 'demo-soft', 4.5],
  ['on-ink', 'ink', 4.5],
  ['on-ink', 'ink-2', 4.5],
  ['on-ink-muted', 'ink', 4.5],
  ['on-ink-muted', 'ink-2', 4.5],
  ['mint', 'ink', 4.5]
];

// non-text (wcag 1.4.11): controls, focus ring
const UI_PAIRS: [string, string, number][] = [
  ['border-strong', 'surface', 3],
  ['border-strong', 'bg', 3],
  ['border-strong', 'surface-2', 3],
  ['focus', 'surface', 3],
  ['focus', 'bg', 3],
  ['focus-on-ink', 'ink', 3],
  ['cta', 'surface', 3],
  ['chart-line', 'surface', 3]
];

describe.each([
  ['light', lightBlock],
  ['dark', darkBlock]
])('token contrast (%s)', (_name, block) => {
  it.each([...TEXT_PAIRS, ...UI_PAIRS])('%s on %s >= %d', (fg, bg, min) => {
    expect(ratio(tokenIn(block, fg), tokenIn(block, bg))).toBeGreaterThanOrEqual(min);
  });
});

describe('form controls', () => {
  it('inputs use --border-strong', () => {
    expect(globalCss).toMatch(/textarea\s*\{[^}]*border:\s*1px solid var\(--border-strong\)/);
  });
});
