import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link, useParams } from 'react-router-dom';
import { APIError, getRun, listRunEvents, mutateRun, type Run, type SequencedEvent } from '../api/client';
import { useAuth } from '../auth/AuthProvider';
import { RunSubnav } from '../components/RunSubnav';
import { LoadingRows, PageState } from '../components/States';
import { useRunEventStream } from '../hooks/useRunEventStream';
import { formatDateTime, formatDuration, shortPath, statusLabel } from '../utils/format';

const terminal = new Set(['completed', 'failed', 'cancelled']);

export function RunDetailPage() {
  const { runId = '' } = useParams();
  const { user } = useAuth();
  const queryClient = useQueryClient();
  const runQuery = useQuery({ queryKey: ['runs', runId], queryFn: () => getRun(runId), enabled: Boolean(runId) });
  const eventsQuery = useQuery({ queryKey: ['runs', runId, 'events'], queryFn: () => listRunEvents(runId), enabled: Boolean(runId) });
  const run = runQuery.data;
  const lastSequence = eventsQuery.data?.items.at(-1)?.sequence ?? 0;
  const streamState = useRunEventStream(runId, lastSequence, Boolean(run && eventsQuery.data && !terminal.has(run.status)));
  const lifecycle = useMutation({
    mutationFn: ({ action, reason }: { action: 'pause' | 'resume' | 'cancel'; reason?: string }) => mutateRun(runId, action, reason),
    onSuccess: (updated) => { queryClient.setQueryData(['runs', runId], updated); void queryClient.invalidateQueries({ queryKey: ['runs'] }); },
  });

  if (runQuery.isPending) return <div className="page"><LoadingRows count={7} /></div>;
  if (runQuery.error) {
    const missing = runQuery.error instanceof APIError && [403, 404].includes(runQuery.error.status);
    return <div className="page"><PageState tone="danger" title={missing ? 'Run 不存在或不可访问' : '无法加载 Run'} detail={missing ? '资源可能已删除，或者当前账号没有访问权限。' : '请检查 API 连接后重试。'} action={<Link className="secondary-button" to="/runs">返回列表</Link>} /></div>;
  }
  if (!run) return <div className="page"><PageState tone="danger" title="Run 响应为空" detail="请刷新页面重试。" /></div>;

  function runAction(action: 'pause' | 'resume' | 'cancel') {
    const labels = { pause: '暂停', resume: '恢复', cancel: '取消' };
    if (!window.confirm(`确认${labels[action]}此 Run？`)) return;
    const reason = action === 'resume' ? '' : window.prompt(`请输入${labels[action]}原因（可选）`, '') ?? '';
    lifecycle.mutate({ action, reason });
  }

  const canWrite = user?.role !== 'viewer';
  return <div className="page run-detail">
    <Link className="back-link" to="/runs">← 返回 Runs</Link>
    <div className="detail-heading">
      <div><span className="eyebrow">Run · {run.runId.slice(0, 8)}</span><h1>{run.task}</h1><p>{shortPath(run.repositoryPath)} · {run.baseRevision}</p></div>
      <div className="heading-actions"><span className={`status status--${run.status} status--large`}>{statusLabel(run.status)}</span>{canWrite && <LifecycleActions run={run} busy={lifecycle.isPending} act={runAction} />}</div>
    </div>
    {lifecycle.error && <div className="form-error" role="alert">{lifecycle.error.message}</div>}
    {run.pendingApproval && <div className="approval-banner"><div><span className="eyebrow">Human checkpoint</span><strong>{run.pendingApproval.reason}</strong></div><Link className="primary-button primary-button--fit" to={`/approvals/${run.pendingApproval.approvalId}`}>检查审批</Link></div>}
    <RunSubnav runId={runId} />
    <section className="metric-grid" aria-label="Run 摘要">
      <Metric label="当前节点" value={run.currentNodeId || '—'} mono /><Metric label="运行时长" value={formatDuration(run.createdAt, run.updatedAt)} /><Metric label="版本" value={`v${run.version}`} mono /><Metric label="预计成本" value={formatCost(run)} />
    </section>
    <div className="detail-grid">
      <section className="panel graph-panel"><div className="panel-heading"><div><span className="eyebrow">Graph state</span><h2>执行轨迹</h2></div><span className={`stream-state stream-state--${terminal.has(run.status) ? 'complete' : streamState}`}><i />{terminal.has(run.status) ? '已结束' : streamLabel(streamState)}</span></div><GraphProgress run={run} />{run.error && <div className="run-error" role="alert"><strong>{run.error.code}</strong><span>{run.error.message}</span></div>}</section>
      <section className="panel timeline-panel"><div className="panel-heading"><div><span className="eyebrow">Append-only events</span><h2>事件时间线</h2></div><span className="event-count">{eventsQuery.data?.items.length ?? 0} events</span></div>{eventsQuery.isPending ? <LoadingRows count={4} /> : eventsQuery.error ? <PageState tone="danger" title="事件加载失败" detail="实时连接会继续尝试恢复。" /> : <Timeline items={eventsQuery.data.items} />}</section>
    </div>
  </div>;
}

function LifecycleActions({ run, busy, act }: { run: Run; busy: boolean; act: (action: 'pause' | 'resume' | 'cancel') => void }) {
  if (terminal.has(run.status)) return null;
  return <div className="lifecycle-actions">{run.status === 'paused' ? <button className="secondary-button" disabled={busy} onClick={() => act('resume')}>恢复</button> : <button className="secondary-button" disabled={busy} onClick={() => act('pause')}>暂停</button>}<button className="danger-button" disabled={busy} onClick={() => act('cancel')}>取消 Run</button></div>;
}
function Metric({ label, value, mono = false }: { label: string; value: string; mono?: boolean }) { return <div className="metric"><span>{label}</span><strong className={mono ? 'mono' : ''}>{value}</strong></div>; }
function formatCost(run: Run): string { const value = run.budget?.estimatedCostUsd; return typeof value === 'number' && value > 0 ? `$${value.toFixed(3)}` : '未产生'; }
function streamLabel(state: string): string { return { connecting: '连接中', live: '实时', reconnecting: '正在重连', offline: '离线' }[state] || state; }
function GraphProgress({ run }: { run: Run }) { const completed = new Set(run.completedNodeIds ?? []); const nodes = ['start', 'planner', 'validate-plan', 'plan-approval', 'prepare-workspace', 'implement', 'evaluate', 'judge', 'end']; return <ol className="graph-flow">{nodes.map((node) => <li key={node} className={completed.has(node) ? 'complete' : run.currentNodeId === node ? 'current' : ''}><i /><span>{node}</span></li>)}</ol>; }
function Timeline({ items }: { items: SequencedEvent[] }) { if (items.length === 0) return <PageState title="暂无事件" detail="Worker 开始处理后，追加事件会显示在这里。" />; return <ol className="timeline">{[...items].reverse().map((item) => <li key={item.sequence}><span className="timeline__dot" /><div><span className="timeline__meta"><b>#{item.sequence}</b>{formatDateTime(item.event.createdAt)}</span><strong>{eventTitle(item.event.type)}</strong><p>{item.event.message}</p>{item.event.nodeId && <code>{item.event.nodeId}</code>}</div></li>)}</ol>; }
function eventTitle(type: string): string { return type.split('_').map((word) => word[0]?.toUpperCase() + word.slice(1)).join(' '); }
