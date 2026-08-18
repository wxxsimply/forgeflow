import { defineConfig, devices } from '@playwright/test';

export default defineConfig({
  testDir: './e2e',
  testMatch: 'real-flow.spec.ts',
  fullyParallel: false,
  retries: 0,
  reporter: 'line',
  timeout: 60_000,
  expect: { timeout: 20_000 },
  use: { baseURL: 'http://127.0.0.1:5173', trace: 'retain-on-failure' },
  webServer: {
    command: 'npm run dev -- --host 127.0.0.1 --port 5173',
    url: 'http://127.0.0.1:5173',
    reuseExistingServer: false,
    timeout: 120_000,
  },
  projects: [{ name: 'chromium-real', use: { ...devices['Desktop Chrome'] } }],
});
