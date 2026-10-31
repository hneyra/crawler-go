<script lang="ts">
  // link tabs (url state, works w/o js). current = aria-current. segmented = compact toggle.
  interface Item {
    href: string;
    label: string;
    sublabel?: string;
    current?: boolean;
    rel?: string;
  }
  interface Props {
    label: string;
    items: Item[];
    variant?: 'tabs' | 'segmented';
    currentValue?: 'page' | 'true';
  }
  let { label, items, variant = 'tabs', currentValue = 'page' }: Props = $props();
</script>

<nav aria-label={label} class={variant}>
  <ul>
    {#each items as it (it.href)}
      <li>
        <a
          href={it.href}
          rel={it.rel}
          aria-current={it.current ? currentValue : undefined}
          data-sveltekit-reset="false"
        >
          <span class="l">{it.label}</span>
          {#if it.sublabel}<span class="s">{it.sublabel}</span>{/if}
        </a>
      </li>
    {/each}
  </ul>
</nav>

<style>
  ul {
    list-style: none;
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-2);
    padding: 0;
    margin: 0;
  }
  a {
    display: flex;
    flex-direction: column;
    justify-content: center;
    text-decoration: none;
    color: var(--text);
  }
  .tabs a {
    min-height: 3.25rem;
    min-width: 10rem;
    padding: var(--space-2) var(--space-4);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    background: var(--surface);
    box-shadow: var(--shadow-s);
    transition:
      border-color var(--duration) var(--ease),
      box-shadow var(--duration) var(--ease);
  }
  .tabs a:hover {
    border-color: var(--border-strong);
  }
  .tabs a[aria-current] {
    border-color: var(--brand);
    box-shadow: 0 0 0 1px var(--brand);
  }
  .tabs .l {
    font-weight: var(--fw-medium);
    font-size: var(--fs-sm);
  }
  .tabs .s {
    font-size: var(--fs-sm);
    color: var(--text-muted);
    font-variant-numeric: tabular-nums;
  }
  /* segmented control: track + raised thumb. */
  .segmented ul {
    display: inline-flex;
    gap: 2px;
    padding: 3px;
    background: var(--surface-2);
    border: 1px solid var(--border);
    border-radius: var(--radius);
  }
  .segmented a {
    flex-direction: row;
    align-items: center;
    min-height: 2rem;
    padding: 0 var(--space-3);
    border: 1px solid transparent;
    border-radius: var(--radius-s);
    color: var(--text-muted);
    font-size: var(--fs-sm);
    font-weight: var(--fw-medium);
    transition:
      color var(--duration) var(--ease),
      background var(--duration) var(--ease);
  }
  .segmented a:hover {
    color: var(--text);
  }
  .segmented a[aria-current] {
    background: var(--surface);
    border-color: var(--border);
    color: var(--text);
    box-shadow: var(--shadow-s);
  }
</style>
