// Copyright 2024 The Kubetail Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

import { act, render, renderHook, waitFor } from '@testing-library/react';
import type { ReactNode } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import type { Cluster } from '@/lib/clusters';
import type {
  Incident,
  IncidentOccurrence,
  MonitorTarget,
  MonitorTargetEnabledSet,
  IncidentTriageRequest,
} from '@/lib/incidents';
import { clusterRecord, incidentRecord, mockTauriCore, monitorTargetRecord, occurrenceRecord } from '@/test-utils';

const { invokeMock, channels, factory } = mockTauriCore();
vi.mock('@tauri-apps/api/core', () => factory());

// Stand in for useFoldedSubscription: keep each operation's reducer and accumulator, and
// fold frames through the reducer directly.
type Fold = { reduce: (prev: unknown, frames: unknown[]) => unknown; acc: unknown; args: Record<string, unknown> };
const { folds, useFoldedSubscriptionMock, useQueryMock, sendEnabled, state, listenMock } = vi.hoisted(() => ({
  listenMock: vi.fn(),
  folds: new Map<string, Fold>(),
  useFoldedSubscriptionMock: vi.fn(),
  useQueryMock: vi.fn(),
  sendEnabled: vi.fn(),
  state: { clusters: null, signals: undefined } as { clusters: Cluster[] | null; signals: unknown },
}));
vi.mock('@/lib/graphql/use-folded-subscription', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/graphql/use-folded-subscription')>()),
  useFoldedSubscription: useFoldedSubscriptionMock,
}));
vi.mock('urql', async (importOriginal) => ({
  ...(await importOriginal<typeof import('urql')>()),
  useQuery: useQueryMock,
  useMutation: () => [{}, sendEnabled],
}));
vi.mock('@tauri-apps/api/webviewWindow', () => ({ getCurrentWebviewWindow: () => ({ listen: listenMock }) }));
vi.mock('@/lib/clusters', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/clusters')>()),
  useClusters: () => ({ clusters: state.clusters, connected: true }),
}));

const {
  IncidentsProvider,
  incidentState,
  targetStatus,
  useIncidentOccurrenceEventFeed,
  useIncidents,
  useMonitorSignals,
  useIncidentTriage,
  useMonitorTargetEnabledSet,
  useMonitorTargets,
  useNotificationPermission,
} = await import('./incidents');
const { DialogProvider, useDialog } = await import('@/lib/dialog');

const operationName = (args: { query: { definitions: { name?: { value: string } }[] } }) =>
  args.query.definitions[0].name!.value;

beforeEach(() => {
  folds.clear();
  vi.clearAllMocks();
  state.clusters = [clusterRecord({ context: 'prod' })];
  state.signals = {
    monitorSignalList: [
      { name: 'kubernetes.crash_loop', integration: 'Kubernetes', label: 'Crash loop', severity: 'Critical' },
    ],
  };
  useFoldedSubscriptionMock.mockImplementation((args, reduce) => {
    const name = operationName(args);
    const fold = folds.get(name) ?? { reduce, acc: undefined, args };
    fold.reduce = reduce;
    fold.args = args;
    folds.set(name, fold);
    return { data: fold.acc, connected: true, generation: 1 };
  });
  useQueryMock.mockImplementation(() => [{ data: state.signals }]);
  invokeMock.mockResolvedValue(undefined);
  listenMock.mockResolvedValue(() => {});
});

// Folds frames into an operation's accumulator, under the field that operation reads.
function push(operation: string, field: string, ...frames: unknown[]) {
  const fold = folds.get(operation)!;
  act(() => {
    fold.acc = fold.reduce(
      fold.acc,
      frames.map((frame) => ({ [field]: frame })),
    );
  });
}

const pushTargets = (...frames: unknown[]) =>
  push('MonitorTargetCollectionWatch', 'monitorTargetCollectionWatch', ...frames);
const pushIncidents = (...frames: unknown[]) => push('IncidentCollectionWatch', 'incidentCollectionWatch', ...frames);
const pushOccurrences = (...frames: unknown[]) =>
  push('IncidentOccurrenceEventFeed', 'incidentOccurrenceEventFeed', ...frames);

