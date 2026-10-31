<script lang="ts">
  // async error/success region. polite live region; focusable for keyboard users.
  interface Props {
    error?: string | null;
    success?: string | null;
  }
  let { error = null, success = null }: Props = $props();
</script>

<div aria-live="polite" class="form-msg">
  {#if error}
    <div class="alert alert-danger" role="alert">{error}</div>
  {:else if success}
    <div class="alert alert-success">{success}</div>
  {/if}
</div>
