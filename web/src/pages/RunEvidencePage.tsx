import { useQuery } from '@tanstack/react-query';
import { Link, useParams } from 'react-router-dom';
import { getRun, getRunReport, listRunArtifacts, type RunReport } from '../api/client';
import { RunSubnav } from '../components/RunSubnav';
import { LoadingRows, PageState } from '../components/States';
import { formatDateTime, statusLabel } from '../utils/format';
import { actionLabel, agentLabel, artifactKindLabel, invocationStatusLabel, nodeLabel } from '../utils/labels';

export function RunEvidencePage({ view }: { view: 'artifacts' | 'trace' | 'report' }) {
  const { runId = '' } = useParams();
  const runQuery = useQuery({ queryKey: ['runs', runId], queryFn: () => getRun(runId), enabled: Boolean(runId) });
  const artifactsQuery = useQuery({ queryKey: ['runs', runId, 'artifacts'], queryFn: () => listRunArtifacts(runId), enabled: view === 'artifacts' && Boolean(runId) });
  const reportQuery = useQuery({ queryKey: ['runs', runId, 'report'], queryFn: () => getRunReport(runId), enabled: view === 'report' && Boolean(runId) });
  if (runQuery.isPending) return <div className="page"><LoadingRows count={6} /></div>;
  if (runQuery.error) return <div className="page"><PageState tone="danger" title="无法加载任务证据" detail="任务不存在或当前账号没有访问权限。" /></div>;
  const run = runQuery.data;
  return <div className="page evidence-page">
    <Link className="back-link" to={`/runs/${runId}`}>← 返回任务概览</Link>
    <div className="detail-heading"><div><span className="eyebrow">任务 · {run.runId.slice(0, 8)}</span><h1>{title(view)}</h1><p>{run.task}</p></div><span className={`status status--${run.status} status--large`}>{statusLabel(run.status)}</span></div>
    <RunSubnav runId={runId} />
    {view === 'artifacts' && <Artifacts run={run} loading={artifactsQuery.isPending} error={artifactsQuery.error} items={artifactsQuery.data?.items ?? []} />}
    {view === 'trace' && <Trace run={run} />}
    {view === 'report' && (reportQuery.isPending ? <LoadingRows count={5} /> : reportQuery.error ? <PageState tone="danger" title="报告加载失败" detail="请稍后重试。" /> : <Report report={reportQuery.data} />)}
  </div>;
}

function Artifacts({ run, loading, error, items }: { run: Awaited<ReturnType<typeof getRun>>; loading: boolean; error: Error | null; items: Awaited<ReturnType<typeof listRunArtifacts>>['items'] }) {
  return <div className="evidence-stack">
    <section className="panel"><div className="panel-heading"><div><span className="eyebrow">仓库证据</span><h2>代码差异</h2></div><span className="event-count">{run.diff?.changedFiles.length ?? run.changedFiles?.length ?? 0} 个文件</span></div>
      <div className="evidence-body">{run.diff?.patch || run.implementation?.patch ? <><div className="diff-meta"><code>sha256: {run.diff?.sha256 || '待生成'}</code><span>{run.diff?.size ?? (run.implementation?.patch.length || 0)} 字节</span></div><pre className="diff-view"><code>{run.diff?.patch || run.implementation?.patch}</code></pre></> : <PageState title="尚无代码差异" detail="实现节点产出补丁后会显示在这里。" />}</div>
    </section>
    <section className="panel"><div className="panel-heading"><div><span className="eyebrow">产物元数据</span><h2>持久化产物</h2></div></div><div className="evidence-body">{loading ? <LoadingRows count={3} /> : error ? <PageState tone="danger" title="产物加载失败" detail="请稍后重试。" /> : items.length === 0 ? <p className="muted">尚无持久化产物。</p> : <div className="artifact-list">{items.map((item) => <div key={item.id}><span className="risk">{artifactKindLabel(item.kind)}</span><strong>{item.contentType}</strong><code>{item.sha256}</code><small>{item.size} 字节 · {formatDateTime(item.createdAt)}</small></div>)}</div>}</div></section>
    {run.testAssessment && <section className="panel"><div className="panel-heading"><div><span className="eyebrow">测试证据</span><h2>{run.testAssessment.passed ? '测试通过' : '测试失败'}</h2></div><span className={`risk risk--${run.testAssessment.passed ? 'low' : 'high'}`}>退出码 {run.testAssessment.exitCode}</span></div><div className="evidence-body"><code>{[run.testAssessment.program, ...run.testAssessment.args].join(' ')}</code><pre className="log-view">{run.testAssessment.stdout || run.testAssessment.stderr || '命令未产生输出'}</pre></div></section>}
  </div>;
}

