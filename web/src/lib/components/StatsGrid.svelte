<script lang="ts">
  // url counts by status for one site.
  import type { SiteStats } from '#lib/api/types.js';
  import { fmtDate, fmtNum } from '#lib/format.js';
  interface Props {
    stats: SiteStats;
  }
  let { stats }: Props = $props();
  const rows = $derived([
    ['Total URLs', fmtNum(stats.total_urls)],
    ['Pendientes', fmtNum(stats.pending)],
    ['En progreso', fmtNum(stats.in_progress)],
    ['Completadas', fmtNum(stats.completed)],
    ['Fallidas', fmtNum(stats.failed)],
    ['Items extraídos', fmtNum(stats.extracted_items)],
    ['Último crawl', fmtDate(stats.last_crawl)]
  ]);
</script>

<dl class="stats">
  {#each rows as [k, v] (k)}
    <div>
      <dt class="small muted">{k}</dt>
      <dd class="tnum">{v}</dd>
    </div>
  {/each}
</dl>

<style>
  .stats {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(8.5rem, 1fr));
    gap: var(--space-3);
    margin: 0;
  }
  dd {
    margin: 0;
    font-size: var(--fs-lg);
    font-weight: var(--fw-semibold);
  }
</style>
