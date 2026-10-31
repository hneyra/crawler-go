<script lang="ts">
  interface Props {
    title?: string;
    message: string;
  }
  let { title = 'No disponible', message }: Props = $props();
</script>

<div class="alert alert-warn" role="status">
  <p><strong>{title}.</strong> {message}</p>
</div>
