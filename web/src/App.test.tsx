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
    getCurrentUser: vi.fn(), login: vi.fn(), logout: vi.fn(), registerAccount: vi.fn(),
    listRuns: vi.fn(), getRun: vi.fn(), listRunEvents: vi.fn(),
    listSessions: vi.fn(), revokeSession: vi.fn(),
    getMFAStatus: vi.fn(), setupMFA: vi.fn(), confirmMFA: vi.fn(),
    createUserDataExport: vi.fn(), downloadUserDataExport: vi.fn(), deleteCurrentAccount: vi.fn(),
  };
});

import * as api from './api/client';

const viewer: User = { id: '00000000-0000-4000-8000-000000000001', email: 'viewer@example.com', role: 'viewer', status: 'active', mfaEnabled: false, mfaRequired: false, createdAt: '2026-08-10T08:00:00Z' };
const unenrolledAdmin: User = { ...viewer, email: 'admin@example.com', role: 'admin', mfaRequired: true };
const run: Run = {
  runId: '00000000-0000-4000-8000-000000000010', traceId: '00000000-0000-4000-8000-000000000011', version: 4,
  status: 'waiting_for_plan_approval', task: '为订单接口增加幂等保护', repositoryPath: 'D:/Code/orders', baseRevision: 'main',
  currentNodeId: 'plan-approval', completedNodeIds: ['start', 'planner', 'validate-plan'], createdAt: '2026-08-10T08:00:00Z', updatedAt: '2026-08-10T08:02:00Z',
};

beforeEach(() => {
  vi.mocked(api.getCurrentUser).mockReset(); vi.mocked(api.login).mockReset(); vi.mocked(api.logout).mockReset(); vi.mocked(api.registerAccount).mockReset();
  vi.mocked(api.listRuns).mockReset(); vi.mocked(api.getRun).mockReset(); vi.mocked(api.listRunEvents).mockReset();
  vi.mocked(api.listSessions).mockReset(); vi.mocked(api.revokeSession).mockReset();
  vi.mocked(api.getMFAStatus).mockReset(); vi.mocked(api.setupMFA).mockReset(); vi.mocked(api.confirmMFA).mockReset();
  vi.mocked(api.createUserDataExport).mockReset(); vi.mocked(api.downloadUserDataExport).mockReset(); vi.mocked(api.deleteCurrentAccount).mockReset();
  vi.mocked(api.listRuns).mockResolvedValue({ items: [run] });
});

