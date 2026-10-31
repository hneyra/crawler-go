<script lang="ts">
  import type { Snippet } from 'svelte';
  interface Props {
    title: string;
    children?: Snippet;
  }
  let { title, children }: Props = $props();
</script>

<div class="empty">
  <svg width="40" height="40" viewBox="0 0 32 32" aria-hidden="true" focusable="false">
    <rect
      x="3"
      y="3"
      width="26"
      height="26"
      rx="7"
      fill="none"
      stroke="currentColor"
      stroke-width="2"
    />
    <path
      d="M10 13h4v4h4v4h4"
      fill="none"
      stroke="currentColor"
      stroke-width="2"
      stroke-linecap="round"
      stroke-linejoin="round"
    />
  </svg>
  <p class="title">{title}</p>
  {#if children}<div class="body">{@render children()}</div>{/if}
</div>

<style>
  .empty {
    text-align: center;
    padding: var(--space-6) var(--space-4);
    background: var(--surface);
    border: 1px dashed var(--border-strong);
    border-radius: var(--radius);
    color: var(--text-muted);
  }
  svg {
    color: var(--text-subtle);
  }
  .title {
    font-weight: var(--fw-semibold);
    color: var(--text);
    margin: var(--space-2) 0;
  }
  .body :global(p:last-child) {
    margin-bottom: 0;
  }
</style>
