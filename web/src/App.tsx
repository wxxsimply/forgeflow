import { Component, type ErrorInfo, type PropsWithChildren, type ReactNode } from 'react';
import { Navigate, Outlet, Route, Routes, useLocation } from 'react-router-dom';
import { useAuth } from './auth/AuthProvider';
import { AppShell } from './components/AppShell';
import { FullPageStatus } from './components/States';
import { ApprovalDetailPage } from './pages/ApprovalDetailPage';
import { ApprovalsPage } from './pages/ApprovalsPage';
import { LoginPage } from './pages/LoginPage';
import { EvalDetailPage, EvalsPage } from './pages/EvalsPage';
import { NewRunPage } from './pages/NewRunPage';
import { RunDetailPage } from './pages/RunDetailPage';
import { RunEvidencePage } from './pages/RunEvidencePage';
import { RunsPage } from './pages/RunsPage';
import { SessionsPage } from './pages/SessionsPage';

export function App() {
  return <ErrorBoundary><Routes>
    <Route path="/login" element={<LoginPage />} />
    <Route element={<ProtectedRoute />}><Route element={<AppShell />}>
      <Route index element={<Navigate to="/runs" replace />} />
      <Route path="/runs" element={<RunsPage />} />
      <Route path="/runs/new" element={<NewRunPage />} />
      <Route path="/runs/:runId" element={<RunDetailPage />} />
      <Route path="/runs/:runId/artifacts" element={<RunEvidencePage view="artifacts" />} />
      <Route path="/runs/:runId/trace" element={<RunEvidencePage view="trace" />} />
      <Route path="/runs/:runId/report" element={<RunEvidencePage view="report" />} />
      <Route path="/approvals" element={<ApprovalsPage />} />
      <Route path="/approvals/:approvalId" element={<ApprovalDetailPage />} />
      <Route path="/sessions" element={<SessionsPage />} />
      <Route path="/evals" element={<EvalsPage />} />
      <Route path="/evals/:evalRunId" element={<EvalDetailPage />} />
    </Route></Route>
    <Route path="*" element={<Navigate to="/runs" replace />} />
  </Routes></ErrorBoundary>;
}

function ProtectedRoute() {
  const auth = useAuth();
  const location = useLocation();
  if (auth.loading) return <FullPageStatus title="正在恢复会话" detail="正在验证安全会话，请稍候。" busy />;
  if (auth.error) return <FullPageStatus title="无法连接 ForgeFlow" detail="请检查 API 服务和网络后刷新页面。" />;
  if (!auth.user) return <Navigate to={`/login?next=${encodeURIComponent(`${location.pathname}${location.search}`)}`} replace />;
  return <Outlet />;
}

type ErrorBoundaryState = { error: Error | null };
class ErrorBoundary extends Component<PropsWithChildren, ErrorBoundaryState> {
  state: ErrorBoundaryState = { error: null };
  static getDerivedStateFromError(error: Error): ErrorBoundaryState { return { error }; }
  componentDidCatch(error: Error, info: ErrorInfo) { console.error('UI boundary', error.name, info.componentStack); }
  render(): ReactNode {
    if (this.state.error) return <FullPageStatus title="页面出现问题" detail="请刷新页面重试；错误详情不会显示在界面中。" />;
    return this.props.children;
  }
}
