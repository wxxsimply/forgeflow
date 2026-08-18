import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { Approval, Run, User } from './api/client';
import { APIError } from './api/client';
import { renderApp } from './test/render';

vi.mock('./api/client', async (importOriginal) => {
  const actual = await importOriginal<typeof import('./api/client')>();
  return {
    ...actual,
    getCurrentUser: vi.fn(), logout: vi.fn(),
    listRepositories: vi.fn(), createRepository: vi.fn(), createRun: vi.fn(),
    getRun: vi.fn(), listRunEvents: vi.fn(), mutateRun: vi.fn(),
    listApprovals: vi.fn(), getApproval: vi.fn(), decideApproval: vi.fn(),
    listRunArtifacts: vi.fn(), getRunReport: vi.fn(),
    listEvalRuns: vi.fn(), getEvalRun: vi.fn(), listAgents: vi.fn(), listPrompts: vi.fn(),
  };
});

import * as api from './api/client';

const operator: User = { id: '00000000-0000-4000-8000-000000000001', email: 'operator@example.com', role: 'operator', status: 'active', createdAt: '2026-08-10T08:00:00Z' };
const run: Run = {
  runId: '00000000-0000-4000-8000-000000000010', traceId: '00000000-0000-4000-8000-000000000011', version: 4,
  status: 'waiting_for_plan_approval', task: '为订单接口增加幂等保护', repositoryPath: 'D:/Code/orders', baseRevision: 'main', currentNodeId: 'plan-approval',
  completedNodeIds: ['start', 'planner', 'validate-plan'], createdAt: '2026-08-10T08:00:00Z', updatedAt: '2026-08-10T08:02:00Z',
  plan: { summary: '增加幂等键校验与冲突响应。', assumptions: [], filesLikelyAffected: ['internal/orders.go'], steps: [{ id: 'step-1', description: '实现幂等校验', acceptanceCriteria: ['重复请求返回相同结果'], dependsOn: [] }], risks: [{ level: 'medium', description: '并发竞争' }], testStrategy: ['并发集成测试'] },
};
const approval: Approval = {
  runVersion: 4,
  request: { approvalId: '00000000-0000-4000-8000-000000000020', runId: run.runId, actionType: 'plan', reason: '执行前需要人工确认计划', scope: ['internal/orders.go'], risk: 'medium', status: 'pending', requestedAt: '2026-08-10T08:01:00Z' },
};

beforeEach(() => {
  vi.mocked(api.getCurrentUser).mockReset().mockResolvedValue(operator);
  vi.mocked(api.listRepositories).mockReset().mockResolvedValue({ items: [{ id: '00000000-0000-4000-8000-000000000030', ownerId: operator.id, name: 'orders', localPath: 'D:/Code/orders', defaultBranch: 'main', createdAt: operator.createdAt, updatedAt: operator.createdAt }] });
  vi.mocked(api.createRun).mockReset().mockResolvedValue(run);
  vi.mocked(api.getRun).mockReset().mockResolvedValue(run);
  vi.mocked(api.listRunEvents).mockReset().mockResolvedValue({ items: [], nextCursor: 0 });
  vi.mocked(api.getApproval).mockReset().mockResolvedValue({ approval, etag: '"4"' });
  vi.mocked(api.decideApproval).mockReset();
  vi.spyOn(window, 'confirm').mockReturnValue(true);
});

describe('governed workflow pages', () => {
  it('creates a run with repository, budget and a stable idempotency key', async () => {
    const user = userEvent.setup();
    renderApp('/runs/new');
    await user.selectOptions(await screen.findByLabelText('仓库'), '00000000-0000-4000-8000-000000000030');
    await user.type(screen.getByLabelText('任务描述'), '增加重复提交保护');
    await user.clear(screen.getByLabelText('最大迭代次数'));
    await user.type(screen.getByLabelText('最大迭代次数'), '3');
    await user.click(screen.getByRole('button', { name: '创建并进入 Run' }));
    await waitFor(() => expect(api.createRun).toHaveBeenCalledOnce());
    const [input, key] = vi.mocked(api.createRun).mock.calls[0];
    expect(input).toMatchObject({ repositoryId: '00000000-0000-4000-8000-000000000030', task: '增加重复提交保护', maxIterations: 3 });
    expect(key).toMatch(/^[0-9a-f-]{36}$/i);
  });

  it('reloads approval evidence after an ETag conflict', async () => {
    vi.mocked(api.decideApproval).mockRejectedValue(new APIError(409, { code: 'conflict', message: 'stale approval' }));
    const user = userEvent.setup();
    renderApp(`/approvals/${approval.request.approvalId}`);
    expect(await screen.findByText('增加幂等键校验与冲突响应。')).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: '批准并继续' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('审批已被其他人更新');
    expect(api.decideApproval).toHaveBeenCalledWith(approval.request.approvalId, '"4"', 'approve', '');
    expect(api.getApproval).toHaveBeenCalledTimes(2);
  });

  it('shows an honest empty eval state without synthetic scores', async () => {
    vi.mocked(api.listEvalRuns).mockResolvedValue({ items: [] });
    vi.mocked(api.listAgents).mockResolvedValue({ items: [
      { name: 'planner', version: 'v1', model: 'gpt-fixture', promptVersion: 'planner/v1', role: 'bounded planning' },
    ] });
    vi.mocked(api.listPrompts).mockResolvedValue({ items: [
      { agent: 'planner', version: 'planner/v1', sha256: 'a'.repeat(64), configured: true },
    ], releases: [] });
    renderApp('/evals');
    expect(await screen.findByText('尚无真实 Eval 报告')).toBeInTheDocument();
    expect(screen.getByText(/不会生成或展示虚构成绩/)).toBeInTheDocument();
  });
});
