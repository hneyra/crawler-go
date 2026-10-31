<script lang="ts">
  interface Props {
    label?: string;
  }
  let { label = 'Cargando…' }: Props = $props();
</script>

<p class="loading muted" role="status" aria-live="polite">{label}</p>

<style>
  .loading {
    padding: var(--space-4) 0;
  }
</style>
