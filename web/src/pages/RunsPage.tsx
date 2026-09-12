import { useInfiniteQuery } from '@tanstack/react-query';
import { Link } from 'react-router-dom';
import { APIError, listRuns, type Run } from '../api/client';
import { LoadingRows, PageState } from '../components/States';
import { formatDateTime, formatDuration, shortPath, statusLabel } from '../utils/format';
import { nodeLabel } from '../utils/labels';

export function RunsPage() {
  const query = useInfiniteQuery({
    queryKey: ['runs'],
    queryFn: ({ pageParam }) => listRuns(pageParam),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (page) => page.nextCursor,
  });
  const runs = query.data?.pages.flatMap((page) => page.items) ?? [];
  const offline = typeof navigator !== 'undefined' && !navigator.onLine;
  return (
    <div className="page">
      <div className="page-heading">
        <div><span className="eyebrow">执行记录</span><h1>运行任务</h1><p>查看 智能体工作流状态、当前节点和可验证事件。</p></div>
        <div className="page-heading__meta"><strong>{runs.length}</strong><span>当前已加载</span></div>
      </div>
      {offline && <div className="offline-banner" role="status">当前处于离线状态，显示的是最后一次缓存结果。</div>}
      {query.isPending ? <LoadingRows /> : query.error ? <RunsError error={query.error} retry={() => query.refetch()} /> : runs.length === 0 ? (
        <PageState title="还没有运行任务" detail="创建第一个任务后，它会出现在这里；只读用户可等待管理员创建任务。" />
      ) : (
        <section className="run-table-wrap" aria-label="任务列表">
          <div className="run-table-header"><span>任务</span><span>状态</span><span>当前节点</span><span>耗时</span><span>更新时间</span></div>
          <div className="run-list">{runs.map((run) => <RunRow key={run.runId} run={run} />)}</div>
          {query.hasNextPage && <button className="secondary-button load-more" disabled={query.isFetchingNextPage} onClick={() => query.fetchNextPage()}>{query.isFetchingNextPage ? '正在加载' : '加载更多'}</button>}
        </section>
      )}
    </div>
  );
}

function RunRow({ run }: { run: Run }) {
  return <Link className="run-row" to={`/runs/${run.runId}`}>
    <span className="run-task"><strong>{run.task}</strong><small>{shortPath(run.repositoryPath)} · {run.baseRevision}</small></span>
    <span><span className={`status status--${run.status}`}>{statusLabel(run.status)}</span></span>
    <span className="mono">{nodeLabel(run.currentNodeId)}</span>
    <span>{formatDuration(run.createdAt, run.updatedAt)}</span>
    <span>{formatDateTime(run.updatedAt)}</span>
  </Link>;
}

function RunsError({ error, retry }: { error: Error; retry: () => void }) {
  const forbidden = error instanceof APIError && error.status === 403;
  return <PageState tone="danger" title={forbidden ? '没有访问权限' : '无法加载运行任务'} detail={forbidden ? '当前账号不能查看这些资源。' : '服务暂时不可用，请检查连接后重试。'} action={!forbidden && <button className="secondary-button" onClick={retry}>重新加载</button>} />;
}