// Each frame as its watch sends it, over the record `@/test-utils` builds.
const targetAdded = (over: Partial<MonitorTarget>) => ({ type: 'Added', target: monitorTargetRecord(over) });
const incidentAdded = (over: Partial<Incident>) => ({ type: 'Added', incident: incidentRecord(over) });
function occurrenceFrame(over: Partial<IncidentOccurrence>) {
  const occurrence = occurrenceRecord(over);
  return { type: 'Occurrence', incidentID: occurrence.incidentID, occurrence };
}
const occurrenceBookmark = (incidentID: string) => ({ type: 'Bookmark', incidentID, occurrence: null });

const wrapper = ({ children }: { children: ReactNode }) => <IncidentsProvider>{children}</IncidentsProvider>;

function renderProviderHooks(targetID?: string) {
  return renderHook(
    () => ({ targets: useMonitorTargets(), incidents: useIncidents(targetID), signals: useMonitorSignals() }),
    { wrapper },
  );
}

function bookmarkBoth() {
  pushTargets({ type: 'Bookmark', target: null });
  pushIncidents({ type: 'Bookmark', incident: null });
}

describe('IncidentsProvider', () => {
  it('holds nothing until both watches and the clusters list have their snapshots', () => {
    state.clusters = null;
    const { result, rerender } = renderProviderHooks();
    pushTargets(targetAdded({ id: 't1', clusterID: 'id-prod' }));
    pushTargets({ type: 'Bookmark', target: null });
    rerender();
    expect(result.current.targets).toBeNull();
    expect(result.current.incidents).toBeNull();

    pushIncidents({ type: 'Bookmark', incident: null });
    rerender();
    expect(result.current.targets).toBeNull();

    state.clusters = [clusterRecord({ context: 'prod' })];
    rerender();
    expect(result.current.targets?.map((t) => t.id)).toEqual(['t1']);
    expect(result.current.incidents).toEqual([]);
  });

  it("leaves out a cluster's target once its cluster leaves the list, and lists a target with no cluster always", () => {
    const { result, rerender } = renderProviderHooks();
    pushTargets(
      targetAdded({ id: 't1', clusterID: 'id-prod', label: 'prod' }),
      targetAdded({ id: 't2', clusterID: null, label: 'account' }),
    );
    pushIncidents(incidentAdded({ id: 'i1', targetID: 't1' }), incidentAdded({ id: 'i2', targetID: 't2' }));
    bookmarkBoth();
    rerender();
    expect(result.current.targets?.map((t) => t.id)).toEqual(['t2', 't1']);

    state.clusters = [];
    rerender();
    expect(result.current.targets?.map((t) => t.id)).toEqual(['t2']);
    expect(result.current.incidents?.map((i) => i.id)).toEqual(['i2']);
  });

  it('drops a frame whose item is null, and folds each change by id', () => {
    const { result, rerender } = renderProviderHooks();
    pushTargets(targetAdded({ id: 't1', clusterID: 'id-prod', label: 'prod' }));
    bookmarkBoth();
    pushTargets(
      { type: 'Added', target: null },
      { type: 'Modified', target: monitorTargetRecord({ id: 't1', clusterID: 'id-prod', label: 'renamed' }) },
    );
    pushIncidents({ type: 'Added', incident: null });
    rerender();
    expect(result.current.targets?.map((t) => t.label)).toEqual(['renamed']);
    expect(result.current.incidents).toEqual([]);
  });

  it("returns one target's incidents, open first by severity then newest start, then ended by newest last sighting", () => {
    const { result, rerender } = renderProviderHooks('t1');
    pushTargets(targetAdded({ id: 't1', clusterID: 'id-prod' }), targetAdded({ id: 't2', clusterID: null }));
    pushIncidents(
      incidentAdded({
        id: 'ended-old',
        targetID: 't1',
        open: false,
        lastSeenAt: '2026-11-01T00:00:00Z',
        lastEndReason: 'Resolved',
      }),
      incidentAdded({
        id: 'info',
        targetID: 't1',
        severity: 'Info',
        lastStartedAt: '2026-11-05T00:00:00Z',
      }),
      incidentAdded({
        id: 'warning-old',
        targetID: 't1',
        severity: 'Warning',
        lastStartedAt: '2026-11-02T00:00:00Z',
      }),
      incidentAdded({
        id: 'ended-new',
        targetID: 't1',
        open: false,
        lastSeenAt: '2026-11-03T00:00:00Z',
        lastEndReason: 'Stale',
      }),
      incidentAdded({
        id: 'warning-new',
        targetID: 't1',
        severity: 'Warning',
        lastStartedAt: '2026-11-04T00:00:00Z',
      }),
      incidentAdded({
        id: 'critical',
        targetID: 't1',
        severity: 'Critical',
        lastStartedAt: '2026-11-01T00:00:00Z',
      }),
      incidentAdded({ id: 'elsewhere', targetID: 't2' }),
    );
    bookmarkBoth();
    rerender();
    expect(result.current.incidents?.map((i) => i.id)).toEqual([
      'critical',
      'warning-new',
      'warning-old',
      'info',
      'ended-new',
      'ended-old',
    ]);
  });

  it('keeps the signals by name', () => {
    const { result } = renderProviderHooks();
    expect(result.current.signals.get('kubernetes.crash_loop')?.label).toBe('Crash loop');
  });

  it('keeps no signals until the list answers', () => {
    state.signals = undefined;
    const { result } = renderProviderHooks();
    expect(result.current.signals.size).toBe(0);
  });

  it('refuses to work outside its provider', () => {
    expect(() => renderHook(() => useMonitorTargets())).toThrow(/inside <IncidentsProvider>/);
    expect(() => renderHook(() => useMonitorTargetEnabledSet())).toThrow(/inside <IncidentsProvider>/);
  });
});

