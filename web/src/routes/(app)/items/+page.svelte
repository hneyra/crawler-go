<script lang="ts">
  import { untrack } from 'svelte';
  import { page } from '$app/state';
  import ExternalLink from '#lib/components/ExternalLink.svelte';
  import AsyncState from '#lib/components/AsyncState.svelte';
  import Pager from '#lib/components/Pager.svelte';
  import PropsTree from '#lib/components/PropsTree.svelte';
  import { api, unwrap } from '#lib/api.js';
  import type { Item, Site } from '#lib/api/types.js';
  import { fmtDate } from '#lib/format.js';
  import { Paged } from '#lib/paged.svelte.js';

  let sites = $state<Site[]>([]);
  let siteId = $state(page.url.searchParams.get('site_id') ?? '');
  let open = $state<Record<number, boolean>>({});

  const list = new Paged<Item>((p) =>
    unwrap(
      api().GET('/api/v1/items', {
        params: { query: { site_id: siteId ? Number(siteId) : undefined, page: p, per_page: 25 } }
      })
    )
  );

  // mount only. untrack: load() reads siteId -> effect would re-run on every filter change.
  $effect(() => {
    untrack(() => {
      void unwrap(api().GET('/api/v1/sites')).then(
        (r) => (sites = r.items),
        () => {}
      );
      void list.load(1);
    });
  });
</script>

<svelte:head><title>Items | Crawler</title></svelte:head>

<h1>Items extraídos</h1>
<form
  class="filters"
  onsubmit={(e) => {
    e.preventDefault();
    void list.load(1);
  }}
>
  <label for="site">Sitio</label>
  <select
    id="site"
    value={siteId}
    onchange={(e) => {
      // set filter first, then reload (bind + onchange order is not guaranteed).
      siteId = e.currentTarget.value;
      void list.load(1);
    }}
  >
    <option value="">Todos</option>
    {#each sites as s (s.id)}<option value={String(s.id)}>{s.name}</option>{/each}
  </select>
  <span class="small muted">{list.total} items</span>
</form>

<AsyncState
  loading={list.loading}
  error={list.error}
  retry={() => list.load()}
  empty={list.items.length === 0}
  emptyText="No hay items extraídos."
>
  <div class="table-wrap">
    <table class="data-table">
      <thead>
        <tr>
          <th scope="col">ID</th>
          <th scope="col">Título</th>
          <th scope="col">URL</th>
          <th scope="col">Sitio</th>
          <th scope="col">Extraído</th>
          <th scope="col">Propiedades</th>
        </tr>
      </thead>
      <tbody>
        {#each list.items as it (it.id)}
          <tr>
            <td class="tnum">{it.id}</td>
            <td>{it.title || '—'}</td>
            <td class="small url"><ExternalLink href={it.url} /></td>
            <td><a href={`/sitios/${it.site_id}`}>{it.site_name}</a></td>
            <td class="small">{fmtDate(it.extracted_at)}</td>
            <td>
              <button
                class="btn btn-ghost btn-small"
                type="button"
                aria-expanded={!!open[it.id]}
                onclick={() => (open[it.id] = !open[it.id])}
                >{open[it.id] ? 'Ocultar' : 'Ver'} ({Object.keys(it.properties).length})<span
                  class="visually-hidden"
                >
                  propiedades de {it.title}</span
                ></button
              >
            </td>
          </tr>
          {#if open[it.id]}
            <tr><td colspan="6"><PropsTree value={it.properties} /></td></tr>
          {/if}
        {/each}
      </tbody>
    </table>
  </div>
  <Pager page={list.page} totalPages={list.totalPages} go={(p) => list.load(p)} />
</AsyncState>

<style>
  .filters {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-2);
    align-items: center;
    margin-bottom: var(--space-4);
  }
  .url {
    overflow-wrap: anywhere;
    max-width: 24rem;
  }
</style>
