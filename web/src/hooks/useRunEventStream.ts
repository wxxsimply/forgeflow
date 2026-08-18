import { useQueryClient } from '@tanstack/react-query';
import { useEffect, useRef, useState } from 'react';
import { runEventStreamURL, type RunEvent, type SequencedEvent } from '../api/client';
import type { components } from '../api/schema';

type EventPage = components['schemas']['EventPage'];
export type StreamState = 'connecting' | 'live' | 'reconnecting' | 'offline';

export function useRunEventStream(runId: string, initialAfter: number, enabled: boolean): StreamState {
  const queryClient = useQueryClient();
  const sequence = useRef(Math.max(initialAfter, savedSequence(runId)));
  const [state, setState] = useState<StreamState>(navigator.onLine ? 'connecting' : 'offline');

  useEffect(() => {
	sequence.current = Math.max(sequence.current, initialAfter);
	if (!enabled) return;
    let source: EventSource | null = null;
    let timer = 0;
    let stopped = false;
    let attempt = 0;
    const receive = (message: MessageEvent<string>) => {
      const nextSequence = Number(message.lastEventId);
      if (!Number.isSafeInteger(nextSequence) || nextSequence <= sequence.current) return;
      try {
        const event = JSON.parse(message.data) as RunEvent;
        const item: SequencedEvent = { sequence: nextSequence, event };
        sequence.current = nextSequence;
        sessionStorage.setItem(storageKey(runId), String(nextSequence));
        queryClient.setQueryData<EventPage>(['runs', runId, 'events'], (current) => {
          const items = current?.items ?? [];
          if (items.some((existing) => existing.sequence === nextSequence)) return current;
          return { items: [...items, item].slice(-500), nextCursor: nextSequence };
        });
        void queryClient.invalidateQueries({ queryKey: ['runs', runId] });
      } catch { /* Invalid events are ignored and never shown as evidence. */ }
    };
    const connect = () => {
      if (stopped || !navigator.onLine) { setState('offline'); return; }
      setState(attempt === 0 ? 'connecting' : 'reconnecting');
      source = new EventSource(runEventStreamURL(runId, sequence.current), { withCredentials: true });
      source.onopen = () => { attempt = 0; setState('live'); };
      source.onmessage = receive;
      source.onerror = () => {
        source?.close();
        if (stopped) return;
        attempt += 1;
        setState(navigator.onLine ? 'reconnecting' : 'offline');
        timer = window.setTimeout(connect, Math.min(10_000, 500 * 2 ** Math.min(attempt, 5)));
      };
    };
    const online = () => { window.clearTimeout(timer); attempt = 0; connect(); };
    const offline = () => { source?.close(); window.clearTimeout(timer); setState('offline'); };
    window.addEventListener('online', online);
    window.addEventListener('offline', offline);
    connect();
    return () => {
      stopped = true; source?.close(); window.clearTimeout(timer);
      window.removeEventListener('online', online); window.removeEventListener('offline', offline);
    };
  }, [enabled, initialAfter, queryClient, runId]);
  return state;
}

function storageKey(runId: string): string { return `forgeflow:last-event:${runId}`; }
function savedSequence(runId: string): number {
  const value = Number(sessionStorage.getItem(storageKey(runId)) || 0);
  return Number.isSafeInteger(value) && value > 0 ? value : 0;
}
