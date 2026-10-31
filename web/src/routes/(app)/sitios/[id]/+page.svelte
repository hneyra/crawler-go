<script lang="ts">
  import { goto } from '$app/navigation';
  import { page } from '$app/state';
  import ExternalLink from '#lib/components/ExternalLink.svelte';
  import AsyncState from '#lib/components/AsyncState.svelte';
  import Badge from '#lib/components/Badge.svelte';
  import CategoryForm from '#lib/components/CategoryForm.svelte';
  import FormError from '#lib/components/FormError.svelte';
  import RunTable from '#lib/components/RunTable.svelte';
  import SiteForm from '#lib/components/SiteForm.svelte';
  import StatsGrid from '#lib/components/StatsGrid.svelte';
  import { api, CSRF, unwrap } from '#lib/api.js';
  import { errorMessage } from '#lib/api/problem.js';
  import type { Category, CategoryInput, Run, Site, SiteInput, SiteStats } from '#lib/api/types.js';
  import { queuedMessage, runCategory, runSite, type SiteAction } from '#lib/actions.js';

  const siteId = $derived(Number(page.params.id));

  let site = $state<Site | null>(null);
  let stats = $state<SiteStats | null>(null);
  let categories = $state<Category[]>([]);
  let runs = $state<Run[]>([]);
  let loading = $state(true);
  let error = $state<string | null>(null);
  let notice = $state<string | null>(null);
  let actionError = $state<string | null>(null);
  let editingSite = $state(false);
  let newCategory = $state(false);
  let editingCat = $state<number | null>(null);
  let busy = $state(false);

  async function loadRuns() {
    runs = (
      await unwrap(
        api().GET('/api/v1/runs', { params: { query: { site_id: siteId, per_page: 10 } } })
      )
    ).items;
    stats = await unwrap(
      api().GET('/api/v1/sites/{siteId}/stats', { params: { path: { siteId } } })
    );
  }

  async function load() {
    loading = true;
    try {
      const path = { params: { path: { siteId } } };
      site = await unwrap(api().GET('/api/v1/sites/{siteId}', path));
      categories = (await unwrap(api().GET('/api/v1/sites/{siteId}/categories', path))).items;
      await loadRuns();
      error = null;
    } catch (e) {
      error = errorMessage(e);
    } finally {
      loading = false;
    }
  }
  $effect(() => {
    void siteId;
    void load();
  });

  // poll while something runs. stops when idle or page left.
  const active = $derived(runs.some((r) => r.status === 'QUEUED' || r.status === 'RUNNING'));
  $effect(() => {
    if (!active) return;
    const t = setInterval(() => void loadRuns().catch(() => {}), 3000);
    return () => clearInterval(t);
  });

  async function guarded(fn: () => Promise<string | void>) {
    busy = true;
    notice = null;
    actionError = null;
    try {
      notice = (await fn()) || null;
    } catch (e) {
      actionError = errorMessage(e);
    } finally {
      busy = false;
    }
  }

  const act = (a: SiteAction) =>
    guarded(async () => {
      const r = await runSite(siteId, a);
      await loadRuns().catch(() => {}); // run is queued; refresh failure is not the action's
      return queuedMessage(r);
    });

  async function saveSite(body: SiteInput) {
    site = await unwrap(
      api().PATCH('/api/v1/sites/{siteId}', { params: { path: { siteId }, header: CSRF }, body })
    );
    editingSite = false;
    notice = 'Sitio guardado.';
  }

  const deleteSite = () =>
    guarded(async () => {
      if (!site || !confirm(`¿Borrar "${site.name}" con sus categorías, URLs e items?`)) return;
      await unwrap(
        api().DELETE('/api/v1/sites/{siteId}', { params: { path: { siteId }, header: CSRF } })
      );
      await goto('/sitios');
    });

  async function createCategory(body: CategoryInput) {
    const c = await unwrap(
      api().POST('/api/v1/sites/{siteId}/categories', {
        params: { path: { siteId }, header: CSRF },
        body
      })
    );
    categories = [...categories, c];
    newCategory = false;
    notice = 'Categoría creada.';
  }

  async function saveCategory(id: number, body: CategoryInput) {
    const c = await unwrap(
      api().PATCH('/api/v1/categories/{categoryId}', {
        params: { path: { categoryId: id }, header: CSRF },
        body
      })
    );
    categories = categories.map((x) => (x.id === id ? c : x));
    editingCat = null;
    notice = 'Categoría guardada.';
  }

  const deleteCategory = (c: Category) =>
    guarded(async () => {
      if (!confirm(`¿Borrar la categoría "${c.name}" y sus URLs?`)) return;
      await unwrap(
        api().DELETE('/api/v1/categories/{categoryId}', {
          params: { path: { categoryId: c.id }, header: CSRF }
        })
      );
      categories = categories.filter((x) => x.id !== c.id);
      return 'Categoría borrada.';
    });

  const discoverCategory = (c: Category) =>
    guarded(async () => {
      const r = await runCategory(c.id);
      await loadRuns().catch(() => {});
      return queuedMessage(r);
    });
</script>

<svelte:head><title>{site?.name ?? 'Sitio'} | Crawler</title></svelte:head>

