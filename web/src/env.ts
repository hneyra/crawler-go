import { defineEnvVars } from '@sveltejs/kit/env';

// strip trailing slash. must be absolute http(s).
function absoluteUrl(name: string, fallback: string) {
  return (value: string | undefined) => {
    const raw = value && value.trim() !== '' ? value.trim() : fallback;
    let u: URL;
    try {
      u = new URL(raw);
    } catch {
      throw new Error(`${name} must be an absolute URL, got ${JSON.stringify(raw)}`);
    }
    if (u.protocol !== 'http:' && u.protocol !== 'https:') {
      throw new Error(`${name} must be http(s)`);
    }
    return u.origin + u.pathname.replace(/\/+$/, '');
  };
}

export const variables = defineEnvVars({
  API_INTERNAL_URL: {
    description: 'Backend base URL reachable from the SSR server (e.g. http://127.0.0.1:8080).',
    schema: absoluteUrl('API_INTERNAL_URL', 'http://127.0.0.1:8080')
  },
  PUBLIC_ORIGIN: {
    description: 'Public origin of the admin ui (e.g. https://crawler.example).',
    schema: absoluteUrl('PUBLIC_ORIGIN', 'http://localhost:5173')
  }
});
