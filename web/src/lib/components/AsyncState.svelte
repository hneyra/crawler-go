<script lang="ts">
  // loading / error / empty wrapper for spa lists.
  import type { Snippet } from 'svelte';
  import Loading from './Loading.svelte';

  interface Props {
    loading: boolean;
    error: string | null;
    empty?: boolean;
    emptyText?: string;
    retry?: () => void;
    children: Snippet;
  }
  let {
    loading,
    error,
    empty = false,
    emptyText = 'No hay elementos.',
    retry,
    children
  }: Props = $props();
</script>

{#if loading}
  <Loading />
{:else if error}
  <div class="alert alert-danger" role="alert">
    <p>{error}</p>
    {#if retry}<button class="btn btn-secondary btn-small" type="button" onclick={retry}
        >Reintentar</button
      >{/if}
  </div>
{:else if empty}
  <p class="muted empty">{emptyText}</p>
{:else}
  {@render children()}
{/if}
