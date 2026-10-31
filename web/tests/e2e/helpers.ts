import { createServer, type Server } from 'node:http';
import type { AddressInfo } from 'node:net';
import { expect, test, type Page } from '@playwright/test';

export const hasStack = !!process.env.BASE_URL;
export const admin = {
  email: process.env.E2E_ADMIN_EMAIL ?? '',
  password: process.env.E2E_ADMIN_PASSWORD ?? ''
};

// whole file skipped (reported as skipped, never as passed) without a stack.
export function requireStack() {
  test.skip(!hasStack, 'BASE_URL not set: e2e needs the full stack.');
  test.skip(!admin.email || !admin.password, 'E2E_ADMIN_EMAIL/E2E_ADMIN_PASSWORD not set');
}

export async function login(page: Page) {
  await page.goto('/login');
  await page.getByLabel('Email').fill(admin.email);
  await page.getByLabel('Contraseña').fill(admin.password);
  await page.getByRole('button', { name: 'Entrar' }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'Dashboard' })).toBeVisible();
}

// tiny shop for the worker to crawl: 1 listing page, 2 products. worker must reach this host.
export async function startShop(): Promise<{ url: string; close: () => Promise<void> }> {
  const server: Server = createServer((req, res) => {
    res.setHeader('content-type', 'text/html; charset=utf-8');
    if (req.url === '/lista') {
      res.end('<a class="item" href="/p/1">Uno</a><a class="item" href="/p/2">Dos</a>');
      return;
    }
    const m = /^\/p\/(\d+)$/.exec(req.url ?? '');
    if (m) {
      res.end(
        `<title>P${m[1]}</title><h1>Producto ${m[1]}</h1><span class="price">S/ ${m[1]}9.90</span>` +
          `<script type="application/ld+json">{"sku":"SKU-${m[1]}"}</script>`
      );
      return;
    }
    res.statusCode = 404;
    res.end('no');
  });
  await new Promise<void>((r) => server.listen(0, '127.0.0.1', r));
  const { port } = server.address() as AddressInfo;
  return {
    url: `http://127.0.0.1:${port}`,
    close: () => new Promise((r) => server.close(() => r()))
  };
}
