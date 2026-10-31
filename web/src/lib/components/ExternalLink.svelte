<script lang="ts">
  // link to crawled/configured url. only http(s) is clickable; anything else = plain text.
  interface Props {
    href: string;
  }
  let { href }: Props = $props();
  const safe = $derived.by(() => {
    try {
      const u = new URL(href);
      return u.protocol === 'http:' || u.protocol === 'https:';
    } catch {
      return false;
    }
  });
</script>

{#if safe}<a {href} rel="noopener noreferrer nofollow" target="_blank">{href}</a>{:else}{href}{/if}
