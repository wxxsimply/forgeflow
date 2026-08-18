import type { ReactNode } from 'react';

export function FullPageStatus({ title, detail, busy = false }: { title: string; detail: string; busy?: boolean }) {
  return (
    <main className="full-status" aria-busy={busy}>
      <div className="status-mark" aria-hidden="true">{busy ? <span className="spinner" /> : '!'}</div>
      <h1>{title}</h1>
      <p>{detail}</p>
    </main>
  );
}

export function PageState({ title, detail, action, tone = 'neutral' }: { title: string; detail: string; action?: ReactNode; tone?: 'neutral' | 'danger' }) {
  return (
    <section className={`page-state page-state--${tone}`} role={tone === 'danger' ? 'alert' : 'status'}>
      <span className="page-state__line" aria-hidden="true" />
      <h2>{title}</h2>
      <p>{detail}</p>
      {action}
    </section>
  );
}

export function LoadingRows({ count = 5 }: { count?: number }) {
  return <div className="loading-rows" aria-label="正在加载" aria-busy="true">
    {Array.from({ length: count }, (_, index) => <span key={index} style={{ animationDelay: `${index * 70}ms` }} />)}
  </div>;
}
