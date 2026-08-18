import { expect, test } from '@playwright/test';

const environment = (globalThis as { process?: { env?: Record<string, string | undefined> } }).process?.env ?? {};
const enabled = environment.FORGEFLOW_REAL_E2E === '1';
const email = environment.FORGEFLOW_E2E_EMAIL || 'stage9-admin@example.com';
const password = environment.FORGEFLOW_E2E_PASSWORD || 'Stage9-Admin-Password-Only-For-E2E!';
const repositoryPath = environment.FORGEFLOW_E2E_REPOSITORY || '';

test.skip(!enabled, 'Set FORGEFLOW_REAL_E2E=1 to run against a real API and PostgreSQL.');

test('real PostgreSQL, API and worker workflow reaches an approved report', async ({ page }) => {
  expect(repositoryPath, 'FORGEFLOW_E2E_REPOSITORY is required').not.toBe('');
  await page.goto('/login');
  await page.getByLabel('邮箱').fill(email);
  await page.getByLabel('密码').fill(password);
  await page.getByRole('button', { name: '登录' }).click();
  await expect(page).toHaveURL(/\/runs$/);

  await page.getByRole('link', { name: 'New Run' }).click();
  await page.getByRole('button', { name: '+ 登记本地仓库' }).click();
  await page.getByLabel('名称').fill(`stage9-${Date.now()}`);
  await page.getByLabel('本地绝对路径').fill(repositoryPath);
  await page.getByLabel('默认基线').fill('HEAD');
  await page.getByRole('button', { name: '登记仓库' }).click();
  await expect(page.getByLabel('仓库')).not.toHaveValue('');
  await page.getByLabel('任务描述').fill('检查当前仓库并生成一份受审批保护的执行计划；不要执行外部副作用。');
  await page.getByRole('button', { name: '创建并进入 Run' }).click();
  await expect(page).toHaveURL(/\/runs\/[0-9a-f-]{36}$/);

  await expect(page.getByRole('link', { name: '检查审批' })).toBeVisible({ timeout: 30_000 });
  await page.getByRole('link', { name: '检查审批' }).click();
  await expect(page.getByRole('heading', { name: '执行计划审批' })).toBeVisible();
  page.once('dialog', (dialog) => dialog.accept());
  await page.getByRole('button', { name: '批准并继续' }).click();
  await expect(page).toHaveURL(/\/runs\/[0-9a-f-]{36}$/);
  await page.getByRole('link', { name: '最终报告' }).click();
  await expect(page.getByRole('heading', { name: '最终执行报告' })).toBeVisible();
  await expect(page.getByText('已生成').first()).toBeVisible();
});
