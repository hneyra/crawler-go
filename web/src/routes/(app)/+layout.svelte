<script lang="ts">
  import { goto } from '$app/navigation';
  import { page } from '$app/state';
  import AppShell from '#lib/components/AppShell.svelte';
  import Loading from '#lib/components/Loading.svelte';
  import { session } from '#lib/session.svelte.js';
  import type { LayoutProps } from './$types';

  let { children }: LayoutProps = $props();

  $effect(() => {
    void session.load();
  });
  $effect(() => {
    if (session.loaded && !session.current && !session.error) {
      const next = page.url.pathname + page.url.search;
      void goto(`/login?next=${encodeURIComponent(next)}`, { replaceState: true });
    }
  });

  const links = [
    { href: '/', label: 'Dashboard' },
    { href: '/sitios', label: 'Sitios' },
    { href: '/ejecuciones', label: 'Ejecuciones' },
    { href: '/items', label: 'Items' }
  ];
  const nav = $derived(
    links.map((l) => ({
      ...l,
      current: l.href === '/' ? page.url.pathname === '/' : page.url.pathname.startsWith(l.href)
    }))
  );

  async function logout() {
    await session.logout();
    await goto('/login');
  }
</script>

{#if !session.loaded}
  <main id="main" class="container"><Loading /></main>
{:else if session.error}
  <main id="main" class="container">
    <p class="alert alert-danger" role="alert">{session.error}</p>
    <button class="btn btn-secondary" type="button" onclick={() => session.load(true)}
      >Reintentar</button
    >
  </main>
{:else if session.current}
  <AppShell
    area="Administración"
    home="/"
    nav={session.current.user.is_admin ? nav : []}
    user={session.current.user.email}
    onlogout={logout}
  >
    {#if session.current.user.is_admin}
      {@render children()}
    {:else}
      <p class="alert alert-warn" role="alert">
        Tu usuario no es administrador. Pide acceso a un administrador (<code
          >crawler admin promote</code
        >).
      </p>
    {/if}
  </AppShell>
{/if}
