import AxeBuilder from '@axe-core/playwright';
import { expect, test } from '@playwright/test';
import { login, requireStack, startShop } from './helpers';

test.describe('admin flow @admin', () => {
  requireStack();

  test('anonymous user is sent to login', async ({ page }) => {
    await page.goto('/sitios');
    await expect(page).toHaveURL(
      (u) => u.pathname === '/login' && u.searchParams.get('next') === '/sitios'
    );
  });

  test('create site + category, crawl, see items', async ({ page }) => {
    test.setTimeout(120_000);
    const shop = await startShop();
    try {
      await login(page);
      const name = `e2e-${Date.now()}`;

      await page.getByRole('link', { name: 'Sitios' }).click();
      await page.getByRole('button', { name: 'Nuevo sitio' }).click();
      await page.getByLabel('Nombre').fill(name);
      await page.getByLabel('URL base').fill(shop.url);
      await page.getByLabel('Delay entre peticiones (ms)').fill('0');
      await page.getByRole('button', { name: 'Añadir' }).first().click();
      await page.getByPlaceholder('Campo').first().fill('title');
      await page.getByPlaceholder('h1.product-title').fill('h1');
      await page.getByRole('button', { name: 'Crear sitio' }).click();
      await expect(page.getByRole('heading', { level: 1, name })).toBeVisible();

      await page.getByRole('button', { name: 'Nueva categoría' }).click();
      await page.getByLabel('Nombre').fill('lista');
      await page.getByLabel('URL del listado').fill(`${shop.url}/lista`);
      await page.getByLabel('Selector de items').fill('a.item');
      await page.getByRole('button', { name: 'Crear categoría' }).click();
      await expect(page.getByRole('cell', { name: 'lista', exact: true })).toBeVisible();

      await page.getByRole('button', { name: 'Crawl completo' }).click();
      await expect(page.getByText(/Crawl completo en cola/)).toBeVisible();
      // page polls runs while active.
      await expect(page.getByText('Completada', { exact: true })).toBeVisible({ timeout: 60_000 });

      await page.getByRole('link', { name: 'Items' }).click();
      await expect(page.getByRole('heading', { level: 1, name: 'Items extraídos' })).toBeVisible();
      // other runs leave items too: filter to this site.
      await page.getByLabel('Sitio').selectOption({ label: name });
      await expect(page.getByText('2 items')).toBeVisible();
      await expect(page.getByRole('cell', { name: 'Producto 1', exact: true })).toBeVisible();
      await page.getByRole('button', { name: /Ver \(\d+\) propiedades de Producto 1/ }).click();
      await expect(page.getByText('SKU-1')).toBeVisible();
      await expect(page.getByRole('alert')).toHaveCount(0);
    } finally {
      await shop.close();
    }
  });

  test('main pages have no serious a11y violations @a11y', async ({ page }) => {
    test.setTimeout(90_000);
    await login(page);
    const serious: string[] = [];
    for (const path of ['/', '/sitios', '/ejecuciones', '/items']) {
      await page.goto(path);
      await page.waitForLoadState('networkidle');
      const r = await new AxeBuilder({ page })
        .withTags(['wcag2a', 'wcag2aa', 'wcag21aa'])
        .analyze();
      for (const v of r.violations) {
        if (v.impact === 'serious' || v.impact === 'critical') serious.push(`${path}: ${v.id}`);
      }
    }
    expect(serious).toEqual([]);
  });
});