describe('the list order', () => {
  it('puts an ended incident with no last sighting after those with one', () => {
    const { result, rerender } = renderProviderHooks();
    pushTargets({ type: 'Added', target: monitorTargetRecord({ id: 't1', clusterID: 'id-prod' }) });
    pushIncidents(
      {
        type: 'Added',
        incident: incidentRecord({ id: 'unseen', open: false, lastSeenAt: null, lastEndReason: 'Stopped' }),
      },
      {
        type: 'Added',
        incident: incidentRecord({
          id: 'seen',
          open: false,
          lastSeenAt: '2026-11-01T00:00:00Z',
          lastEndReason: 'Stopped',
        }),
      },
    );
    bookmarkBoth();
    rerender();
    expect(result.current.incidents?.map((i) => i.id)).toEqual(['seen', 'unseen']);
  });
});

describe('useIncidentOccurrenceEventFeed', () => {
  it('opens no feed without an incident, and an unshared one for an incident', () => {
    const { rerender } = renderHook(({ id }) => useIncidentOccurrenceEventFeed(id), {
      initialProps: { id: undefined as string | undefined },
    });
    const call = () => useFoldedSubscriptionMock.mock.lastCall!;
    expect(call()[0]).toMatchObject({ pause: true });

    rerender({ id: 'i1' });
    expect(call()[0]).toMatchObject({ pause: false, variables: { incidentID: 'i1' } });
    expect(call()[2]).toEqual({ unshared: true });
  });

  it('holds back its occurrences until its own Bookmark, ordered by number whatever their starts', () => {
    const { result, rerender } = renderHook(() => useIncidentOccurrenceEventFeed('i1'));
    pushOccurrences(
      occurrenceFrame({ id: 'o1', number: 1, startedAt: '2026-11-05T00:00:00Z' }),
      occurrenceFrame({ id: 'o2', number: 2, startedAt: '2026-11-01T00:00:00Z' }),
    );
    rerender();
    expect(result.current.snapshotComplete).toBe(false);

    pushOccurrences(occurrenceBookmark('i1'));
    rerender();
    expect(result.current.snapshotComplete).toBe(true);
    expect(result.current.occurrences.map((o) => o.number)).toEqual([2, 1]);

    pushOccurrences(occurrenceFrame({ id: 'o2', number: 2, endedAt: '2026-11-02T00:00:00Z', endReason: 'Resolved' }));
    rerender();
    expect(result.current.occurrences.map((o) => o.endReason)).toEqual(['Resolved', null]);
  });

  it("drops another incident's frames, its Bookmark included, and shows none of the last incident's", () => {
    const { result, rerender } = renderHook(({ id }) => useIncidentOccurrenceEventFeed(id), {
      initialProps: { id: 'i1' },
    });
    pushOccurrences(occurrenceFrame({ id: 'o1', incidentID: 'i1' }), occurrenceBookmark('i1'));
    rerender({ id: 'i2' });
    expect(result.current.occurrences).toEqual([]);
    expect(result.current.snapshotComplete).toBe(false);

    pushOccurrences(occurrenceFrame({ id: 'o9', incidentID: 'i1' }), occurrenceBookmark('i1'));
    rerender({ id: 'i2' });
    expect(result.current.occurrences).toEqual([]);
    expect(result.current.snapshotComplete).toBe(false);

    pushOccurrences(occurrenceBookmark('i2'));
    rerender({ id: 'i2' });
    expect(result.current.snapshotComplete).toBe(true);
  });

  it('keeps the newest 100', () => {
    const { result, rerender } = renderHook(() => useIncidentOccurrenceEventFeed('i1'));
    pushOccurrences(
      ...Array.from({ length: 101 }, (_, i) => occurrenceFrame({ id: `o${i + 1}`, number: i + 1 })),
      occurrenceBookmark('i1'),
    );
    rerender();
    expect(result.current.occurrences).toHaveLength(100);
    expect(result.current.occurrences.at(-1)?.number).toBe(2);
  });
});