describe('authentication shell', () => {
  it('lets a visitor create an account and then log in', async () => {
    vi.mocked(api.getCurrentUser).mockRejectedValue(new APIError(401));
    vi.mocked(api.registerAccount).mockResolvedValue({ ...viewer, email: 'new@example.com', role: 'operator' });
    vi.mocked(api.login).mockResolvedValue({ ...viewer, email: 'new@example.com', role: 'operator' });
    const user = userEvent.setup(); renderApp('/login');
    await user.click(await screen.findByRole('link', { name: '创建账号' }));
    expect(await screen.findByRole('heading', { name: '创建账号' })).toBeInTheDocument();
    await user.type(screen.getByLabelText('邮箱'), 'new@example.com');
    await user.type(screen.getByLabelText('密码'), 'a strong passphrase for signup');
    await user.type(screen.getByLabelText('确认密码'), 'different strong password');
    await user.click(screen.getByRole('button', { name: '注册账号' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('两次输入的密码不一致');
    expect(api.registerAccount).not.toHaveBeenCalled();
    await user.clear(screen.getByLabelText('确认密码'));
    await user.type(screen.getByLabelText('确认密码'), 'a strong passphrase for signup');
    await user.click(screen.getByRole('button', { name: '注册账号' }));
    expect(api.registerAccount).toHaveBeenCalledWith({ email: 'new@example.com', password: 'a strong passphrase for signup' });
    expect(await screen.findByRole('status')).toHaveTextContent('注册成功');
    await user.type(screen.getByLabelText('邮箱'), 'new@example.com');
    await user.type(screen.getByLabelText('密码'), 'a strong passphrase for signup');
    await user.click(screen.getByRole('button', { name: '登录' }));
    expect(await screen.findByRole('heading', { name: '运行任务' })).toBeInTheDocument();
  });

  it('keeps registration failures actionable without exposing server internals', async () => {
    vi.mocked(api.getCurrentUser).mockRejectedValue(new APIError(401));
    vi.mocked(api.registerAccount).mockRejectedValue(new APIError(409, { code: 'conflict', message: 'database trace must not render' }));
    const user = userEvent.setup(); renderApp('/register');
    await user.type(await screen.findByLabelText('邮箱'), 'existing@example.com');
    await user.type(screen.getByLabelText('密码'), 'a strong passphrase for signup');
    await user.type(screen.getByLabelText('确认密码'), 'a strong passphrase for signup');
    await user.click(screen.getByRole('button', { name: '注册账号' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('该邮箱已注册');
    expect(screen.queryByText(/database trace/i)).not.toBeInTheDocument();
    vi.mocked(api.registerAccount).mockRejectedValue(new APIError(429));
    await user.click(screen.getByRole('button', { name: '注册账号' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('注册尝试过于频繁');
  });

  it('provides accessible login controls and redirects only to an internal path', async () => {
    vi.mocked(api.getCurrentUser).mockRejectedValue(new APIError(401, { code: 'unauthorized', message: 'hidden server text' }));
    vi.mocked(api.login).mockResolvedValue(viewer);
    const user = userEvent.setup();
    renderApp('/login?next=https%3A%2F%2Fevil.example%2Fsteal');
    expect(await screen.findByText(/不会从网页调用真实模型或修改源码/)).toBeInTheDocument();
    await user.type(await screen.findByLabelText('邮箱'), 'viewer@example.com');
    await user.type(screen.getByLabelText('密码'), 'viewer secure password');
    await user.click(screen.getByRole('button', { name: '登录' }));
    expect(vi.mocked(api.login).mock.calls[0][0]).toEqual({ email: 'viewer@example.com', password: 'viewer secure password', secondFactor: undefined, remember: false });
    expect(await screen.findByRole('heading', { name: '运行任务' })).toBeInTheDocument();
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
    expect(await screen.findByRole('alert')).toHaveTextContent('邮箱、密码或管理员验证码错误');
    expect(screen.queryByText(/database trace/i)).not.toBeInTheDocument();
    vi.mocked(api.login).mockRejectedValue(new APIError(429));
    await user.click(screen.getByRole('button', { name: '登录' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('尝试次数过多');
  });

  it('redirects an unenrolled administrator into MFA setup and shows recovery codes once', async () => {
    vi.mocked(api.getCurrentUser).mockRejectedValue(new APIError(401));
    vi.mocked(api.login).mockResolvedValue(unenrolledAdmin);
    vi.mocked(api.getMFAStatus).mockResolvedValue({ enabled: false, required: true });
    vi.mocked(api.setupMFA).mockResolvedValue({ secret: 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567', provisioningUri: 'otpauth://totp/ForgeFlow%3Aadmin', expiresAt: '2026-09-21T03:00:00Z' });
    vi.mocked(api.confirmMFA).mockResolvedValue({ recoveryCodes: Array.from({ length: 10 }, (_, index) => `ABCD-EFGH-JKLM-${String(index).padStart(4, '2')}`) });
    const user = userEvent.setup(); renderApp('/login?next=/runs');
    await user.type(await screen.findByLabelText('邮箱'), 'admin@example.com');
    await user.type(screen.getByLabelText('密码'), 'correct horse battery staple');
    await user.click(screen.getByRole('button', { name: '登录' }));
    expect(await screen.findByRole('heading', { name: '多因素认证' })).toBeInTheDocument();
    expect(screen.getByText(/当前会话仅可完成 MFA 绑定/)).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: '生成并下载' })).not.toBeInTheDocument();
    await user.type(screen.getByLabelText('当前密码', { selector: '#mfa-password' }), 'correct horse battery staple');
    await user.click(screen.getByRole('button', { name: '开始绑定' }));
    expect(await screen.findByText('ABCDEFGHIJKLMNOPQRSTUVWXYZ234567')).toBeInTheDocument();
    await user.type(screen.getByLabelText('6 位动态验证码'), '123456');
    await user.click(screen.getByRole('button', { name: '确认并启用' }));
    expect(await screen.findByText(/恢复码只显示这一次/)).toBeInTheDocument();
    expect(vi.mocked(api.confirmMFA)).toHaveBeenCalledWith('123456');
  });

  it('restores a viewer session and renders a read-only run list', async () => {
    vi.mocked(api.getCurrentUser).mockResolvedValue(viewer);
    renderApp('/runs');
    expect(await screen.findByText('为订单接口增加幂等保护')).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: /ForgeFlow 管理 AI 辅助开发任务的计划、审批与执行记录/ })).toBeInTheDocument();
    expect(screen.getByText(/当前网页只模拟这套流程，不会调用模型或修改代码/)).toBeInTheDocument();
    expect(screen.getByText(/选择受控仓库，创建模拟任务，查看计划并批准或拒绝/)).toBeInTheDocument();
    expect(screen.getByText(/真实模型调用、源码修改、测试运行、生产发布或批量评测/)).toBeInTheDocument();
    expect(screen.getByText('只读账号：请联系管理员创建模拟任务。')).toBeInTheDocument();
    expect(screen.getByText('只读用户')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /创建|审批|取消/ })).not.toBeInTheDocument();
    expect(screen.queryByRole('link', { name: '开始体验模拟流程' })).not.toBeInTheDocument();
  });

  it('gives task creators a direct, clearly scoped first step', async () => {
    vi.mocked(api.getCurrentUser).mockResolvedValue({ ...viewer, role: 'operator' });
    renderApp('/runs');
    expect(await screen.findByRole('link', { name: '开始体验模拟流程' })).toHaveAttribute('href', '/runs/new');
  });

  it('logs out with the authenticated mutation and returns to login', async () => {
    vi.mocked(api.getCurrentUser).mockResolvedValue(viewer); vi.mocked(api.logout).mockResolvedValue();
    const user = userEvent.setup(); renderApp('/runs');
    await user.click(await screen.findByRole('button', { name: '退出' }));
    expect(api.logout).toHaveBeenCalledOnce();
    expect(await screen.findByRole('heading', { name: '登录控制台' })).toBeInTheDocument();
  });
});

describe('account data', () => {
  it('exposes owner data export from the account page', async () => {
    vi.mocked(api.getCurrentUser).mockResolvedValue(viewer);
    vi.mocked(api.createUserDataExport).mockResolvedValue({ id: '00000000-0000-4000-8000-000000000030', expiresAt: '2026-08-10T08:15:00Z' });
    vi.mocked(api.downloadUserDataExport).mockResolvedValue();
    const user = userEvent.setup();
    renderApp('/account');
    await user.click(await screen.findByRole('button', { name: '生成并下载' }));
    await waitFor(() => expect(api.downloadUserDataExport).toHaveBeenCalledWith('00000000-0000-4000-8000-000000000030'));
    expect(screen.getByRole('status')).toHaveTextContent('导出已开始下载');
  });
});

describe('run states', () => {
  it('renders empty and error states explicitly', async () => {
    vi.mocked(api.getCurrentUser).mockResolvedValue(viewer); vi.mocked(api.listRuns).mockResolvedValue({ items: [] });
    const view = renderApp('/runs');
    expect(await screen.findByRole('heading', { name: '还没有运行任务' })).toBeInTheDocument();
    view.unmount();
    vi.mocked(api.listRuns).mockRejectedValue(new APIError(503));
    renderApp('/runs');
    expect(await screen.findByRole('heading', { name: '无法加载运行任务' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '重新加载' })).toBeInTheDocument();
  });

  it('loads a run detail and its append-only timeline', async () => {
    vi.mocked(api.getCurrentUser).mockResolvedValue(viewer); vi.mocked(api.getRun).mockResolvedValue(run);
    vi.mocked(api.listRunEvents).mockResolvedValue({ items: [{ sequence: 1, event: { eventId: '00000000-0000-4000-8000-000000000020', runId: run.runId, traceId: run.traceId, type: 'run_created', message: 'Run created', createdAt: run.createdAt } }], nextCursor: 1 });
    renderApp(`/runs/${run.runId}`);
    expect(await screen.findByRole('heading', { name: run.task })).toBeInTheDocument();
    expect(await screen.findByText('Run created')).toBeInTheDocument();
    await waitFor(() => expect(screen.getAllByText('计划审批')).toHaveLength(2));
  });
});
