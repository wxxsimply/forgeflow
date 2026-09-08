import { defineConfig, devices } from '@playwright/test';

const baseURL = process.env.FORGEFLOW_STAGING_BASE_URL;
if (!baseURL || !baseURL.startsWith('https://')) {
  throw new Error('FORGEFLOW_STAGING_BASE_URL must be an HTTPS URL');
}

export default defineConfig({
  testDir: './e2e',
  testMatch: 'real-flow.spec.ts',
  fullyParallel: false,
  retries: 0,
  reporter: 'line',
  timeout: 120_000,
  expect: { timeout: 30_000 },
  use: {
    baseURL,
    trace: 'retain-on-failure',
    ignoreHTTPSErrors: false,
  },
  projects: [{ name: 'chromium-staging', use: { ...devices['Desktop Chrome'] } }],
});
