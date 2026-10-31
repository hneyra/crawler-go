import { describe, expect, it } from 'vitest';
import {
  ApiError,
  asProblem,
  authErrorMessage,
  errorMessage,
  fieldErrorMap
} from '#lib/api/problem.js';
import { unwrap } from '#lib/api/client.js';
import { safeNext } from '#lib/next.js';

describe('problem+json', () => {
  it('parses field errors and maps to inputs', () => {
    const p = asProblem(
      {
        title: 'Bad',
        status: 400,
        errors: [
          { field: 'rules[0].cost', code: 'min', message: 'debe ser >= 0' },
          { field: 'email', code: 'x', message: 'inválido' }
        ]
      },
      400
    );
    const e = new ApiError(400, p);
    expect(fieldErrorMap(e)).toEqual({
      'rules[0].cost': 'debe ser >= 0',
      cost: 'debe ser >= 0',
      email: 'inválido'
    });
    expect(e.message).toBe('Bad');
  });
  it('non-problem bodies are null', () => {
    expect(asProblem('oops', 500)).toBeNull();
    expect(errorMessage(new ApiError(501, null))).toMatch(/no está disponible/);
  });
  it('unwrap throws ApiError on non-2xx', async () => {
    await expect(
      unwrap(
        Promise.resolve({
          error: { title: 'Nope', status: 404 },
          response: new Response(null, { status: 404 })
        })
      )
    ).rejects.toMatchObject({ status: 404 });
    expect(
      await unwrap(Promise.resolve({ data: 1, response: new Response(null, { status: 200 }) }))
    ).toBe(1);
  });
  it('safeNext blocks open redirects', () => {
    expect(safeNext('/sitios/3?x=1')).toBe('/sitios/3?x=1');
    expect(safeNext('//evil.example')).toBe('/');
    expect(safeNext('https://evil.example')).toBe('/');
    expect(safeNext('/\\evil')).toBe('/');
    expect(safeNext(null, '/items')).toBe('/items');
  });
});

describe('stable problem codes -> spanish copy', () => {
  const err = (status: number, code: string, title = 'x') =>
    new ApiError(status, asProblem({ title, status, code }, status));
  it.each([
    [409, 'site_name_taken', /Ya existe un sitio/],
    [409, 'category_name_taken', /Ya existe una categoría/],
    [403, 'admin_required', /administrador/],
    [403, 'csrf', /Recarga la página/],
    [409, 'run_busy', /ejecución terminando/],
    [503, 'busy', /Servicio ocupado, inténtalo de nuevo en unos segundos/]
  ])('%i %s', (status, code, re) => {
    expect(errorMessage(err(status, code))).toMatch(re);
  });
  it('unknown code keeps server title', () => {
    expect(errorMessage(err(409, 'nuevo_codigo', 'Título del servidor'))).toBe(
      'Título del servidor'
    );
  });
  it('auth: 429 = too many attempts, busy = retry soon', () => {
    expect(authErrorMessage(err(429, 'rate_limited'))).toMatch(/^Demasiados intentos/);
    expect(authErrorMessage(err(503, 'busy'))).toMatch(/Servicio ocupado/);
    expect(authErrorMessage(err(400, 'validation_failed', 'Datos no válidos'))).toBe(
      'Datos no válidos'
    );
  });
  it('validation_failed maps errors[].field to inputs', () => {
    const e = new ApiError(
      400,
      asProblem(
        {
          title: 'Datos no válidos',
          status: 400,
          code: 'validation_failed',
          errors: [{ field: 'reason', code: 'schema', message: 'obligatorio' }]
        },
        400
      )
    );
    expect(fieldErrorMap(e)).toEqual({ reason: 'obligatorio' });
  });
});
