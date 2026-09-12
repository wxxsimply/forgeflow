import { describe, expect, it } from 'vitest';
import { APIError } from '../api/client';
import { actionLabel, approvalStatusLabel, eventLabel, invocationStatusLabel, nodeLabel, riskLabel, roleLabel } from './labels';

describe('Chinese presentation labels', () => {
  it('translates known labels while preserving unknown extension values', () => {
    expect(roleLabel('admin')).toBe('管理员');
    expect(riskLabel('high')).toBe('高风险');
    expect(approvalStatusLabel('pending')).toBe('待处理');
    expect(nodeLabel('plan-approval')).toBe('计划审批');
    expect(eventLabel('run_created')).toBe('任务已创建');
    expect(actionLabel('pass')).toBe('通过');
    expect(nodeLabel('custom-node')).toBe('custom-node');
    expect(invocationStatusLabel()).toBe('—');
  });

  it('shows Chinese API errors and retains diagnostic metadata', () => {
    const error = new APIError(403, { message: 'permission denied', code: 'forbidden', requestId: 'request-1' });
    expect(error.message).toMatch(/[\u3400-\u9fff]/u);
    expect(error.code).toBe('forbidden');
    expect(error.requestId).toBe('request-1');
    expect(new APIError(400, { message: '请选择允许的仓库路径。' }).message).toBe('请选择允许的仓库路径。');
  });
});
