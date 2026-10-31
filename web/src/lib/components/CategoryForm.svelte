<script lang="ts">
  import { untrack } from 'svelte';
  import type {
    Category,
    CategoryInput,
    LinkExtractionType,
    PaginationType
  } from '#lib/api/types.js';
  import { fieldErrorMap, errorMessage } from '#lib/api/problem.js';
  import Field from './Field.svelte';
  import FormError from './FormError.svelte';

  interface Props {
    category?: Category | null;
    submitLabel: string;
    onsave: (body: CategoryInput) => Promise<void>;
    oncancel?: () => void;
  }
  let { category = null, submitLabel, onsave, oncancel }: Props = $props();
  const uid = $props.id();

  const c = untrack(() => category);
  let name = $state(c?.name ?? '');
  let url = $state(c?.url ?? '');
  let pagination = $state<PaginationType>(c?.pagination_type ?? 'PAGE_PARAM');
  let itemSel = $state(c?.item_link_selector ?? '');
  let nextSel = $state(c?.next_page_selector ?? '');
  let linkType = $state<LinkExtractionType>(c?.link_extraction_type ?? 'HREF');
  let linkAttr = $state(c?.link_attribute ?? '');
  let enabled = $state(c?.enabled ?? true);

  let busy = $state(false);
  let error = $state<string | null>(null);
  let fields = $state<Record<string, string>>({});

  async function submit(e: SubmitEvent) {
    e.preventDefault();
    busy = true;
    error = null;
    fields = {};
    try {
      await onsave({
        name,
        url,
        pagination_type: pagination,
        item_link_selector: itemSel,
        next_page_selector: nextSel,
        link_extraction_type: linkType,
        link_attribute: linkType === 'DATA_ATTRIBUTE' ? linkAttr : '',
        enabled
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
    label="URL del listado"
    name="url"
    type="url"
    required
    bind:value={url}
    error={fields.url}
  />
  <div class="field">
    <label for={`${uid}-pag`}>Paginación</label>
    <select id={`${uid}-pag`} bind:value={pagination}>
      <option value="PAGE_PARAM">PAGE_PARAM (enlace a siguiente página)</option>
      <option value="CURSOR">CURSOR (enlace con cursor)</option>
      <option value="SCROLL_AJAX">SCROLL_AJAX (scroll infinito, Chrome)</option>
    </select>
  </div>
  <Field
    label="Selector de items"
    name="item_link_selector"
    required
    bind:value={itemSel}
    error={fields.item_link_selector}
    hint="Ej.: a.product-link"
  />
  {#if pagination !== 'SCROLL_AJAX'}
    <Field
      label="Selector de siguiente página (opcional)"
      name="next_page_selector"
      bind:value={nextSel}
      error={fields.next_page_selector}
      hint="Vacío = automático (rel=next, «Siguiente», ›, »)"
    />
  {/if}
  <div class="field">
    <label for={`${uid}-link`}>Dónde está el enlace</label>
    <select id={`${uid}-link`} bind:value={linkType}>
      <option value="HREF">HREF</option>
      <option value="ONCLICK">ONCLICK (JavaScript)</option>
      <option value="DATA_ATTRIBUTE">DATA_ATTRIBUTE</option>
    </select>
  </div>
  {#if linkType === 'DATA_ATTRIBUTE'}
    <Field
      label="Atributo"
      name="link_attribute"
      required
      bind:value={linkAttr}
      error={fields.link_attribute}
      hint="Ej.: data-to-posting"
    />
  {/if}
  <label class="check"><input type="checkbox" bind:checked={enabled} /> Habilitada</label>
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
  select {
    width: 100%;
  }
</style>
