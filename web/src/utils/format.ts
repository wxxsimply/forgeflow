import type { Run } from '../api/client';

export function formatDateTime(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return '—';
  return new Intl.DateTimeFormat('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' }).format(date);
}

export function formatDuration(start: string, end: string): string {
  const milliseconds = Math.max(0, new Date(end).getTime() - new Date(start).getTime());
  if (!Number.isFinite(milliseconds)) return '—';
  if (milliseconds < 60_000) return `${Math.round(milliseconds / 1000)}s`;
  if (milliseconds < 3_600_000) return `${Math.round(milliseconds / 60_000)}m`;
  return `${(milliseconds / 3_600_000).toFixed(1)}h`;
}

export function shortPath(value: string): string {
  const normalized = value.replaceAll('\\', '/');
  return normalized.split('/').filter(Boolean).at(-1) || value;
}

const labels: Record<Run['status'], string> = {
  created: '已创建', planning: '规划中', waiting_for_plan_approval: '等待计划审批', preparing_workspace: '准备工作区',
  implementing: '实现中', evaluating: '评估中', waiting_for_action_approval: '等待动作审批', paused: '已暂停', repairing: '修复中',
  completed: '已完成', failed: '失败', cancelled: '已取消',
};
export function statusLabel(status: Run['status']): string { return labels[status] || status; }
