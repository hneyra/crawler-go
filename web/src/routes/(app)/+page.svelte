<script lang="ts">
  // dashboard: one card per site with stats + quick actions.
  import AsyncState from '#lib/components/AsyncState.svelte';
  import Badge from '#lib/components/Badge.svelte';
  import FormError from '#lib/components/FormError.svelte';
  import StatsGrid from '#lib/components/StatsGrid.svelte';
  import { api, unwrap } from '#lib/api.js';
  import { errorMessage } from '#lib/api/problem.js';
  import type { Site, SiteStats } from '#lib/api/types.js';
  import { queuedMessage, runSite, type SiteAction } from '#lib/actions.js';

  let sites = $state<Site[]>([]);
  let stats = $state<Record<number, SiteStats>>({});
  let loading = $state(true);
  let error = $state<string | null>(null);
  let notice = $state<string | null>(null);
  let actionError = $state<string | null>(null);
  let busy = $state<string | null>(null);

  async function load() {
    loading = true;
    try {
      sites = (await unwrap(api().GET('/api/v1/sites'))).items;
      const all = await Promise.all(
        sites.map((s) =>
          unwrap(api().GET('/api/v1/sites/{siteId}/stats', { params: { path: { siteId: s.id } } }))
        )
      );
      stats = Object.fromEntries(all.map((st) => [st.site_id, st]));
      error = null;
    } catch (e) {
      error = errorMessage(e);
    } finally {
      loading = false;
    }
  }
  $effect(() => {
    void load();
  });

  async function act(site: Site, action: SiteAction) {
    busy = `${site.id}:${action}`;
    notice = null;
    actionError = null;
    try {
      notice = `${site.name}: ${queuedMessage(await runSite(site.id, action))}`;
    } catch (e) {
      actionError = errorMessage(e);
    } finally {
      busy = null;
    }
  }
</script>

<svelte:head><title>Dashboard | Crawler</title></svelte:head>

<div class="head">
  <h1>Dashboard</h1>
  <button class="btn btn-secondary btn-small" type="button" onclick={load} disabled={loading}
    >Refrescar</button
  >
</div>
<FormError error={actionError} success={notice} />

<AsyncState
  {loading}
  {error}
  retry={load}
  empty={sites.length === 0}
  emptyText="No hay sitios. Crea uno en Sitios o carga sites-config.yml con `crawler sync-sites`."
>
  <div class="grid">
    {#each sites as s (s.id)}
      <section class="card">
        <header class="card-head">
          <h2><a href={`/sitios/${s.id}`}>{s.name}</a></h2>
          <Badge tone={s.enabled ? 'verified' : 'neutral'}
            >{s.enabled ? 'Activo' : 'Inactivo'}</Badge
          >
        </header>
        <p class="small muted url">{s.base_url}</p>
        {#if stats[s.id]}<StatsGrid stats={stats[s.id]!} />{/if}
        <div class="actions">
          {#each [['crawl', 'Crawl'], ['discover', 'Discovery'], ['extract', 'Extracción']] as const as [a, label] (a)}
            <button
              class="btn btn-small"
              class:btn-secondary={a !== 'crawl'}
              type="button"
              disabled={busy !== null}
              onclick={() => act(s, a)}
              >{busy === `${s.id}:${a}` ? 'Encolando…' : label}<span class="visually-hidden">
                de {s.name}</span
              ></button
            >
          {/each}
        </div>
      </section>
    {/each}
  </div>
</AsyncState>

<style>
  .head {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: var(--space-3);
  }
  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(min(100%, 22rem), 1fr));
    gap: var(--space-4);
  }
  .card-head {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: var(--space-2);
  }
  .card-head h2 {
    margin: 0;
    font-size: var(--fs-lg);
  }
  .url {
    overflow-wrap: anywhere;
  }
  .actions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-2);
    margin-top: var(--space-4);
  }
</style>
