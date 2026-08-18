import AxeBuilder from '@axe-core/playwright';
import { expect, test, type Page, type Route } from '@playwright/test';

const ids = {
  user: '00000000-0000-4000-8000-000000000001', repo: '00000000-0000-4000-8000-000000000030',
  run: '00000000-0000-4000-8000-000000000010', trace: '00000000-0000-4000-8000-000000000011', approval: '00000000-0000-4000-8000-000000000020',
};
const createdAt = '2026-08-10T08:00:00Z';

test('viewer can recover a session but never receives mutation controls', async ({ page }) => {
  const state = workflowState('viewer');
  await page.route('**/api/v1/**', (route) => mockAPI(route, state));
  await login(page, state.user.email);
  await expect(page).toHaveURL(/\/runs$/);
  await expect(page.getByText(state.run.task)).toBeVisible();
  await expect(page.getByText('viewer', { exact: true })).toBeVisible();
  await expect(page.getByRole('link', { name: 'New Run' })).toHaveCount(0);
  await expect(page.getByRole('button', { name: /暂停|恢复|取消 Run|批准|拒绝/ })).toHaveCount(0);
  await expectNoSeriousAccessibilityViolations(page);
  await page.reload();
  await expect(page.getByRole('heading', { name: 'Runs' })).toBeVisible();
  await page.goto('/runs/00000000-0000-4000-8000-000000000099');
  await expect(page.getByRole('heading', { name: 'Run 不存在或不可访问' })).toBeVisible();
  await page.getByRole('button', { name: '退出' }).click();
  await expect(page).toHaveURL(/\/login(?:\?|$)/);
});

test('operator completes login, create run, approve and report browser flow', async ({ page }) => {
  const state = workflowState('operator');
  await page.route('**/api/v1/**', (route) => mockAPI(route, state));
  await login(page, state.user.email);
  await page.getByRole('link', { name: 'New Run' }).click();
  await expect(page.getByRole('heading', { name: '创建 Run' })).toBeVisible();
  await page.getByLabel('仓库').selectOption(ids.repo);
  await page.getByLabel('任务描述').fill('为订单接口增加幂等保护');
  await expectNoSeriousAccessibilityViolations(page);
  await page.getByRole('button', { name: '创建并进入 Run' }).click();
  await expect(page).toHaveURL(new RegExp(`/runs/${ids.run}$`));
  await expect(page.getByText('Human checkpoint')).toBeVisible();
  await page.getByRole('link', { name: '检查审批' }).click();
  await expect(page.getByRole('heading', { name: '执行计划审批' })).toBeVisible();
  await expect(page.getByText('先增加幂等键校验，再补并发测试。')).toBeVisible();
  await expectNoSeriousAccessibilityViolations(page);
  page.once('dialog', (dialog) => dialog.accept());
  await page.getByRole('button', { name: '批准并继续' }).click();
  await expect(page).toHaveURL(new RegExp(`/runs/${ids.run}$`));
  await page.getByRole('link', { name: '最终报告' }).click();
  await expect(page.getByRole('heading', { name: '最终执行报告' })).toBeVisible();
  await expect(page.getByText('增加幂等请求保护并完成测试。')).toBeVisible();
  await expectNoSeriousAccessibilityViolations(page);
});

type State = ReturnType<typeof workflowState>;
function workflowState(role: 'viewer' | 'operator') {
  const user = { id: ids.user, email: `${role}@example.com`, role, status: 'active', createdAt };
  const plan = { summary: '先增加幂等键校验，再补并发测试。', assumptions: [], filesLikelyAffected: ['internal/orders.go'], steps: [{ id: 'step-1', description: '实现幂等保护', acceptanceCriteria: ['重复请求返回相同结果'], dependsOn: [] }], risks: [{ level: 'medium', description: '并发竞争' }], testStrategy: ['并发集成测试'] };
  const approval = { request: { approvalId: ids.approval, runId: ids.run, actionType: 'plan', reason: '执行前需要人工确认计划', scope: ['internal/orders.go'], risk: 'medium', status: 'pending', requestedAt: createdAt }, runVersion: 4 };
  const run = { runId: ids.run, traceId: ids.trace, repositoryId: ids.repo, version: 4, status: 'waiting_for_plan_approval' as string, task: '为订单接口增加幂等保护', repositoryPath: 'D:/Code/orders', baseRevision: 'main', currentNodeId: 'plan-approval', completedNodeIds: ['start', 'planner', 'validate-plan'], plan, pendingApproval: approval.request as typeof approval.request | undefined, createdAt, updatedAt: '2026-08-10T08:02:00Z' };
  return { authenticated: false, user, run, approval };
}

