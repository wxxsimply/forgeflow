import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, render, screen, waitFor } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import type { components } from '../api/schema';
import { useRunEventStream } from './useRunEventStream';

type EventPage = components['schemas']['EventPage'];

class FakeEventSource {
  static latest: FakeEventSource | null = null;
  readonly url: string;
  readonly withCredentials = true;
  readyState = 1;
  onopen: ((event: Event) => void) | null = null;
  onmessage: ((event: MessageEvent<string>) => void) | null = null;
  onerror: ((event: Event) => void) | null = null;
  constructor(url: string | URL) { this.url = String(url); FakeEventSource.latest = this; queueMicrotask(() => this.onopen?.(new Event('open'))); }
  close() { this.readyState = 2; }
  addEventListener() { /* EventSource contract */ }
  removeEventListener() { /* EventSource contract */ }
  dispatchEvent() { return true; }
}

afterEach(() => { sessionStorage.clear(); vi.unstubAllGlobals(); FakeEventSource.latest = null; });

it('resumes from the latest sequence and appends future event types without a whitelist', async () => {
  vi.stubGlobal('EventSource', FakeEventSource);
  const queryClient = new QueryClient();
  queryClient.setQueryData<EventPage>(['runs', 'run-1', 'events'], { items: [], nextCursor: 5 });
  function Harness() { const state = useRunEventStream('run-1', 5, true); return <span>{state}</span>; }
  render(<QueryClientProvider client={queryClient}><Harness /></QueryClientProvider>);
  await waitFor(() => expect(screen.getByText('live')).toBeInTheDocument());
  expect(FakeEventSource.latest?.url).toContain('after=5');
  const event = { eventId: '00000000-0000-4000-8000-000000000020', runId: 'run-1', traceId: 'trace-1', type: 'future_event_type', message: 'Future event', createdAt: '2026-08-10T08:00:00Z' };
  act(() => FakeEventSource.latest?.onmessage?.(new MessageEvent('message', { data: JSON.stringify(event), lastEventId: '6' })));
  await waitFor(() => expect(queryClient.getQueryData<EventPage>(['runs', 'run-1', 'events'])?.items).toHaveLength(1));
  act(() => FakeEventSource.latest?.onmessage?.(new MessageEvent('message', { data: JSON.stringify(event), lastEventId: '6' })));
  expect(queryClient.getQueryData<EventPage>(['runs', 'run-1', 'events'])?.items).toHaveLength(1);
  expect(sessionStorage.getItem('forgeflow:last-event:run-1')).toBe('6');
});

it('does not open a stream after a terminal run disables live updates', () => {
  vi.stubGlobal('EventSource', FakeEventSource);
  const queryClient = new QueryClient();
  function Harness() { const state = useRunEventStream('run-terminal', 9, false); return <span>{state}</span>; }
  render(<QueryClientProvider client={queryClient}><Harness /></QueryClientProvider>);
  expect(FakeEventSource.latest).toBeNull();
});
