<script lang="ts">
  import '#lib/styles/tokens.css';
  import '#lib/styles/global.css';
  import type { LayoutProps } from './$types';

  let { children }: LayoutProps = $props();
</script>

<a class="skip-link" href="#main">Saltar al contenido</a>
{@render children()}
