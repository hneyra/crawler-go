<script lang="ts">
  // edits map<string,string> as rows. keys unique; empty rows dropped by caller via toMap().
  interface Props {
    legend: string;
    rows: { key: string; value: string }[];
    keyLabel?: string;
    valueLabel?: string;
    valuePlaceholder?: string;
    error?: string;
  }
  let {
    legend,
    rows = $bindable(),
    keyLabel = 'Campo',
    valueLabel = 'Valor',
    valuePlaceholder = '',
    error
  }: Props = $props();
  const uid = $props.id();

  function add() {
    rows = [...rows, { key: '', value: '' }];
  }
  function remove(i: number) {
    rows = rows.filter((_, j) => j !== i);
  }
</script>

<fieldset class="kv">
  <legend>{legend}</legend>
  {#each rows as row, i (i)}
    <div class="row">
      <label class="visually-hidden" for={`${uid}-k-${i}`}>{keyLabel} {i + 1}</label>
      <input id={`${uid}-k-${i}`} placeholder={keyLabel} bind:value={row.key} autocomplete="off" />
      <label class="visually-hidden" for={`${uid}-v-${i}`}>{valueLabel} {i + 1}</label>
      <input
        id={`${uid}-v-${i}`}
        placeholder={valuePlaceholder || valueLabel}
        bind:value={row.value}
        autocomplete="off"
        spellcheck="false"
      />
      <button class="btn btn-ghost btn-small" type="button" onclick={() => remove(i)}
        >Quitar<span class="visually-hidden"> fila {i + 1}</span></button
      >
    </div>
  {/each}
  <button class="btn btn-secondary btn-small" type="button" onclick={add}>Añadir</button>
  {#if error}<p class="error small" role="alert">{error}</p>{/if}
</fieldset>

<style>
  .kv {
    display: grid;
    gap: var(--space-2);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: var(--space-3);
  }
  .row {
    display: grid;
    grid-template-columns: minmax(6rem, 1fr) 2fr auto;
    gap: var(--space-2);
    align-items: center;
  }
  .kv > button {
    justify-self: start;
  }
  .error {
    color: var(--danger);
    margin: 0;
  }
</style>
