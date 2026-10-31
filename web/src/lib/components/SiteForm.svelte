<script lang="ts">
  import { untrack } from 'svelte';
  // create/edit site + extraction rules. errors: field map from backend problem.
  import type { DetailStrategy, Site, SiteInput } from '#lib/api/types.js';
  import { fieldErrorMap, errorMessage } from '#lib/api/problem.js';
  import { linesToList } from '#lib/format.js';
  import { toMap, toRows } from '#lib/kv.js';
  import Field from './Field.svelte';
  import FormError from './FormError.svelte';
  import KeyValueEditor from './KeyValueEditor.svelte';

  interface Props {
    site?: Site | null;
    submitLabel: string;
    onsave: (body: SiteInput) => Promise<void>;
    oncancel?: () => void;
  }
  let { site = null, submitLabel, onsave, oncancel }: Props = $props();
  const uid = $props.id();

  // form copy of props at mount; parent remounts ({#key}) to reset.
  const s = untrack(() => site);
  let name = $state(s?.name ?? '');
  let baseUrl = $state(s?.base_url ?? '');
  let enabled = $state(s?.enabled ?? true);
  let delay = $state(String(s?.politeness_delay_ms ?? 1000));
  let strategy = $state<DetailStrategy>(s?.extraction_config.detail_strategy ?? 'HTML');
  let selectors = $state(toRows(s?.extraction_config.field_selectors));
  let jsonPaths = $state(toRows(s?.extraction_config.json_paths));
  let scripts = $state((s?.extraction_config.script_patterns ?? []).join('\n'));
  let intercepts = $state((s?.extraction_config.intercept_patterns ?? []).join('\n'));

  let busy = $state(false);
  let error = $state<string | null>(null);
  let fields = $state<Record<string, string>>({});

  async function submit(e: SubmitEvent) {
    e.preventDefault();
    busy = true;
    error = null;
    fields = {};
    const d = Number(delay);
    try {
      await onsave({
        name,
        base_url: baseUrl,
        enabled,
        politeness_delay_ms: Number.isFinite(d) ? Math.trunc(d) : 1000,
        extraction_config: {
          detail_strategy: strategy,
          field_selectors: toMap(selectors),
          json_paths: toMap(jsonPaths),
          script_patterns: linesToList(scripts),
          intercept_patterns: linesToList(intercepts)
        }
      });
    } catch (err) {
      fields = fieldErrorMap(err);
      error = errorMessage(err);
    } finally {
      busy = false;
    }
  }
</script>

<form class="form" onsubmit={submit} novalidate>
  <FormError {error} />
  <Field label="Nombre" name="name" required bind:value={name} error={fields.name} />
  <Field
    label="URL base"
    name="base_url"
    type="url"
    required
    bind:value={baseUrl}
    error={fields.base_url}
    hint="Ej.: https://www.tienda.pe"
  />
  <Field
    label="Delay entre peticiones (ms)"
    name="politeness_delay_ms"
    type="number"
    min="0"
    max="600000"
    bind:value={delay}
    error={fields.politeness_delay_ms}
  />
  <label class="check"><input type="checkbox" bind:checked={enabled} /> Habilitado</label>

  <div class="field">
    <label for={`${uid}-strategy`}>Estrategia de extracción</label>
    <select id={`${uid}-strategy`} bind:value={strategy}>
      <option value="HTML">HTML (selectores CSS)</option>
      <option value="SCRIPT_JSON">SCRIPT_JSON (regex en &lt;script&gt; + JSONPath)</option>
      <option value="AJAX">AJAX (respuestas XHR + JSONPath)</option>
    </select>
    {#if fields.detail_strategy}<p class="error">{fields.detail_strategy}</p>{/if}
  </div>

  <KeyValueEditor
    legend="Selectores CSS (campo → selector)"
    bind:rows={selectors}
    valuePlaceholder="h1.product-title"
    error={fields.field_selectors}
  />
  <KeyValueEditor
    legend="JSONPaths (campo → ruta)"
    bind:rows={jsonPaths}
    valuePlaceholder="$.units[0].title"
    error={fields.json_paths}
  />

  {#if strategy === 'SCRIPT_JSON'}
    <div class="field">
      <label for={`${uid}-scripts`}>Patrones de script (regex, uno por línea; grupo 1 = JSON)</label
      >
      <textarea id={`${uid}-scripts`} rows="3" spellcheck="false" bind:value={scripts}></textarea>
      {#if fields.script_patterns}<p class="error">{fields.script_patterns}</p>{/if}
    </div>
  {/if}
  {#if strategy === 'AJAX'}
    <div class="field">
      <label for={`${uid}-intercepts`}>Patrones de intercepción (glob, uno por línea)</label>
      <textarea id={`${uid}-intercepts`} rows="3" spellcheck="false" bind:value={intercepts}
      ></textarea>
      <p class="hint">Ej.: <code>*/api/product/*</code></p>
      {#if fields.intercept_patterns}<p class="error">{fields.intercept_patterns}</p>{/if}
    </div>
  {/if}

  <div class="actions">
    <button class="btn" type="submit" disabled={busy}>{busy ? 'Guardando…' : submitLabel}</button>
    {#if oncancel}<button class="btn btn-ghost" type="button" onclick={oncancel}>Cancelar</button
      >{/if}
  </div>
</form>

<style>
  .form {
    display: grid;
    gap: var(--space-4);
  }
  .check {
    display: inline-flex;
    gap: var(--space-2);
    align-items: center;
  }
  .actions {
    display: flex;
    gap: var(--space-2);
  }
  textarea,
  select {
    width: 100%;
  }
</style>
