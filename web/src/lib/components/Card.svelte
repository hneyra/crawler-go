<script lang="ts">
  import type { Snippet } from 'svelte';
  interface Props {
    title?: string;
    headingLevel?: 2 | 3;
    headingId?: string;
    tone?: 'default' | 'muted';
    actions?: Snippet;
    children: Snippet;
  }
  let { title, headingLevel = 2, headingId, tone = 'default', actions, children }: Props = $props();
</script>

<section
  class={`card ui-card ${tone}`}
  aria-labelledby={title && headingId ? headingId : undefined}
>
  {#if title || actions}
    <div class="head">
      {#if title}
        <svelte:element this={`h${headingLevel}`} id={headingId} class="title"
          >{title}</svelte:element
        >
      {/if}
      {#if actions}<div class="actions">{@render actions()}</div>{/if}
    </div>
  {/if}
  {@render children()}
</section>

<style>
  .muted {
    background: var(--surface-2);
    box-shadow: none;
  }
  .head {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-2) var(--space-3);
    margin-bottom: var(--space-3);
  }
  .title {
    font-size: var(--fs-lg);
    margin: 0;
  }
</style>
