import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { Run, User } from './api/client';
import { APIError } from './api/client';
import { safeDestination } from './pages/LoginPage';
import { renderApp } from './test/render';

vi.mock('./api/client', async (importOriginal) => {
  const actual = await importOriginal<typeof import('./api/client')>();
  return {
    ...actual,
    getCurrentUser: vi.fn(), login: vi.fn(), logout: vi.fn(),
    listRuns: vi.fn(), getRun: vi.fn(), listRunEvents: vi.fn(),
    listSessions: vi.fn(), revokeSession: vi.fn(),
  };
});

import * as api from './api/client';

const viewer: User = { id: '00000000-0000-4000-8000-000000000001', email: 'viewer@example.com', role: 'viewer', status: 'active', createdAt: '2026-08-10T08:00:00Z' };
const run: Run = {
  runId: '00000000-0000-4000-8000-000000000010', traceId: '00000000-0000-4000-8000-000000000011', version: 4,
  status: 'waiting_for_plan_approval', task: '为订单接口增加幂等保护', repositoryPath: 'D:/Code/orders', baseRevision: 'main',
  currentNodeId: 'plan-approval', completedNodeIds: ['start', 'planner', 'validate-plan'], createdAt: '2026-08-10T08:00:00Z', updatedAt: '2026-08-10T08:02:00Z',
};

beforeEach(() => {
  vi.mocked(api.getCurrentUser).mockReset(); vi.mocked(api.login).mockReset(); vi.mocked(api.logout).mockReset();
  vi.mocked(api.listRuns).mockReset(); vi.mocked(api.getRun).mockReset(); vi.mocked(api.listRunEvents).mockReset();
  vi.mocked(api.listSessions).mockReset(); vi.mocked(api.revokeSession).mockReset();
  vi.mocked(api.listRuns).mockResolvedValue({ items: [run] });
});

describe('authentication shell', () => {
  it('provides accessible login controls and redirects only to an internal path', async () => {
    vi.mocked(api.getCurrentUser).mockRejectedValue(new APIError(401, { code: 'unauthorized', message: 'hidden server text' }));
    vi.mocked(api.login).mockResolvedValue(viewer);
    const user = userEvent.setup();
    renderApp('/login?next=https%3A%2F%2Fevil.example%2Fsteal');
    await user.type(await screen.findByLabelText('邮箱'), 'viewer@example.com');
    await user.type(screen.getByLabelText('密码'), 'viewer secure password');
    await user.click(screen.getByRole('button', { name: '登录' }));
    expect(vi.mocked(api.login).mock.calls[0][0]).toEqual({ email: 'viewer@example.com', password: 'viewer secure password', remember: false });
    expect(await screen.findByRole('heading', { name: 'Runs' })).toBeInTheDocument();
    expect(safeDestination('//evil.example/steal')).toBe('/runs');
    expect(safeDestination('/runs?status=active')).toBe('/runs?status=active');
  });

  it('shows uniform credential and rate-limit messages without leaking server details', async () => {
    vi.mocked(api.getCurrentUser).mockRejectedValue(new APIError(401));
    vi.mocked(api.login).mockRejectedValue(new APIError(401, { code: 'unauthorized', message: 'database trace must not render' }));
    const user = userEvent.setup(); renderApp('/login');
    await user.type(await screen.findByLabelText('邮箱'), 'nobody@example.com');
    await user.type(screen.getByLabelText('密码'), 'incorrect password');
    await user.click(screen.getByRole('button', { name: '登录' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('邮箱或密码错误');
    expect(screen.queryByText(/database trace/i)).not.toBeInTheDocument();
    vi.mocked(api.login).mockRejectedValue(new APIError(429));
    await user.click(screen.getByRole('button', { name: '登录' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('尝试次数过多');
  });

  it('restores a viewer session and renders a read-only run list', async () => {
    vi.mocked(api.getCurrentUser).mockResolvedValue(viewer);
    renderApp('/runs');
    expect(await screen.findByText('为订单接口增加幂等保护')).toBeInTheDocument();
    expect(screen.getByText('viewer')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /创建|审批|取消/ })).not.toBeInTheDocument();
  });

  it('logs out with the authenticated mutation and returns to login', async () => {
    vi.mocked(api.getCurrentUser).mockResolvedValue(viewer); vi.mocked(api.logout).mockResolvedValue();
    const user = userEvent.setup(); renderApp('/runs');
    await user.click(await screen.findByRole('button', { name: '退出' }));
    expect(api.logout).toHaveBeenCalledOnce();
    expect(await screen.findByRole('heading', { name: '登录控制台' })).toBeInTheDocument();
  });
});

describe('run states', () => {
  it('renders empty and error states explicitly', async () => {
    vi.mocked(api.getCurrentUser).mockResolvedValue(viewer); vi.mocked(api.listRuns).mockResolvedValue({ items: [] });
    const view = renderApp('/runs');
    expect(await screen.findByRole('heading', { name: '还没有 Run' })).toBeInTheDocument();
    view.unmount();
    vi.mocked(api.listRuns).mockRejectedValue(new APIError(503));
    renderApp('/runs');
    expect(await screen.findByRole('heading', { name: '无法加载 Runs' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '重新加载' })).toBeInTheDocument();
  });

  it('loads a run detail and its append-only timeline', async () => {
    vi.mocked(api.getCurrentUser).mockResolvedValue(viewer); vi.mocked(api.getRun).mockResolvedValue(run);
    vi.mocked(api.listRunEvents).mockResolvedValue({ items: [{ sequence: 1, event: { eventId: '00000000-0000-4000-8000-000000000020', runId: run.runId, traceId: run.traceId, type: 'run_created', message: 'Run created', createdAt: run.createdAt } }], nextCursor: 1 });
    renderApp(`/runs/${run.runId}`);
    expect(await screen.findByRole('heading', { name: run.task })).toBeInTheDocument();
    expect(await screen.findByText('Run created')).toBeInTheDocument();
    await waitFor(() => expect(screen.getAllByText('plan-approval')).toHaveLength(2));
  });
});
