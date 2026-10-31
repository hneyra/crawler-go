<script lang="ts">
  import { goto } from '$app/navigation';
  import AsyncState from '#lib/components/AsyncState.svelte';
  import Badge from '#lib/components/Badge.svelte';
  import SiteForm from '#lib/components/SiteForm.svelte';
  import { api, CSRF, unwrap } from '#lib/api.js';
  import { errorMessage } from '#lib/api/problem.js';
  import type { Site, SiteInput } from '#lib/api/types.js';
  import { fmtDate } from '#lib/format.js';

  let sites = $state<Site[]>([]);
  let loading = $state(true);
  let error = $state<string | null>(null);
  let creating = $state(false);

  async function load() {
    loading = true;
    try {
      sites = (await unwrap(api().GET('/api/v1/sites'))).items;
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

  async function create(body: SiteInput) {
    const s = await unwrap(api().POST('/api/v1/sites', { params: { header: CSRF }, body }));
    await goto(`/sitios/${s.id}`);
  }
</script>

<svelte:head><title>Sitios | Crawler</title></svelte:head>

<div class="head">
  <h1>Sitios</h1>
  {#if !creating}<button class="btn btn-small" type="button" onclick={() => (creating = true)}
      >Nuevo sitio</button
    >{/if}
</div>

{#if creating}
  <section class="card">
    <h2>Nuevo sitio</h2>
    <SiteForm submitLabel="Crear sitio" onsave={create} oncancel={() => (creating = false)} />
  </section>
{/if}

<AsyncState
  {loading}
  {error}
  retry={load}
  empty={sites.length === 0}
  emptyText="Aún no hay sitios."
>
  <div class="table-wrap">
    <table class="data-table">
      <thead>
        <tr>
          <th scope="col">ID</th>
          <th scope="col">Nombre</th>
          <th scope="col">URL base</th>
          <th scope="col">Estado</th>
          <th scope="col">Estrategia</th>
          <th scope="col">Delay</th>
          <th scope="col">Creado</th>
        </tr>
      </thead>
      <tbody>
        {#each sites as s (s.id)}
          <tr>
            <td class="tnum">{s.id}</td>
            <td><a href={`/sitios/${s.id}`}>{s.name}</a></td>
            <td class="small url">{s.base_url}</td>
            <td
              ><Badge tone={s.enabled ? 'verified' : 'neutral'}
                >{s.enabled ? 'Activo' : 'Inactivo'}</Badge
              ></td
            >
            <td><code>{s.extraction_config.detail_strategy}</code></td>
            <td class="tnum">{s.politeness_delay_ms} ms</td>
            <td class="small">{fmtDate(s.created_at)}</td>
          </tr>
        {/each}
      </tbody>
    </table>
  </div>
</AsyncState>

<style>
  .head {
    display: flex;
    justify-content: space-between;
    align-items: center;
  }
  .url {
    overflow-wrap: anywhere;
  }
</style>
