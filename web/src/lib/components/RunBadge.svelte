<script lang="ts">
  import Badge from './Badge.svelte';
  import { RUN_STATUS_LABEL, runTone } from '#lib/format.js';
  interface Props {
    status: string;
  }
  let { status }: Props = $props();
</script>

<Badge tone={runTone(status)}>{RUN_STATUS_LABEL[status] ?? status}</Badge>
