import { useQuery } from '@tanstack/react-query';
import { Link, useSearchParams } from 'react-router-dom';
import { listApprovals } from '../api/client';
import { LoadingRows, PageState } from '../components/States';
import { formatDateTime } from '../utils/format';

type Status = 'pending' | 'approved' | 'rejected';

export function ApprovalsPage() {
  const [params, setParams] = useSearchParams();
  const selected = params.get('status');
  const status = selected === 'approved' || selected === 'rejected' ? selected : 'pending';
  const query = useQuery({ queryKey: ['approvals', status], queryFn: () => listApprovals(status) });
  return <div className="page">
    <div className="page-heading"><div><span className="eyebrow">Human checkpoints</span><h1>Approvals</h1><p>审查 Agent 计划和高风险工具调用，所有决定都会进入审计日志。</p></div><div className="page-heading__meta"><strong>{query.data?.items.length ?? 0}</strong><span>当前筛选</span></div></div>
    <div className="filter-tabs" role="group" aria-label="审批状态筛选">
      {(['pending', 'approved', 'rejected'] as const).map((value) => <button key={value} type="button" className={status === value ? 'active' : ''} onClick={() => setParams({ status: value })}>{statusText(value)}</button>)}
    </div>
    {query.isPending ? <LoadingRows /> : query.error ? <PageState tone="danger" title="无法加载审批" detail="请检查 API 连接后重试。" /> : query.data.items.length === 0 ? <PageState title="此筛选下没有审批" detail="当 Run 到达人工检查点时，审批会显示在这里。" /> : <div className="approval-list">
      {query.data.items.map(({ request, runVersion }) => <Link className="approval-row" to={`/approvals/${request.approvalId}`} key={request.approvalId}>
        <span className={`risk risk--${request.risk}`}>{request.risk}</span>
        <span><strong>{actionText(request.actionType)}</strong><small>{request.reason}</small></span>
        <span className="mono">Run {request.runId.slice(0, 8)} · v{runVersion}</span>
        <time>{formatDateTime(request.requestedAt)}</time>
      </Link>)}
    </div>}
  </div>;
}

function statusText(status: Status): string { return { pending: '待处理', approved: '已批准', rejected: '已拒绝' }[status]; }
function actionText(action: string): string { return action === 'plan' || action === 'plan_approval' ? '执行计划审批' : action.replaceAll('_', ' '); }
