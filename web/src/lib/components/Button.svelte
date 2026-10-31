<script lang="ts">
  // link when href, button otherwise. variants map to global .btn classes.
  import type { Snippet } from 'svelte';
  import type { HTMLAnchorAttributes, HTMLButtonAttributes } from 'svelte/elements';

  type Common = {
    variant?: 'primary' | 'secondary' | 'ghost' | 'danger';
    size?: 'small' | 'medium' | 'large';
    block?: boolean;
    children: Snippet;
  };
  type Props = Common &
    ((HTMLAnchorAttributes & { href: string }) | (HTMLButtonAttributes & { href?: undefined }));
  let { variant = 'primary', size = 'medium', block = false, children, ...rest }: Props = $props();
  const cls = $derived(
    [
      'btn',
      variant !== 'primary' && `btn-${variant}`,
      size !== 'medium' && `btn-${size}`,
      block && 'btn-block'
    ]
      .filter(Boolean)
      .join(' ')
  );
</script>

{#if rest.href !== undefined}
  <a class={cls} {...rest as HTMLAnchorAttributes}>{@render children()}</a>
{:else}
  <button class={cls} type="button" {...rest as HTMLButtonAttributes}>{@render children()}</button>
{/if}
