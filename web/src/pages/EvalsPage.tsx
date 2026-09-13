import { useQuery } from '@tanstack/react-query';
import { Link, useParams } from 'react-router-dom';
import { getEvalRun, listAgents, listEvalRuns, listPrompts, type EvalRun } from '../api/client';
import { LoadingRows, PageState } from '../components/States';
import { formatDateTime } from '../utils/format';
import { agentLabel, agentRoleLabel } from '../utils/labels';

export function EvalsPage() {
  const evals = useQuery({ queryKey: ['evals'], queryFn: listEvalRuns });
  const agents = useQuery({ queryKey: ['agents'], queryFn: listAgents });
  const prompts = useQuery({ queryKey: ['prompts'], queryFn: listPrompts });
  if (evals.isPending || agents.isPending || prompts.isPending) return <div className="page"><Heading /><LoadingRows /></div>;
  if (evals.error || agents.error || prompts.error) return <div className="page"><Heading /><PageState tone="danger" title="无法加载评测治理数据" detail="请确认服务可用且数据库迁移已完成后重试。" /></div>;
  return <div className="page"><Heading />
    <section className="eval-catalog" aria-label="智能体与提示词版本">
      {agents.data.items.map((agent) => <article key={agent.name}><span className="eyebrow">{agentRoleLabel(agent.role)}</span><h2>{agentLabel(agent.name)}</h2><p><code>{agent.model}</code></p><small>{agent.promptVersion}</small></article>)}
    </section>
    <div className="eval-release-note">已嵌入 {prompts.data.items.length} 个提示词；发布晋级与回滚历史 {prompts.data.releases.length} 条。只有管理员可以通过接口导入真实评测证据并执行发布晋级。</div>
    {evals.data.items.length === 0 ? <PageState title="尚无真实评测报告" detail="完成真实任务的三组基线评测后，由管理员导入结果。界面不会生成或展示虚构成绩。" /> : <section className="eval-list">{evals.data.items.map((run) => <EvalRow key={run.id} run={run} />)}</section>}
  </div>;
}

function Heading() { return <div className="page-heading"><div><span className="eyebrow">质量证据</span><h1>评测报告</h1><p>比较单智能体、规划与开发双智能体、完整 ForgeFlow 三种模式的真实任务结果。</p></div></div>; }

function EvalRow({ run }: { run: EvalRun }) {
  const forgeflow = run.report.reports.find((report) => report.configuration.mode === 'forgeflow');
  return <Link to={`/evals/${run.id}`} className="eval-row"><span><strong>{run.dataset}</strong><small>{run.datasetVersion}</small></span><span>{forgeflow ? `${forgeflow.passed}/${forgeflow.total}` : '—'}</span><span>{forgeflow ? percent(forgeflow.metrics.completionRate) : '—'}</span><time>{formatDateTime(run.createdAt)}</time></Link>;
}

export function EvalDetailPage() {
  const { evalRunId = '' } = useParams();
  const query = useQuery({ queryKey: ['eval', evalRunId], queryFn: () => getEvalRun(evalRunId), enabled: Boolean(evalRunId) });
  if (query.isPending) return <div className="page"><LoadingRows /></div>;
  if (query.error || !query.data) return <div className="page"><PageState tone="danger" title="无法加载评测报告" detail="报告不存在或当前会话无权访问。" /></div>;
  return <div className="page"><Link className="back-link" to="/evals">← 返回评测列表</Link><div className="detail-heading"><div><span className="eyebrow">对比报告</span><h1>{query.data.dataset}</h1><p>{query.data.datasetVersion} · {formatDateTime(query.data.createdAt)}</p></div></div><section className="eval-comparison">{query.data.report.reports.map((report) => <article key={report.configuration.mode}><span className="eyebrow">{modeLabel(report.configuration.mode)}</span><h2>{report.passed}/{report.total}</h2><dl><div><dt>完成率</dt><dd>{percent(report.metrics.completionRate)}</dd></div><div><dt>隐藏测试</dt><dd>{percent(report.metrics.hiddenTestPassRate)}</dd></div><div><dt>回归率</dt><dd>{percent(report.metrics.regressionRate)}</dd></div><div><dt>人工介入</dt><dd>{percent(report.metrics.humanInterventionRate)}</dd></div><div><dt>平均成本</dt><dd>{report.metrics.averageCostUsd == null ? '暂无数据' : `$${report.metrics.averageCostUsd.toFixed(4)}`}</dd></div><div><dt>95 分位延迟</dt><dd>{report.metrics.p95LatencyMs == null ? '暂无数据' : `${Math.round(report.metrics.p95LatencyMs)} 毫秒`}</dd></div></dl><code className="eval-commit">{report.configuration.gitCommit}</code></article>)}</section></div>;
}

function percent(value: number) { return `${(value * 100).toFixed(1)}%`; }
function modeLabel(mode: string) { return mode === 'single_agent' ? '单智能体' : mode === 'planner_developer' ? '规划与开发双智能体' : 'ForgeFlow'; }
