<script lang="ts">
  // json value as nested list. third-party data: text only, never {@html}.
  import Self from './PropsTree.svelte';
  interface Props {
    value: unknown;
    depth?: number;
  }
  let { value, depth = 0 }: Props = $props();
  const isObj = (v: unknown): v is Record<string, unknown> =>
    !!v && typeof v === 'object' && !Array.isArray(v);
  const entries = $derived(
    Array.isArray(value)
      ? value.map((v, i) => [String(i), v] as const)
      : isObj(value)
        ? Object.entries(value)
        : []
  );
  const scalar = (v: unknown) =>
    v === null ? 'null' : typeof v === 'string' ? v : JSON.stringify(v);
</script>

{#if entries.length === 0 && (Array.isArray(value) || isObj(value))}
  <span class="muted">{Array.isArray(value) ? '[]' : '{}'}</span>
{:else if entries.length > 0}
  <ul class="tree" class:nested={depth > 0}>
    {#each entries as [k, v] (k)}
      <li>
        {#if (Array.isArray(v) || isObj(v)) && depth < 8}
          <details open={depth < 1}>
            <summary><span class="key">{k}</span></summary>
            <Self value={v} depth={depth + 1} />
          </details>
        {:else}
          <span class="key">{k}</span>: <span class="val">{scalar(v)}</span>
        {/if}
      </li>
    {/each}
  </ul>
{:else}
  <span class="val">{scalar(value)}</span>
{/if}

<style>
  .tree {
    list-style: none;
    margin: 0;
    padding: 0;
    font-size: var(--fs-sm);
    overflow-wrap: anywhere;
  }
  .nested {
    padding-left: var(--space-4);
    border-left: 1px solid var(--border);
  }
  .key {
    font-weight: var(--fw-medium);
  }
  .val {
    color: var(--text-muted);
    white-space: pre-wrap;
  }
</style>