describe('targetStatus', () => {
  it('says off alone for a target that is off, whatever its last reading', () => {
    expect(targetStatus(monitorTargetRecord({ enabled: false, readState: 'Failed' }))).toEqual({ kind: 'off' });
  });

  it('names each reason a failed reading gave, once', () => {
    const target = monitorTargetRecord({
      readState: 'Failed',
      unknownSignals: [
        { name: 'a', reason: 'PermissionDenied' },
        { name: 'b', reason: 'PermissionDenied' },
        { name: 'c', reason: 'Unavailable' },
      ],
    });
    expect(targetStatus(target)).toEqual({ kind: 'failed', reasons: ['PermissionDenied', 'Unavailable'] });
  });

  it('says a target was never read', () => {
    expect(targetStatus(monitorTargetRecord({ readState: 'NeverRead' }))).toEqual({ kind: 'never-read' });
  });

  it('names the signals a partial reading could not judge', () => {
    const unknownSignals = [{ name: 'a', reason: 'NotReady' as const }];
    expect(targetStatus(monitorTargetRecord({ readState: 'Partial', unknownSignals }))).toEqual({
      kind: 'partial',
      unknownSignals,
    });
  });

  it('says nothing of a target that is on and judged everything', () => {
    expect(targetStatus(monitorTargetRecord({ readState: 'Ok' }))).toBeNull();
  });
});

describe('incidentState', () => {
  it('reads an open incident from its newest start', () => {
    expect(incidentState(incidentRecord({ lastStartedAt: '2026-11-01T00:00:00Z' }))).toEqual({
      label: 'Seen since',
      at: '2026-11-01T00:00:00Z',
    });
  });

  it('reads Resolved only for an incident that resolved, and Last seen for one stopped or stale', () => {
    const ended = { open: false, lastSeenAt: '2026-11-02T00:00:00Z' };
    expect(incidentState(incidentRecord({ ...ended, lastEndReason: 'Resolved' })).label).toBe('Resolved, last seen');
    expect(incidentState(incidentRecord({ ...ended, lastEndReason: 'Stale' }))).toEqual({
      label: 'Last seen',
      at: '2026-11-02T00:00:00Z',
    });
    expect(incidentState(incidentRecord({ ...ended, lastEndReason: 'Stopped' })).label).toBe('Last seen');
  });
});

