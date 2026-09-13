import { expect, test } from '@playwright/test';

for (const viewport of [{ width: 1440, height: 960 }, { width: 390, height: 844 }]) {
  test(`Chinese login fits ${viewport.width}px viewport`, async ({ page }, testInfo) => {
    await page.setViewportSize(viewport);
    await page.route('**/api/v1/**', (route) => route.fulfill({
      status: 401, contentType: 'application/json', body: JSON.stringify({ code: 'unauthorized' }),
    }));
    await page.goto('/login');
    await expect(page).toHaveTitle('ForgeFlow 控制台');
    await expect(page.locator('html')).toHaveAttribute('lang', 'zh-CN');
    await expect(page.getByRole('heading', { name: '登录控制台' })).toBeVisible();
    await expect(page.getByText('欢迎回来')).toBeVisible();
    await expect(page.getByRole('button', { name: '登录', exact: true })).toBeVisible();
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
    await page.screenshot({ path: testInfo.outputPath('chinese-login.png'), fullPage: true });
  });
}
