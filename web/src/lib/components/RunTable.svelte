<script lang="ts">
  import type { Run } from '#lib/api/types.js';
  import { RUN_KIND_LABEL, fmtDate, fmtDuration, runSummary } from '#lib/format.js';
  import RunBadge from './RunBadge.svelte';
  interface Props {
    runs: Run[];
    siteNames?: Record<number, string>;
  }
  let { runs, siteNames }: Props = $props();
</script>

<div class="table-wrap">
  <table class="data-table">
    <thead>
      <tr>
        <th scope="col">#</th>
        {#if siteNames}<th scope="col">Sitio</th>{/if}
        <th scope="col">Tipo</th>
        <th scope="col">Estado</th>
        <th scope="col">Creada</th>
        <th scope="col">Duración</th>
        <th scope="col">Resultado</th>
      </tr>
    </thead>
    <tbody>
      {#each runs as r (r.id)}
        <tr>
          <td class="tnum">{r.id}</td>
          {#if siteNames}<td
              ><a href={`/sitios/${r.site_id}`}>{siteNames[r.site_id] ?? r.site_id}</a></td
            >{/if}
          <td>{RUN_KIND_LABEL[r.kind] ?? r.kind}</td>
          <td><RunBadge status={r.status} /></td>
          <td class="small">{fmtDate(r.created_at)}</td>
          <td class="small tnum">{fmtDuration(r.started_at, r.finished_at)}</td>
          <td class="small">
            {runSummary(r.stats)}
            {#if r.error}<div class="err">{r.error}</div>{/if}
          </td>
        </tr>
      {/each}
    </tbody>
  </table>
</div>

<style>
  .err {
    color: var(--danger);
    overflow-wrap: anywhere;
  }
</style>