describe('useIncidentTriage', () => {
  const pick = { model: { providerID: 'anthropic', id: 'claude' }, effort: 'high' };

  it('asks for the triage and holds the incident in flight until the mutation answers', async () => {
    let answer!: (result: unknown) => void;
    // The one mock answers every mutation of this module.
    sendEnabled.mockReturnValueOnce(
      new Promise((resolve) => {
        answer = resolve;
      }),
    );
    const { result } = renderHook(() => useIncidentTriage(), { wrapper });

    act(() => void result.current.triage('i1', pick));
    act(() => void result.current.triage('i1', pick));
    expect(sendEnabled).toHaveBeenCalledTimes(1);
    expect(sendEnabled).toHaveBeenCalledWith({ id: 'i1', providerID: 'anthropic', modelID: 'claude', effort: 'high' });
    expect(result.current.sending).toEqual(new Set(['i1']));

    await act(async () => answer({}));
    expect(result.current.sending).toEqual(new Set());
    expect(result.current.errors).toEqual(new Map());
  });

  it('keeps a refusal until the incident’s next press, and sends no effort for a model with none', async () => {
    sendEnabled.mockResolvedValueOnce({ error: { graphQLErrors: [{ message: 'Validation error' }], message: 'x' } });
    const { result } = renderHook(() => useIncidentTriage(), { wrapper });

    await act(async () => result.current.triage('i1', pick));
    expect(result.current.errors).toEqual(new Map([['i1', 'Validation error']]));

    sendEnabled.mockResolvedValueOnce({});
    await act(async () => result.current.triage('i1', { ...pick, effort: '' }));
    expect(sendEnabled).toHaveBeenLastCalledWith({
      id: 'i1',
      providerID: 'anthropic',
      modelID: 'claude',
      effort: null,
    });
    expect(result.current.errors).toEqual(new Map());
  });

  it('keeps a refusal while no dialog is drawn', async () => {
    let answer!: (result: unknown) => void;
    sendEnabled.mockReturnValueOnce(
      new Promise((resolve) => {
        answer = resolve;
      }),
    );
    let request!: IncidentTriageRequest;
    function Triage() {
      request = useIncidentTriage();
      return null;
    }
    const { rerender } = render(
      <IncidentsProvider>
        <Triage />
      </IncidentsProvider>,
    );

    act(() => void request.triage('i1', pick));
    rerender(<IncidentsProvider>{null}</IncidentsProvider>);
    await act(async () => answer({ error: { graphQLErrors: [{ message: 'Validation error' }], message: 'x' } }));
    rerender(
      <IncidentsProvider>
        <Triage />
      </IncidentsProvider>,
    );
    expect(request.errors).toEqual(new Map([['i1', 'Validation error']]));
  });
});

describe('useMonitorTargetEnabledSet', () => {
  // A mutation answer the test releases.
  function staged() {
    let answer!: (result: unknown) => void;
    sendEnabled.mockReturnValueOnce(
      new Promise((resolve) => {
        answer = resolve;
      }),
    );
    return (result: unknown = {}) => act(async () => answer(result));
  }
  const refused = (message: string) => ({ error: { graphQLErrors: [{ message }], message } });

  it('sends the switch and holds the target in flight until the mutation answers', async () => {
    const answer = staged();
    const { result } = renderHook(() => useMonitorTargetEnabledSet(), { wrapper });

    act(() => void result.current.set('t1', false));
    expect(sendEnabled).toHaveBeenCalledWith({ id: 't1', enabled: false });
    expect(result.current.changing).toEqual(new Set(['t1']));

    await answer();
    expect(result.current.changing).toEqual(new Set());
  });

  it('sends nothing for a target already in flight', async () => {
    const answer = staged();
    const { result } = renderHook(() => useMonitorTargetEnabledSet(), { wrapper });

    act(() => {
      void result.current.set('t1', false);
      void result.current.set('t1', true);
    });
    expect(sendEnabled).toHaveBeenCalledTimes(1);

    await answer();
    sendEnabled.mockResolvedValueOnce({});
    await act(() => result.current.set('t1', true));
    expect(sendEnabled).toHaveBeenCalledTimes(2);
  });

  it("keeps a refusal on its own target until that target's next press", async () => {
    sendEnabled.mockResolvedValueOnce(refused('Record not found')).mockResolvedValueOnce(refused('Unreachable'));
    const { result } = renderHook(() => useMonitorTargetEnabledSet(), { wrapper });

    await act(() => result.current.set('t1', false));
    await act(() => result.current.set('t2', false));
    expect(result.current.errors).toEqual(
      new Map([
        ['t1', 'Record not found'],
        ['t2', 'Unreachable'],
      ]),
    );

    const answer = staged();
    act(() => void result.current.set('t1', false));
    expect(result.current.errors).toEqual(new Map([['t2', 'Unreachable']]));
    // An answer that took brings back no refusal.
    await answer();
    expect(result.current.errors).toEqual(new Map([['t2', 'Unreachable']]));
  });

  it('keeps a switch in flight and its refusal while no switch is drawn', async () => {
    const answer = staged();
    let enabledSet!: MonitorTargetEnabledSet;
    function Switches() {
      enabledSet = useMonitorTargetEnabledSet();
      return null;
    }
    const { rerender } = render(
      <IncidentsProvider>
        <Switches />
      </IncidentsProvider>,
    );

    act(() => void enabledSet.set('t1', false));
    rerender(<IncidentsProvider>{null}</IncidentsProvider>);
    rerender(
      <IncidentsProvider>
        <Switches />
      </IncidentsProvider>,
    );
    expect(enabledSet.changing).toEqual(new Set(['t1']));
    act(() => void enabledSet.set('t1', false));
    expect(sendEnabled).toHaveBeenCalledTimes(1);

    rerender(<IncidentsProvider>{null}</IncidentsProvider>);
    await answer(refused('Unreachable'));
    rerender(
      <IncidentsProvider>
        <Switches />
      </IncidentsProvider>,
    );
    expect(enabledSet.changing).toEqual(new Set());
    expect(enabledSet.errors).toEqual(new Map([['t1', 'Unreachable']]));
  });

  it('keeps the same set across renders', () => {
    const { result, rerender } = renderHook(() => useMonitorTargetEnabledSet(), { wrapper });
    const { set } = result.current;
    rerender();
    expect(result.current.set).toBe(set);
  });
});

