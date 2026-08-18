import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { listSessions, revokeSession, type Session } from '../api/client';
import { LoadingRows, PageState } from '../components/States';
import { formatDateTime } from '../utils/format';

export function SessionsPage() {
  const queryClient = useQueryClient();
  const query = useQuery({ queryKey: ['sessions'], queryFn: listSessions });
  const revoke = useMutation({ mutationFn: revokeSession, onSuccess: () => queryClient.invalidateQueries({ queryKey: ['sessions'] }) });
  return <div className="page">
    <div className="page-heading"><div><span className="eyebrow">Account security</span><h1>Sessions</h1><p>查看并撤销已登录设备。当前会话请使用右上角“退出”。</p></div></div>
    {query.isPending ? <LoadingRows /> : query.error ? <PageState tone="danger" title="无法加载会话" detail="请检查网络后重试。" /> : (
      <section className="session-list">{query.data.items.map((session) => <SessionRow key={session.id} session={session} current={session.id === query.data.currentSessionId} pending={revoke.isPending} onRevoke={() => revoke.mutate(session.id)} />)}</section>
    )}
  </div>;
}

function SessionRow({ session, current, pending, onRevoke }: { session: Session; current: boolean; pending: boolean; onRevoke: () => void }) {
  return <article className="session-row"><div className="session-icon" aria-hidden="true">◇</div><div><strong>{deviceName(session.userAgent)}</strong><span>{session.sourceIp || '未知来源'} · 最近活动 {formatDateTime(session.lastSeenAt)}</span><small>到期时间 {formatDateTime(session.expiresAt)}</small></div>{current ? <span className="current-session">当前会话</span> : <button className="danger-button" type="button" disabled={pending} onClick={onRevoke}>撤销</button>}</article>;
}
function deviceName(userAgent: string): string { if (/Edg/i.test(userAgent)) return 'Microsoft Edge'; if (/Chrome/i.test(userAgent)) return 'Google Chrome'; if (/Firefox/i.test(userAgent)) return 'Mozilla Firefox'; if (/Safari/i.test(userAgent)) return 'Safari'; return userAgent || '未知设备'; }
