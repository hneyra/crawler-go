<script lang="ts">
  // button pager for spa lists (no url).
  interface Props {
    page: number;
    totalPages: number;
    go: (p: number) => void;
  }
  let { page, totalPages, go }: Props = $props();
</script>

{#if totalPages > 1}
  <nav class="pager" aria-label="Paginación">
    <button
      class="btn btn-small btn-secondary"
      type="button"
      disabled={page <= 1}
      onclick={() => go(page - 1)}>Anterior</button
    >
    <span class="small">Página {page} de {totalPages}</span>
    <button
      class="btn btn-small btn-secondary"
      type="button"
      disabled={page >= totalPages}
      onclick={() => go(page + 1)}>Siguiente</button
    >
  </nav>
{/if}

<style>
  .pager {
    display: flex;
    gap: var(--space-3);
    align-items: center;
    margin-top: var(--space-4);
  }
</style>