function Trace({ run }: { run: Awaited<ReturnType<typeof getRun>> }) {
  const models = run.modelInvocations ?? [];
  const tools = run.toolCallAudits ?? [];
  return <div className="evidence-stack"><section className="panel"><div className="panel-heading"><div><span className="eyebrow">模型调用记录</span><h2>模型调用</h2></div><span className="event-count">{models.length} 次调用</span></div><div className="evidence-body">{models.length === 0 ? <p className="muted">尚无模型调用记录。</p> : <div className="audit-table">{models.map((item, index) => <div key={`${item.promptSha256}-${index}`}><strong>{item.agent ? agentLabel(item.agent) : item.nodeId ? nodeLabel(item.nodeId) : '智能体'}</strong><span>{item.provider} / {item.model}</span><span>{invocationStatusLabel(item.status)}</span><span>{item.inputTokens ?? 0} → {item.outputTokens ?? 0} 个词元</span><code>{item.promptVersion || '未标注版本'} · {item.promptSha256?.slice(0, 12)}</code></div>)}</div>}</div></section>
    <section className="panel"><div className="panel-heading"><div><span className="eyebrow">策略执行记录</span><h2>工具调用</h2></div><span className="event-count">{tools.length} 次调用</span></div><div className="evidence-body">{tools.length === 0 ? <p className="muted">尚无工具调用记录。</p> : <div className="audit-table">{tools.map((item, index) => <div key={`${item.callId}-${index}`}><strong>{item.toolName}@{item.toolVersion}</strong><span>{item.agent ? agentLabel(item.agent) : nodeLabel(item.nodeId)}</span><span>{invocationStatusLabel(item.status)}</span><span>{actionLabel(item.policyAction)} · {item.policyRuleId}</span><code>{item.callId}</code></div>)}</div>}</div></section></div>;
}

function Report({ report }: { report: RunReport }) {
  return <div className="report-grid">
    <ReportSection title="计划" value={report.plan?.summary} />
    <ReportSection title="实现" value={report.implementation?.summary} />
    <ReportSection title="测试" value={report.tests ? `${report.tests.passed ? '通过' : '失败'} · 退出码 ${report.tests.exitCode}` : undefined} />
    <ReportSection title="代码审查" value={report.review?.summary} count={report.review?.findings.length} />
    <ReportSection title="安全审查" value={report.security?.summary} count={report.security?.findings.length} />
    <ReportSection title="最终判断" value={report.decision ? `${actionLabel(report.decision.action)}：${report.decision.reasons.join('；')}` : undefined} />
    {report.error && <section className="panel report-section report-section--wide"><span className="eyebrow">错误信息</span><h2>{report.error.code}</h2><p>{report.error.message}</p></section>}
  </div>;
}

function ReportSection({ title: sectionTitle, value, count }: { title: string; value?: string; count?: number }) { return <section className="panel report-section"><span className="eyebrow">{sectionTitle}</span><h2>{value ? '已生成' : '尚无结果'}{typeof count === 'number' ? ` · ${count} 条发现` : ''}</h2><p>{value || '该阶段尚未完成。'}</p></section>; }
function title(view: 'artifacts' | 'trace' | 'report'): string { return { artifacts: '产物与代码差异', trace: '模型与工具调用审计', report: '最终执行报告' }[view]; }