describe('useNotificationPermission', () => {
  it('is undefined until the host’s first frame, then each frame it sends while mounted', () => {
    const { result, unmount } = renderHook(() => useNotificationPermission());
    expect(result.current).toBeUndefined();
    const channel = channels.at(-1)!;
    expect(invokeMock).toHaveBeenCalledWith('notification_permission_state_watch', { channel });

    act(() => channel.onmessage!('Denied'));
    expect(result.current).toBe('Denied');
    unmount();
    channel.onmessage!('Granted');
  });
});

describe('a notification press', () => {
  const inProviders = ({ children }: { children: ReactNode }) => (
    <DialogProvider>
      <IncidentsProvider>{children}</IncidentsProvider>
    </DialogProvider>
  );
  // The host's answers to notification_press_take, in turn; null once none waits.
  const pressesWaiting = (...presses: ({ incidentID: string | null } | null)[]) =>
    invokeMock.mockImplementation((cmd: string) =>
      Promise.resolve(cmd === 'notification_press_take' ? (presses.shift() ?? null) : undefined),
    );

  it('opens the dialog on a press that waited for the page, taken once it listens', async () => {
    pressesWaiting({ incidentID: 'i1' });
    const { result } = renderHook(() => useDialog(), { wrapper: inProviders });
    await waitFor(() => expect(result.current.activeDialog).toBe('incidents'));
    expect(result.current.incidentID).toBe('i1');
    expect(listenMock).toHaveBeenCalledWith('notification-press-waiting', expect.any(Function));
  });

  it('opens the dialog with nothing selected on a summary’s press, the one that waited for a window it built', async () => {
    pressesWaiting({ incidentID: null });
    const { result } = renderHook(() => useDialog(), { wrapper: inProviders });
    await waitFor(() => expect(result.current.activeDialog).toBe('incidents'));
    expect(result.current.incidentID).toBeNull();
  });

  it('opens nothing at mount with no press waiting, then takes each press it is told of, and stops listening on unmount', async () => {
    const unlisten = vi.fn();
    listenMock.mockResolvedValue(unlisten);
    pressesWaiting(null, { incidentID: 'i2' });
    const { result, unmount } = renderHook(() => useDialog(), { wrapper: inProviders });
    await waitFor(() => expect(invokeMock).toHaveBeenCalledWith('notification_press_take'));
    expect(result.current.activeDialog).toBeNull();

    const waiting = listenMock.mock.calls[0][1] as () => void;
    act(() => waiting());
    await waitFor(() => expect(result.current.incidentID).toBe('i2'));
    expect(result.current.activeDialog).toBe('incidents');

    unmount();
    expect(unlisten).toHaveBeenCalled();
  });

  it('drops a listen that lands after the provider unmounts, and takes nothing', async () => {
    const unlisten = vi.fn();
    let land!: (fn: () => void) => void;
    listenMock.mockReturnValue(
      new Promise((resolve) => {
        land = resolve;
      }),
    );
    const { unmount } = renderHook(() => useDialog(), { wrapper: inProviders });
    await waitFor(() => expect(listenMock).toHaveBeenCalled());
    unmount();
    land(unlisten);
    await waitFor(() => expect(unlisten).toHaveBeenCalled());
    expect(invokeMock).not.toHaveBeenCalledWith('notification_press_take');
  });
});
