import type { FieldError, Problem } from './types.js';

// api error w/ rfc 9457 body when present.
export class ApiError extends Error {
  readonly status: number;
  readonly problem: Problem | null;

  constructor(status: number, problem: Problem | null, fallback = 'Error inesperado') {
    super(problem?.detail || problem?.title || fallback);
    this.name = 'ApiError';
    this.status = status;
    this.problem = problem;
  }

  get code(): string | undefined {
    return this.problem?.code;
  }

  get fieldErrors(): FieldError[] {
    return this.problem?.errors ?? [];
  }
}

// loose check. body from server = untrusted shape.
export function asProblem(body: unknown, status: number): Problem | null {
  if (!body || typeof body !== 'object') return null;
  const b = body as Record<string, unknown>;
  if (typeof b.title !== 'string') return null;
  const errors = Array.isArray(b.errors)
    ? b.errors
        .filter(
          (e): e is FieldError =>
            !!e &&
            typeof e === 'object' &&
            typeof (e as FieldError).field === 'string' &&
            typeof (e as FieldError).message === 'string'
        )
        .map((e) => ({ field: e.field, code: String(e.code ?? ''), message: e.message }))
    : undefined;
  return {
    type: typeof b.type === 'string' ? b.type : 'about:blank',
    title: b.title,
    status: typeof b.status === 'number' ? b.status : status,
    detail: typeof b.detail === 'string' ? b.detail : undefined,
    code: typeof b.code === 'string' ? b.code : undefined,
    errors
  };
}

// field -> first message. for mapping to inputs.
export function fieldErrorMap(err: unknown): Record<string, string> {
  const out: Record<string, string> = {};
  if (err instanceof ApiError) {
    for (const fe of err.fieldErrors) {
      // "items[0].price" -> keep full key + leaf key
      if (!(fe.field in out)) out[fe.field] = fe.message;
      const leaf = fe.field.split('.').pop();
      if (leaf && !(leaf in out)) out[leaf] = fe.message;
    }
  }
  return out;
}

// stable Problem.code -> friendly spanish. unknown code -> server title.
const CODE_MESSAGES: Record<string, string> = {
  site_name_taken: 'Ya existe un sitio con ese nombre.',
  category_name_taken: 'Ya existe una categoría con ese nombre en el sitio.',
  admin_required: 'Necesitas permisos de administrador.',
  csrf: 'Tu sesión cambió. Recarga la página e inténtalo de nuevo.',
  run_busy: 'Hay una ejecución terminando. Inténtalo de nuevo en unos segundos.',
  busy: 'Servicio ocupado, inténtalo de nuevo en unos segundos.'
};

// spanish message for ui. never leak stack.
export function errorMessage(err: unknown): string {
  if (err instanceof ApiError) {
    const known =
      err.code && Object.hasOwn(CODE_MESSAGES, err.code) ? CODE_MESSAGES[err.code] : undefined;
    if (known) return known;
    if (err.status === 501) return 'Esta función todavía no está disponible en el servidor.';
    if (err.status === 429)
      return 'Demasiadas solicitudes. Espera un momento e inténtalo de nuevo.';
    if (err.status >= 500) return 'El servidor no respondió correctamente. Inténtalo de nuevo.';
    return err.message;
  }
  if (err instanceof TypeError) return 'No se pudo conectar con el servidor.';
  return 'Error inesperado.';
}

// login: 429 = brute force guard, wording about attempts.
export function authErrorMessage(err: unknown): string {
  if (err instanceof ApiError && err.status === 429)
    return 'Demasiados intentos. Espera un minuto e inténtalo de nuevo.';
  return errorMessage(err);
}
