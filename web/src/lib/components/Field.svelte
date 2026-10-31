<script lang="ts">
  // labelled input w/ hint + field error (aria-describedby). value bound.
  import type { HTMLInputAttributes } from 'svelte/elements';

  interface Props extends Omit<HTMLInputAttributes, 'value'> {
    label: string;
    name: string;
    value?: string;
    hint?: string;
    error?: string;
  }
  let { label, name, value = $bindable(''), hint, error, id, ...rest }: Props = $props();
  const uid = $props.id();
  const fid = $derived(id ?? `${uid}-${name}`);
  const describedby = $derived(
    [hint ? `${fid}-hint` : '', error ? `${fid}-err` : ''].filter(Boolean).join(' ') || undefined
  );
</script>

<div class="field">
  <label for={fid}>{label}</label>
  <input
    id={fid}
    {name}
    bind:value
    aria-invalid={error ? 'true' : undefined}
    aria-describedby={describedby}
    {...rest}
  />
  {#if hint}<p class="hint" id={`${fid}-hint`}>{hint}</p>{/if}
  {#if error}<p class="error" id={`${fid}-err`}>{error}</p>{/if}
</div>
