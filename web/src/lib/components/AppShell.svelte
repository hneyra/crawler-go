<script lang="ts">
  // shell for the admin spa.
  import type { Snippet } from 'svelte';
  import Logo from './Logo.svelte';

  interface Props {
    area: string;
    home: string;
    nav: { href: string; label: string; current?: boolean }[];
    user?: string | null;
    onlogout?: () => void;
    children: Snippet;
  }
  let { area, home, nav, user = null, onlogout, children }: Props = $props();
</script>

<svelte:head>
  <meta name="robots" content="noindex, nofollow" />
</svelte:head>

<header class="shell-head">
  <div class="container bar">
    <span class="brand"
      ><Logo href={home} label={`Crawler, ${area}: inicio`} /><span class="area">{area}</span></span
    >
    {#if user}
      <div class="user small">
        <span>{user}</span>
        <button class="link-button" type="button" onclick={onlogout}>Cerrar sesión</button>
      </div>
    {/if}
  </div>
  {#if nav.length > 0}
    <nav aria-label={`Navegación de ${area}`} class="container">
      <ul>
        {#each nav as n (n.href)}
          <li><a href={n.href} aria-current={n.current ? 'page' : undefined}>{n.label}</a></li>
        {/each}
      </ul>
    </nav>
  {/if}
</header>
<main id="main" class="container shell-main">
  {@render children()}
</main>

<style>
  .shell-head {
    position: sticky;
    top: 0;
    z-index: var(--z-header);
    background: var(--header-bg);
    border-bottom: 1px solid var(--border);
  }
  .bar {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-2) var(--space-4);
    justify-content: space-between;
    align-items: center;
    padding-block: var(--space-2);
  }
  .brand {
    display: inline-flex;
    align-items: center;
    gap: var(--space-3);
  }
  .area {
    color: var(--text-muted);
    font-size: var(--fs-sm);
    font-weight: var(--fw-medium);
    padding-left: var(--space-3);
    border-left: 1px solid var(--border);
  }
  .user {
    display: flex;
    gap: var(--space-3);
    align-items: center;
    color: var(--text-muted);
  }
  nav ul {
    list-style: none;
    display: flex;
    gap: var(--space-4);
    padding: 0;
    margin: 0 auto;
    max-width: var(--content);
    overflow-x: auto;
    scrollbar-width: none;
    font-size: var(--fs-sm);
  }
  nav a {
    display: inline-flex;
    align-items: center;
    min-height: var(--tap);
    color: var(--text-subtle);
    text-decoration: none;
    border-bottom: 2px solid transparent;
    margin-bottom: -1px;
    white-space: nowrap;
    transition:
      color var(--duration) var(--ease),
      border-color var(--duration) var(--ease);
  }
  nav a:hover {
    color: var(--text);
  }
  nav a[aria-current] {
    color: var(--text);
    font-weight: var(--fw-medium);
    border-bottom-color: var(--brand);
  }
  .shell-main {
    padding-block: var(--space-6);
  }
</style>