async function login(page: Page, email: string) {
  await page.goto('/login?next=https%3A%2F%2Fevil.example%2Fsteal');
  await page.getByLabel('邮箱').fill(email);
  await page.getByLabel('密码').fill('secure test password');
  await page.getByRole('button', { name: '登录' }).click();
}

async function mockAPI(route: Route, state: State) {
  const request = route.request();
  const path = new URL(request.url()).pathname;
  if (path.endsWith('/auth/login')) { state.authenticated = true; return json(route, 200, { user: state.user, session: { id: '00000000-0000-4000-8000-000000000002', sourceIp: '127.0.0.1', userAgent: 'Playwright', createdAt, lastSeenAt: createdAt, expiresAt: '2026-08-11T08:00:00Z', idleExpiresAt: '2026-08-11T08:00:00Z' }, csrfToken: 'e2e-csrf' }, { 'Set-Cookie': 'forgeflow_csrf=e2e-csrf; Path=/; SameSite=Lax' }); }
  if (path.endsWith('/auth/logout')) { state.authenticated = false; return route.fulfill({ status: 204 }); }
  if (!state.authenticated) return json(route, 401, { code: 'unauthorized', message: 'authentication required', requestId: 'e2e', details: {} });
  if (path.endsWith('/auth/me')) return json(route, 200, state.user);
  if (path.endsWith('/repositories') && request.method() === 'GET') return json(route, 200, { items: [{ id: ids.repo, ownerId: ids.user, name: 'orders', localPath: 'D:/Code/orders', defaultBranch: 'main', createdAt, updatedAt: createdAt }] });
  if (path.endsWith('/runs') && request.method() === 'POST') return json(route, 202, state.run);
  if (path.endsWith('/runs') && request.method() === 'GET') return json(route, 200, { items: [state.run] });
  if (path.endsWith(`/runs/${ids.run}/events`)) return json(route, 200, { items: [], nextCursor: 0 });
  if (path.endsWith(`/runs/${ids.run}/stream`)) return route.fulfill({ status: 204 });
  if (path.endsWith(`/runs/${ids.run}/report`)) return json(route, 200, { runId: ids.run, status: state.run.status, plan: state.run.plan, implementation: { summary: '增加幂等请求保护并完成测试。', patch: 'diff --git a/orders.go b/orders.go', changedFiles: ['internal/orders.go'], evidence: [], unresolvedIssues: [], requestedApprovals: [] }, tests: { toolCallId: 'call-1', program: 'go', args: ['test', './...'], exitCode: 0, stdout: 'ok', stderr: '', duration: 1000, truncated: false, passed: true, completedAt: createdAt }, decision: { version: 'v1', action: 'pass', reasons: ['gates passed'], findingIds: [], inputSha256: 'abc', decidedAt: createdAt } });
  if (path.endsWith(`/runs/${ids.run}`)) return json(route, 200, state.run);
  if (path.endsWith(`/approvals/${ids.approval}/decision`)) { state.approval.request.status = 'approved'; state.run = { ...state.run, status: 'completed', version: 8, currentNodeId: 'end', pendingApproval: undefined }; return json(route, 200, state.run); }
  if (path.endsWith(`/approvals/${ids.approval}`)) return json(route, 200, state.approval, { ETag: `"${state.approval.runVersion}"` });
  if (path.endsWith('/approvals')) return json(route, 200, { items: [state.approval] });
  if (/\/runs\/[0-9a-f-]+$/.test(path)) return json(route, 404, { code: 'not_found', message: 'resource not found', requestId: 'e2e', details: {} });
  return json(route, 404, { code: 'not_found', message: 'not found', requestId: 'e2e', details: {} });
}

function json(route: Route, status: number, body: unknown, headers: Record<string, string> = {}) { return route.fulfill({ status, contentType: 'application/json', headers, body: JSON.stringify(body) }); }
async function expectNoSeriousAccessibilityViolations(page: Page) { const results = await new AxeBuilder({ page }).analyze(); expect(results.violations.filter((violation) => violation.impact === 'serious' || violation.impact === 'critical')).toEqual([]); }
