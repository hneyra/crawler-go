<script lang="ts">
  import { untrack } from 'svelte';
  import { page } from '$app/state';
  import AsyncState from '#lib/components/AsyncState.svelte';
  import Pager from '#lib/components/Pager.svelte';
  import RunTable from '#lib/components/RunTable.svelte';
  import { api, unwrap } from '#lib/api.js';
  import type { Run, Site } from '#lib/api/types.js';
  import { Paged } from '#lib/paged.svelte.js';

  let sites = $state<Site[]>([]);
  let siteId = $state(page.url.searchParams.get('site_id') ?? '');
  const siteNames = $derived(Object.fromEntries(sites.map((s) => [s.id, s.name])));

  const list = new Paged<Run>((p) =>
    unwrap(
      api().GET('/api/v1/runs', {
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

<svelte:head><title>Ejecuciones | Crawler</title></svelte:head>

<h1>Ejecuciones</h1>
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
  <button class="btn btn-secondary btn-small" type="submit">Refrescar</button>
</form>

<AsyncState
  loading={list.loading}
  error={list.error}
  retry={() => list.load()}
  empty={list.items.length === 0}
  emptyText="No hay ejecuciones."
>
  <RunTable runs={list.items} {siteNames} />
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
</style>
