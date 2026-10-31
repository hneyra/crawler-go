// typed api client (openapi-fetch). result unwrap -> data or ApiError.
import createClient from 'openapi-fetch';
import type { paths } from './schema.js';
import { ApiError, asProblem } from './problem.js';

export type ApiClient = ReturnType<typeof createClient<paths>>;

export function createApi(baseUrl: string, fetch: (req: Request) => Promise<Response>): ApiClient {
  return createClient<paths>({ baseUrl, fetch });
}

interface Result<T> {
  data?: T;
  error?: unknown;
  response: Response;
}

// ok -> data. not ok -> ApiError w/ parsed problem.
export async function unwrap<T>(p: Promise<Result<T>>): Promise<T> {
  const { data, error, response } = await p;
  if (!response.ok) throw new ApiError(response.status, asProblem(error, response.status));
  return data as T;
}
