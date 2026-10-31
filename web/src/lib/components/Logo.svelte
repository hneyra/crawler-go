<script lang="ts">
  // own mark: link graph (page -> pages). crawl = follow links.
  interface Props {
    tone?: 'on-ink' | 'default';
    href?: string;
    label?: string;
  }
  let { tone = 'default', href = '/', label = 'Crawler, inicio' }: Props = $props();
</script>

<a class={`logo ${tone}`} {href} aria-label={label}>
  <svg width="28" height="28" viewBox="0 0 32 32" aria-hidden="true" focusable="false">
    <rect class="tile" width="32" height="32" rx="8" />
    <path
      class="step"
      d="M9 10l7 6 7-6M16 16v7"
      fill="none"
      stroke-width="2.5"
      stroke-linecap="round"
    />
    <circle class="dot" cx="9" cy="10" r="2.6" />
    <circle class="dot" cx="23" cy="10" r="2.6" />
    <circle class="dot" cx="16" cy="16" r="3" />
    <circle class="dot" cx="16" cy="23" r="2.6" />
  </svg>
  <span class="word">crawler</span>
</a>

<style>
  .logo {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    text-decoration: none;
    font-family: var(--font-display);
    font-weight: var(--fw-bold);
    font-size: 1.3125rem;
    letter-spacing: -0.035em;
    line-height: 1;
    min-height: var(--tap);
  }
  .word {
    padding-bottom: 0.1em;
  }
  .on-ink {
    color: var(--on-ink);
  }
  .on-ink .tile {
    fill: var(--ink-2);
    stroke: var(--mint);
    stroke-opacity: 0.35;
  }
  .default {
    color: var(--text);
  }
  .default .tile {
    fill: var(--ink);
    stroke: var(--border);
  }
  .step {
    stroke: var(--mint);
  }
  .dot {
    fill: var(--mint);
  }
  .logo:hover {
    color: inherit;
  }
  .on-ink:hover {
    color: var(--on-ink);
  }
  .on-ink:focus-visible {
    outline-color: var(--focus-on-ink);
  }
</style>
