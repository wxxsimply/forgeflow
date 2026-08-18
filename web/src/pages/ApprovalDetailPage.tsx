import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import { APIError, decideApproval, getApproval, getRun } from '../api/client';
import { useAuth } from '../auth/AuthProvider';
import { LoadingRows, PageState } from '../components/States';
import { formatDateTime } from '../utils/format';

export function ApprovalDetailPage() {
  const { approvalId = '' } = useParams();
  const { user } = useAuth();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [comment, setComment] = useState('');
  const [conflict, setConflict] = useState(false);
  const approvalQuery = useQuery({ queryKey: ['approvals', approvalId], queryFn: () => getApproval(approvalId), enabled: Boolean(approvalId) });
  const runId = approvalQuery.data?.approval.request.runId ?? '';
  const runQuery = useQuery({ queryKey: ['runs', runId], queryFn: () => getRun(runId), enabled: Boolean(runId) });
  const decision = useMutation({
    mutationFn: (value: 'approve' | 'reject') => decideApproval(approvalId, approvalQuery.data!.etag, value, comment.trim()),
    onSuccess: async (run) => {
      queryClient.setQueryData(['runs', run.runId], run);
      await queryClient.invalidateQueries({ queryKey: ['approvals'] });
      navigate(`/runs/${run.runId}`);
    },
    onError: async (error) => {
      if (error instanceof APIError && error.status === 409) {
        setConflict(true);
        await approvalQuery.refetch();
      }
    },
  });

  if (approvalQuery.isPending) return <div className="page"><LoadingRows count={7} /></div>;
  if (approvalQuery.error) return <div className="page"><PageState tone="danger" title="审批不存在或不可访问" detail="它可能已被删除，或当前账号没有权限。" action={<Link className="secondary-button" to="/approvals">返回审批列表</Link>} /></div>;
  const { request, runVersion } = approvalQuery.data.approval;
  const canDecide = user?.role !== 'viewer' && request.status === 'pending';

  function decide(value: 'approve' | 'reject') {
    const label = value === 'approve' ? '批准' : '拒绝';
    if (window.confirm(`确认${label}此审批？该决定会被永久记录。`)) decision.mutate(value);
  }

  return <div className="page approval-detail">
    <Link className="back-link" to="/approvals">← 返回 Approvals</Link>
    <div className="detail-heading"><div><span className="eyebrow">Approval · v{runVersion}</span><h1>{request.actionType === 'plan' || request.actionType === 'plan_approval' ? '执行计划审批' : request.actionType}</h1><p>请求于 {formatDateTime(request.requestedAt)}</p></div><span className={`status status--${request.status} status--large`}>{request.status}</span></div>
    {conflict && <div className="offline-banner" role="alert">审批已被其他人更新，页面已重新加载。请基于最新版本再次检查。</div>}
    <div className="approval-layout">
      <section className="panel evidence-panel"><div className="panel-heading"><div><span className="eyebrow">Decision evidence</span><h2>计划与风险</h2></div><span className={`risk risk--${request.risk}`}>{request.risk}</span></div>
        <div className="evidence-body"><h3>请求原因</h3><p>{request.reason}</p>
          <h3>影响范围</h3>{request.scope.length ? <ul className="file-list">{request.scope.map((item) => <li key={item}><code>{item}</code></li>)}</ul> : <p className="muted">未声明额外范围</p>}
          {runQuery.isPending ? <LoadingRows count={3} /> : runQuery.data?.plan ? <Plan plan={runQuery.data.plan} /> : <p className="muted">此审批没有可展示的执行计划。</p>}
        </div>
      </section>
      <aside className="panel decision-panel"><div className="panel-heading"><div><span className="eyebrow">Human decision</span><h2>审批决定</h2></div></div><div className="evidence-body">
        {request.status !== 'pending' ? <><p>状态：<strong>{request.status}</strong></p>{request.comment && <p>{request.comment}</p>}</> : canDecide ? <>
          <label htmlFor="approval-comment">审批备注</label><textarea id="approval-comment" rows={6} value={comment} onChange={(event) => setComment(event.target.value)} maxLength={4000} placeholder="记录判断依据、约束或拒绝原因…" />
          {decision.error && !conflict && <div className="form-error" role="alert">{decision.error.message}</div>}
          <div className="decision-actions"><button className="danger-button" disabled={decision.isPending} onClick={() => decide('reject')}>拒绝</button><button className="primary-button primary-button--fit" disabled={decision.isPending} onClick={() => decide('approve')}>批准并继续</button></div>
        </> : <PageState title="只读模式" detail="viewer 可以检查审批证据，但不能提交决定。" />}
        <Link className="secondary-button view-run-link" to={`/runs/${request.runId}`}>查看关联 Run</Link>
      </div></aside>
    </div>
  </div>;
}

function Plan({ plan }: { plan: NonNullable<Awaited<ReturnType<typeof getRun>>['plan']> }) {
  return <><h3>计划摘要</h3><p>{plan.summary}</p><h3>执行步骤</h3><ol className="plan-steps">{plan.steps.map((step) => <li key={step.id}><strong>{step.id}</strong><span>{step.description}</span>{step.acceptanceCriteria.length > 0 && <small>验收：{step.acceptanceCriteria.join('；')}</small>}</li>)}</ol><h3>可能影响的文件</h3><ul className="file-list">{plan.filesLikelyAffected.map((file) => <li key={file}><code>{file}</code></li>)}</ul>{plan.risks.length > 0 && <><h3>计划风险</h3><ul>{plan.risks.map((risk, index) => <li key={`${risk.level}-${index}`}><strong>{risk.level}：</strong>{risk.description}</li>)}</ul></>}</>;
}
