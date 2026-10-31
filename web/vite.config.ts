import { defineConfig } from 'vitest/config';
import adapter from '@sveltejs/adapter-node';
import { sveltekit } from '@sveltejs/kit/vite';

// dev proxy target. backend http addr (CRAWLER_HTTP_ADDR).
const apiTarget = process.env.API_INTERNAL_URL ?? 'http://127.0.0.1:8080';

export default defineConfig({
  plugins: [
    sveltekit({
      compilerOptions: {
        // runes everywhere except deps.
        runes: ({ filename }) =>
          filename.split(/[/\\]/).includes('node_modules') ? undefined : true
      },
      adapter: adapter(),
      // origin known at build -> csrf + url.origin. unset -> derived from request (use HOST_HEADER/PROTOCOL_HEADER behind proxy).
      paths: process.env.PUBLIC_ORIGIN ? { origin: process.env.PUBLIC_ORIGIN } : {},
      csp: {
        mode: 'auto',
        directives: {
          'default-src': ['self'],
          'script-src': ['self'],
          'style-src': ['self'],
          // kit's route announcer sets one inline style attr. allow exactly that hash.
          'style-src-attr': [
            'unsafe-hashes',
            'sha256-S8qMpvofolR8Mpjy4kQvEm7m1q8clzU4dfDH0AmvZjo='
          ],
          'img-src': ['self', 'https:', 'data:'],
          'font-src': ['self'],
          'connect-src': ['self'],
          'object-src': ['none'],
          'frame-ancestors': ['none'],
          'base-uri': ['self'],
          'form-action': ['self']
        }
      }
    })
  ],
  server: {
    proxy: {
      '/api': { target: apiTarget, changeOrigin: false }
    }
  },
  test: {
    expect: { requireAssertions: true },
    include: ['src/**/*.{test,spec}.{js,ts}', 'tests/unit/**/*.test.ts'],
    environment: 'node'
  }
});