<p class="small"><a href="/sitios">← Sitios</a></p>
<AsyncState {loading} {error} retry={load}>
  {#if site}
    <div class="head">
      <h1>{site.name}</h1>
      <Badge tone={site.enabled ? 'verified' : 'neutral'}
        >{site.enabled ? 'Activo' : 'Inactivo'}</Badge
      >
    </div>
    <p class="small muted url">
      {site.base_url} · <code>{site.extraction_config.detail_strategy}</code> · delay {site.politeness_delay_ms}
      ms
    </p>
    <FormError error={actionError} success={notice} />

    <div class="actions">
      <button class="btn btn-small" type="button" disabled={busy} onclick={() => act('crawl')}
        >Crawl completo</button
      >
      <button
        class="btn btn-secondary btn-small"
        type="button"
        disabled={busy}
        onclick={() => act('discover')}>Discovery</button
      >
      <button
        class="btn btn-secondary btn-small"
        type="button"
        disabled={busy}
        onclick={() => act('extract')}>Extracción</button
      >
      <button
        class="btn btn-ghost btn-small"
        type="button"
        onclick={() => (editingSite = !editingSite)}
        >{editingSite ? 'Cerrar edición' : 'Editar sitio'}</button
      >
      <button class="btn btn-danger btn-small" type="button" disabled={busy} onclick={deleteSite}
        >Borrar sitio</button
      >
    </div>

    {#if editingSite}
      <section class="card">
        <h2>Editar sitio</h2>
        {#key site.updated_at}
          <SiteForm
            {site}
            submitLabel="Guardar"
            onsave={saveSite}
            oncancel={() => (editingSite = false)}
          />
        {/key}
      </section>
    {/if}

    {#if stats}
      <section class="card">
        <h2>Estadísticas</h2>
        <StatsGrid {stats} />
      </section>
    {/if}

    <section class="card">
      <div class="head">
        <h2>Categorías</h2>
        {#if !newCategory}<button
            class="btn btn-small"
            type="button"
            onclick={() => (newCategory = true)}>Nueva categoría</button
          >{/if}
      </div>
      {#if newCategory}
        <CategoryForm
          submitLabel="Crear categoría"
          onsave={createCategory}
          oncancel={() => (newCategory = false)}
        />
      {/if}
      {#if categories.length === 0}
        <p class="muted">Sin categorías. Añade una página de listado para empezar el discovery.</p>
      {:else}
        <div class="table-wrap">
          <table class="data-table">
            <thead>
              <tr>
                <th scope="col">ID</th>
                <th scope="col">Nombre</th>
                <th scope="col">URL</th>
                <th scope="col">Paginación</th>
                <th scope="col">Selector</th>
                <th scope="col">Estado</th>
                <th scope="col"><span class="visually-hidden">Acciones</span></th>
              </tr>
            </thead>
            <tbody>
              {#each categories as c (c.id)}
                <tr>
                  <td class="tnum">{c.id}</td>
                  <td>{c.name}</td>
                  <td class="small url"><ExternalLink href={c.url} /></td>
                  <td><code>{c.pagination_type}</code></td>
                  <td><code>{c.item_link_selector}</code></td>
                  <td
                    ><Badge tone={c.enabled ? 'verified' : 'neutral'}
                      >{c.enabled ? 'Habilitada' : 'Deshabilitada'}</Badge
                    ></td
                  >
                  <td class="row-actions">
                    <button
                      class="btn btn-secondary btn-small"
                      type="button"
                      disabled={busy}
                      onclick={() => discoverCategory(c)}
                      >Discovery<span class="visually-hidden"> de {c.name}</span></button
                    >
                    <button
                      class="btn btn-ghost btn-small"
                      type="button"
                      onclick={() => (editingCat = editingCat === c.id ? null : c.id)}
                      >Editar<span class="visually-hidden"> {c.name}</span></button
                    >
                    <button
                      class="btn btn-ghost btn-small"
                      type="button"
                      disabled={busy}
                      onclick={() => deleteCategory(c)}
                      >Borrar<span class="visually-hidden"> {c.name}</span></button
                    >
                  </td>
                </tr>
                {#if editingCat === c.id}
                  <tr>
                    <td colspan="7">
                      <CategoryForm
                        category={c}
                        submitLabel="Guardar categoría"
                        onsave={(b) => saveCategory(c.id, b)}
                        oncancel={() => (editingCat = null)}
                      />
                    </td>
                  </tr>
                {/if}
              {/each}
            </tbody>
          </table>
        </div>
      {/if}
    </section>

    <section class="card">
      <div class="head">
        <h2>Ejecuciones recientes</h2>
        <a class="small" href={`/ejecuciones?site_id=${site.id}`}>Ver todas</a>
      </div>
      {#if runs.length === 0}
        <p class="muted">Todavía no hay ejecuciones.</p>
      {:else}
        <RunTable {runs} />
      {/if}
    </section>
  {/if}
</AsyncState>

<style>
  .head {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: var(--space-3);
  }
  .actions,
  .row-actions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-2);
    margin-block: var(--space-3);
  }
  .url {
    overflow-wrap: anywhere;
  }
  section {
    margin-top: var(--space-4);
  }
</style>
