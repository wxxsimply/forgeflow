import { useQuery } from '@tanstack/react-query';
import { Link, useParams } from 'react-router-dom';
import { getEvalRun, listAgents, listEvalRuns, listPrompts, type EvalRun } from '../api/client';
import { LoadingRows, PageState } from '../components/States';
import { formatDateTime } from '../utils/format';

export function EvalsPage() {
  const evals = useQuery({ queryKey: ['evals'], queryFn: listEvalRuns });
  const agents = useQuery({ queryKey: ['agents'], queryFn: listAgents });
  const prompts = useQuery({ queryKey: ['prompts'], queryFn: listPrompts });
  if (evals.isPending || agents.isPending || prompts.isPending) return <div className="page"><Heading /><LoadingRows /></div>;
  if (evals.error || agents.error || prompts.error) return <div className="page"><Heading /><PageState tone="danger" title="无法加载评测治理数据" detail="请检查 API 和数据库 Migration 后重试。" /></div>;
  return <div className="page"><Heading />
    <section className="eval-catalog" aria-label="Agent 与 Prompt 版本">
      {agents.data.items.map((agent) => <article key={agent.name}><span className="eyebrow">{agent.role}</span><h2>{agent.name}</h2><p><code>{agent.model}</code></p><small>{agent.promptVersion}</small></article>)}
    </section>
    <div className="eval-release-note">已嵌入 {prompts.data.items.length} 个 Prompt；Promotion/回滚历史 {prompts.data.releases.length} 条。只有管理员可以通过 API 导入真实 Evidence 和执行 Promotion。</div>
    {evals.data.items.length === 0 ? <PageState title="尚无真实 Eval 报告" detail="运行 30 Case 的三种基线，将 Evidence 提交到 POST /api/v1/evals/runs。界面不会生成或展示虚构成绩。" /> : <section className="eval-list">{evals.data.items.map((run) => <EvalRow key={run.id} run={run} />)}</section>}
  </div>;
}

function Heading() { return <div className="page-heading"><div><span className="eyebrow">Quality evidence</span><h1>Evals</h1><p>比较单 Agent、Planner + Developer 和完整 ForgeFlow 的真实固定任务证据。</p></div></div>; }

function EvalRow({ run }: { run: EvalRun }) {
  const forgeflow = run.report.reports.find((report) => report.configuration.mode === 'forgeflow');
  return <Link to={`/evals/${run.id}`} className="eval-row"><span><strong>{run.dataset}</strong><small>{run.datasetVersion}</small></span><span>{forgeflow ? `${forgeflow.passed}/${forgeflow.total}` : '—'}</span><span>{forgeflow ? percent(forgeflow.metrics.completionRate) : '—'}</span><time>{formatDateTime(run.createdAt)}</time></Link>;
}

export function EvalDetailPage() {
  const { evalRunId = '' } = useParams();
  const query = useQuery({ queryKey: ['eval', evalRunId], queryFn: () => getEvalRun(evalRunId), enabled: Boolean(evalRunId) });
  if (query.isPending) return <div className="page"><LoadingRows /></div>;
  if (query.error || !query.data) return <div className="page"><PageState tone="danger" title="无法加载 Eval 报告" detail="报告不存在或当前会话无权访问。" /></div>;
  return <div className="page"><Link className="back-link" to="/evals">← 返回 Evals</Link><div className="detail-heading"><div><span className="eyebrow">Comparison report</span><h1>{query.data.dataset}</h1><p>{query.data.datasetVersion} · {formatDateTime(query.data.createdAt)}</p></div></div><section className="eval-comparison">{query.data.report.reports.map((report) => <article key={report.configuration.mode}><span className="eyebrow">{modeLabel(report.configuration.mode)}</span><h2>{report.passed}/{report.total}</h2><dl><div><dt>完成率</dt><dd>{percent(report.metrics.completionRate)}</dd></div><div><dt>隐藏测试</dt><dd>{percent(report.metrics.hiddenTestPassRate)}</dd></div><div><dt>回归率</dt><dd>{percent(report.metrics.regressionRate)}</dd></div><div><dt>人工介入</dt><dd>{percent(report.metrics.humanInterventionRate)}</dd></div><div><dt>平均成本</dt><dd>{report.metrics.averageCostUsd == null ? 'N/A' : `$${report.metrics.averageCostUsd.toFixed(4)}`}</dd></div><div><dt>P95 延迟</dt><dd>{report.metrics.p95LatencyMs == null ? 'N/A' : `${Math.round(report.metrics.p95LatencyMs)} ms`}</dd></div></dl><code className="eval-commit">{report.configuration.gitCommit}</code></article>)}</section></div>;
}

function percent(value: number) { return `${(value * 100).toFixed(1)}%`; }
function modeLabel(mode: string) { return mode === 'single_agent' ? 'Single Agent' : mode === 'planner_developer' ? 'Planner + Developer' : 'ForgeFlow'; }
