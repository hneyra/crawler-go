import { defineConfig, devices } from '@playwright/test';

// e2e runs against a full stack (web + api + worker + db). no BASE_URL -> every spec skips.
// e.g. BASE_URL=http://localhost:3000 E2E_ADMIN_EMAIL=... E2E_ADMIN_PASSWORD=... yarn e2e
// PW_CHROMIUM_PATH: use a preinstalled chromium instead of `playwright install`.
const baseURL = process.env.BASE_URL;
const executablePath = process.env.PW_CHROMIUM_PATH || undefined;

export default defineConfig({
  testDir: 'tests/e2e',
  testMatch: '**/*.e2e.ts',
  fullyParallel: false,
  workers: 1,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? [['list'], ['html', { open: 'never' }]] : 'list',
  use: {
    baseURL,
    trace: 'retain-on-failure',
    locale: 'es-ES',
    launchOptions: { executablePath }
  },
  projects: [{ name: 'desktop', use: { ...devices['Desktop Chrome'] } }]
});
