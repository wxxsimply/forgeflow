import { NavLink, Outlet, useNavigate } from 'react-router-dom';
import { useAuth } from '../auth/AuthProvider';
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
      </nav>
      <div className="sidebar__footer"><span className="environment"><i /> 控制台已连接</span><span>交付管理控制台</span></div>
    </aside>
    <div className="app-content"><header className="topbar"><div><span className="eyebrow">工作空间</span><strong>交付控制台</strong></div><div className="user-menu"><span className={`role role--${user?.role}`}>{roleLabel(user?.role)}</span><span className="user-menu__email">{user?.email}</span><button type="button" className="text-button" onClick={handleLogout}>退出</button></div></header><main className="main-content"><Outlet /></main></div>
  </div>;
}

function PlusIcon() { return <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M11 4h2v7h7v2h-7v7h-2v-7H4v-2h7z" /></svg>; }
function RunIcon() { return <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M5 5h14v14H5zM9 9l6 3-6 3z" /></svg>; }
function ApprovalIcon() { return <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M5 3h14v18H5zm3 5h8V6H8zm0 4h8v-2H8zm0 4h5v-2H8z" /></svg>; }
function EvalIcon() { return <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M4 19h16v2H4zm2-2V9h3v8zm5 0V3h3v14zm5 0v-5h3v5z" /></svg>; }
function SessionIcon() { return <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 3a5 5 0 0 0-5 5v2H5v11h14V10h-2V8a5 5 0 0 0-5-5Zm-3 7V8a3 3 0 0 1 6 0v2Z" /></svg>; }
