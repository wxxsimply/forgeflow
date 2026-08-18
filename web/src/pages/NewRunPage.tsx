import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { type FormEvent, useRef, useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { APIError, createRepository, createRun, listRepositories } from '../api/client';
import { useAuth } from '../auth/AuthProvider';
import { LoadingRows, PageState } from '../components/States';

export function NewRunPage() {
  const { user } = useAuth();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const idempotencyKey = useRef(crypto.randomUUID());
  const repositories = useQuery({ queryKey: ['repositories'], queryFn: () => listRepositories() });
  const [repositoryId, setRepositoryId] = useState('');
  const [task, setTask] = useState('');
  const [baseRevision, setBaseRevision] = useState('');
  const [maxIterations, setMaxIterations] = useState(2);
  const [showRepositoryForm, setShowRepositoryForm] = useState(false);
  const [repository, setRepository] = useState({ name: '', localPath: '', defaultBranch: 'HEAD' });

  const registerMutation = useMutation({
    mutationFn: createRepository,
    onSuccess: async (created) => {
      setRepositoryId(created.id);
      setShowRepositoryForm(false);
      await queryClient.invalidateQueries({ queryKey: ['repositories'] });
    },
  });
  const runMutation = useMutation({
    mutationFn: () => createRun({ repositoryId, task: task.trim(), baseRevision: baseRevision.trim() || undefined, maxIterations }, idempotencyKey.current),
    onSuccess: (run) => navigate(`/runs/${run.runId}`),
  });

  if (user?.role === 'viewer') return <div className="page"><PageState tone="danger" title="只读账号无法创建 Run" detail="请使用 admin 或 operator 账号执行变更任务。" action={<Link className="secondary-button" to="/runs">返回 Runs</Link>} /></div>;
  if (repositories.isPending) return <div className="page"><LoadingRows count={5} /></div>;
  if (repositories.error) return <div className="page"><PageState tone="danger" title="无法加载仓库" detail="请检查 API 连接后重试。" /></div>;
  const items = repositories.data.items;

  function submitRun(event: FormEvent) {
    event.preventDefault();
    if (!repositoryId || !task.trim()) return;
    runMutation.mutate();
  }
  function submitRepository() {
    if (!repository.name.trim() || !repository.localPath.trim()) return;
    registerMutation.mutate(repository);
  }

  return <div className="page narrow-page">
    <Link className="back-link" to="/runs">← 返回 Runs</Link>
    <div className="page-heading"><div><span className="eyebrow">Governed execution</span><h1>创建 Run</h1><p>选择受控仓库，描述任务，并确认本次执行预算。</p></div></div>
    <form className="panel form-panel" onSubmit={submitRun}>
      <div className="form-section">
        <div className="section-number">01</div><div className="form-section__body">
          <label htmlFor="repository">仓库</label>
          <select id="repository" value={repositoryId} onChange={(event) => setRepositoryId(event.target.value)} required>
            <option value="">请选择仓库</option>
            {items.map((item) => <option key={item.id} value={item.id}>{item.name} · {item.defaultBranch}</option>)}
          </select>
          <button className="text-button inline-action" type="button" onClick={() => setShowRepositoryForm((value) => !value)}>{showRepositoryForm ? '收起仓库登记' : '+ 登记本地仓库'}</button>
        </div>
      </div>
      {showRepositoryForm && <fieldset className="nested-form">
        <legend>登记仓库</legend>
        <label htmlFor="repo-name">名称</label><input id="repo-name" value={repository.name} onChange={(e) => setRepository({ ...repository, name: e.target.value })} required />
        <label htmlFor="repo-path">本地绝对路径</label><input id="repo-path" value={repository.localPath} onChange={(e) => setRepository({ ...repository, localPath: e.target.value })} placeholder="D:\Code\project" required />
        <label htmlFor="repo-branch">默认基线</label><input id="repo-branch" value={repository.defaultBranch} onChange={(e) => setRepository({ ...repository, defaultBranch: e.target.value })} />
        {registerMutation.error && <ErrorMessage error={registerMutation.error} />}
        <button className="secondary-button" type="button" disabled={registerMutation.isPending || !repository.name.trim() || !repository.localPath.trim()} onClick={submitRepository}>{registerMutation.isPending ? '登记中…' : '登记仓库'}</button>
      </fieldset>}
      <div className="form-section">
        <div className="section-number">02</div><div className="form-section__body">
          <label htmlFor="task">任务描述</label>
          <textarea id="task" rows={8} maxLength={20000} value={task} onChange={(event) => setTask(event.target.value)} placeholder="说明期望改动、验收标准和禁止事项…" required />
          <span className="field-hint">{task.length} / 20,000 字符。任务会成为 Agent 的主要执行输入。</span>
        </div>
      </div>
      <div className="form-section form-section--split">
        <div className="section-number">03</div><div className="form-section__body">
          <label htmlFor="base-revision">基线版本（可选）</label><input id="base-revision" value={baseRevision} onChange={(event) => setBaseRevision(event.target.value)} placeholder="留空使用仓库默认基线" />
          <label htmlFor="max-iterations">最大迭代次数</label><input id="max-iterations" type="number" min={1} max={10} value={maxIterations} onChange={(event) => setMaxIterations(Number(event.target.value))} />
        </div>
        <aside className="budget-card" aria-label="默认安全预算">
          <span className="eyebrow">Safety budget</span><strong>最多 {maxIterations} 次迭代</strong>
          <ul><li>模型调用 20 次</li><li>工具调用 200 次</li><li>改动文件 32 个</li><li>差异 4,000 行 / 1 MiB</li><li>预计成本上限 $10</li><li>最长 30 分钟</li></ul>
        </aside>
      </div>
      {runMutation.error && <ErrorMessage error={runMutation.error} />}
      <div className="form-actions"><Link className="secondary-button" to="/runs">取消</Link><button className="primary-button primary-button--fit" disabled={!repositoryId || !task.trim() || runMutation.isPending}>{runMutation.isPending ? '正在创建…' : '创建并进入 Run'}</button></div>
    </form>
  </div>;
}

function ErrorMessage({ error }: { error: Error }) {
  const request = error instanceof APIError && error.requestId ? ` 请求 ID：${error.requestId}` : '';
  return <div className="form-error" role="alert">{error.message}{request}</div>;
}
