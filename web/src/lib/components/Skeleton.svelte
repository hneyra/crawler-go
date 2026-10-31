<script lang="ts">
  // loading placeholder. decorative bars + status text for screen readers.
  // no inline styles: csp style-src-attr is locked down.
  interface Props {
    lines?: number;
    size?: 'text' | 'block';
    label?: string;
  }
  let { lines = 3, size = 'text', label = 'Cargando…' }: Props = $props();
</script>

<div class={`skeleton ${size}`} role="status" aria-live="polite">
  <p class="visually-hidden">{label}</p>
  {#each Array.from({ length: lines }, (_, i) => i) as i (i)}
    <span class="bar" aria-hidden="true"></span>
  {/each}
</div>

<style>
  .skeleton {
    display: grid;
    gap: var(--space-2);
  }
  .bar {
    display: block;
    height: 0.875rem;
    border-radius: var(--radius-s);
    background: linear-gradient(90deg, var(--surface-2), var(--border), var(--surface-2));
    background-size: 200% 100%;
    animation: shimmer 1.4s linear infinite;
  }
  .block .bar {
    height: 6rem;
  }
  .bar:last-child:not(:first-of-type) {
    width: 60%;
  }
  @keyframes shimmer {
    from {
      background-position: 100% 0;
    }
    to {
      background-position: -100% 0;
    }
  }
</style>
