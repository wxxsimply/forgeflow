import { NavLink, Outlet, useNavigate } from 'react-router-dom';
import { useAuth } from '../auth/AuthProvider';
import type { User } from '../api/client';
import { roleLabel } from '../utils/labels';

export function AppShell() {
  const { user, signOut } = useAuth();
  const navigate = useNavigate();
  async function handleLogout() { try { await signOut(); } finally { navigate('/login', { replace: true }); } }
  const navClass = ({ isActive }: { isActive: boolean }) => isActive ? 'nav-item nav-item--active' : 'nav-item';
  return <div className="app-frame">
    <aside className="sidebar">
      <NavLink to="/runs" className="brand" aria-label="ForgeFlow 任务列表"><span className="brand__mark" aria-hidden="true"><i /><i /><i /></span><span><strong>ForgeFlow</strong><small>可控交付</small></span></NavLink>
      <nav aria-label="主导航">
        {user?.role !== 'viewer' && <NavLink to="/runs/new" className={navClass}><PlusIcon /><span>新建任务</span></NavLink>}
        <NavLink to="/runs" end className={navClass}><RunIcon /><span>运行任务</span></NavLink>
        <NavLink to="/approvals" className={navClass}><ApprovalIcon /><span>审批中心</span></NavLink>
        <NavLink to="/evals" className={navClass}><EvalIcon /><span>评测报告</span></NavLink>
        <NavLink to="/sessions" className={navClass}><SessionIcon /><span>登录设备</span></NavLink>
        <NavLink to="/account" className={navClass}><AccountIcon /><span>数据与账户</span></NavLink>
      </nav>
      <div className="sidebar__footer"><span className="environment"><i /> 控制台已连接</span><span>交付管理控制台</span></div>
    </aside>
    <div className="app-content">
      <header className="topbar"><div><span className="eyebrow">工作空间</span><strong>交付控制台</strong></div><div className="user-menu"><span className={`role role--${user?.role}`}>{roleLabel(user?.role)}</span><span className="user-menu__email">{user?.email}</span><button type="button" className="text-button" onClick={handleLogout}>退出</button></div></header>
      <PreviewScopeNotice role={user?.role} />
      <main className="main-content"><Outlet /></main>
    </div>
  </div>;
}

function PreviewScopeNotice({ role }: { role?: User['role'] }) {
  return <section className="preview-scope" aria-labelledby="preview-scope-title">
    <div className="preview-scope__title"><span className="eyebrow">产品说明 · 当前为模拟预览</span><h2 id="preview-scope-title">ForgeFlow 管理 AI 辅助开发任务的计划、审批与执行记录。</h2><p>当前网页只模拟这套流程，不会调用模型或修改代码。</p></div>
    <div className="preview-scope__items">
      <article><strong>你可以体验</strong><p>选择受控仓库，创建模拟任务，查看计划并批准或拒绝，之后查看任务状态和审计记录。</p></article>
      <article><strong>从这里开始</strong><p>按“新建任务 → 检查计划 → 作出审批 → 查看记录”的顺序体验完整流程。</p></article>
      <article><strong>当前不会执行</strong><p>真实模型调用、源码修改、测试运行、生产发布或批量评测；不会产生模型费用。</p></article>
    </div>
    {role !== 'viewer' ? <NavLink className="preview-scope__action" to="/runs/new">开始体验模拟流程 <span aria-hidden="true">→</span></NavLink> : <p className="preview-scope__readonly">只读账号：请联系管理员创建模拟任务。</p>}
  </section>;
}

function PlusIcon() { return <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M11 4h2v7h7v2h-7v7h-2v-7H4v-2h7z" /></svg>; }
function RunIcon() { return <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M5 5h14v14H5zM9 9l6 3-6 3z" /></svg>; }
function ApprovalIcon() { return <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M5 3h14v18H5zm3 5h8V6H8zm0 4h8v-2H8zm0 4h5v-2H8z" /></svg>; }
function EvalIcon() { return <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M4 19h16v2H4zm2-2V9h3v8zm5 0V3h3v14zm5 0v-5h3v5z" /></svg>; }
function SessionIcon() { return <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 3a5 5 0 0 0-5 5v2H5v11h14V10h-2V8a5 5 0 0 0-5-5Zm-3 7V8a3 3 0 0 1 6 0v2Z" /></svg>; }
function AccountIcon() { return <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 2 4 5v6c0 5.1 3.4 9.7 8 11 4.6-1.3 8-5.9 8-11V5Zm0 4a3 3 0 1 1 0 6 3 3 0 0 1 0-6Zm0 13.8a9.3 9.3 0 0 1-4.8-3.5c.8-1.6 2.7-2.3 4.8-2.3s4 .7 4.8 2.3a9.3 9.3 0 0 1-4.8 3.5Z" /></svg>; }
