<script lang="ts">
  import { goto } from '$app/navigation';
  import { page } from '$app/state';
  import Field from '#lib/components/Field.svelte';
  import FormError from '#lib/components/FormError.svelte';
  import Logo from '#lib/components/Logo.svelte';
  import { api, safeNext, unwrap } from '#lib/api.js';
  import { session } from '#lib/session.svelte.js';
  import { ApiError, authErrorMessage, fieldErrorMap } from '#lib/api/problem.js';

  let email = $state('');
  let password = $state('');
  let busy = $state(false);
  let error = $state<string | null>(null);
  let fields = $state<Record<string, string>>({});

  async function submit(e: SubmitEvent) {
    e.preventDefault();
    busy = true;
    error = null;
    fields = {};
    try {
      const s = await unwrap(api().POST('/api/v1/auth/login', { body: { email, password } }));
      session.set(s);
      await goto(safeNext(page.url.searchParams.get('next')));
    } catch (err) {
      fields = fieldErrorMap(err);
      error =
        err instanceof ApiError && err.status === 401
          ? 'Email o contraseña incorrectos.'
          : authErrorMessage(err);
    } finally {
      busy = false;
    }
  }
</script>

<svelte:head><title>Entrar | Crawler</title></svelte:head>

<main id="main" class="login container">
  <div class="card">
    <Logo />
    <h1>Entrar</h1>
    <p class="muted small">Panel de administración del crawler.</p>
    <form onsubmit={submit} novalidate>
      <FormError {error} />
      <Field
        label="Email"
        name="email"
        type="email"
        autocomplete="email"
        required
        bind:value={email}
        error={fields.email}
      />
      <Field
        label="Contraseña"
        name="password"
        type="password"
        autocomplete="current-password"
        required
        bind:value={password}
        error={fields.password}
      />
      <button class="btn btn-block" type="submit" disabled={busy}
        >{busy ? 'Entrando…' : 'Entrar'}</button
      >
    </form>
  </div>
</main>

<style>
  .login {
    max-width: 26rem;
    padding-block: var(--space-8);
  }
  form {
    display: grid;
    gap: var(--space-4);
    margin-top: var(--space-4);
  }
</style>
