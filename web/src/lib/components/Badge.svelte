<script lang="ts">
  // status pill. text must say the state; tone only reinforces it.
  import type { Snippet } from 'svelte';
  interface Props {
    tone?: 'best' | 'verified' | 'neutral' | 'warn' | 'danger' | 'info' | 'demo';
    title?: string;
    children: Snippet;
  }
  let { tone = 'neutral', title, children }: Props = $props();
  const cls: Record<NonNullable<Props['tone']>, string> = {
    best: 'badge-best',
    verified: 'badge-verified',
    neutral: 'badge-declared',
    warn: 'badge-stale',
    danger: 'badge-danger',
    info: 'badge-info',
    demo: 'badge-demo'
  };
</script>

<span class={`badge ${cls[tone]}`} {title}>{@render children()}</span>
