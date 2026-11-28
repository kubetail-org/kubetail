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

import { act, renderHook } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { ClusterCachedDataEvent } from '@/lib/cluster-cached-data-events';
import type { ClusterCachedDataObject } from '@/lib/cluster-cached-data-objects';

const { objectsMock, eventsMock, namespacesMock, scope, activeCluster } = vi.hoisted(() => {
  // The active cluster's cache health; `null` is a cache with no rollup yet.
  const health: { reason: string | null } = { reason: 'Watching' };
  const namespace: { namespace?: string } = {};
  return {
    activeCluster: health,
    objectsMock: vi.fn(),
    eventsMock: vi.fn(),
    namespacesMock: vi.fn(),
    scope: namespace,
  };
});
vi.mock('@/lib/cluster-cached-data-objects', () => ({ useClusterCachedDataObjects: objectsMock }));
vi.mock('@/lib/cluster-cached-data-events', () => ({ useClusterCachedDataEvents: eventsMock }));
vi.mock('@/lib/cluster-namespaces', () => ({ useClusterNamespaces: namespacesMock }));
vi.mock('@/lib/namespace-scope', () => ({ useNamespaceScope: () => scope }));
vi.mock('@/lib/active-cluster', () => ({
  useActiveCluster: () => ({
    cluster: { activeCache: { health: activeCluster.reason === null ? null : { reason: activeCluster.reason } } },
  }),
}));

const {
  namespaceRows,
  nodeCounts,
  podOwnerLine,
  podSince,
  podSummary,
  recentWarnings,
  useOverview,
  workloadsByNamespace,
} = await import('./overview');

const NOW = Date.parse('2026-11-28T12:00:00Z');
const MINUTE = 60_000;

function object(name: string, rawJSON: unknown, extra: Partial<ClusterCachedDataObject> = {}): ClusterCachedDataObject {
  return { uid: name, namespace: 'shop', name, creationTimestamp: '', rawJSON, ...extra } as ClusterCachedDataObject;
}

const ready = (status: string, lastTransitionTime?: string) => ({ type: 'Ready', status, lastTransitionTime });

const running = (name: string, extra?: Partial<ClusterCachedDataObject>) =>
  object(name, { status: { phase: 'Running', conditions: [ready('True')] } }, extra);
const crashLooping = (name: string, since: string, extra?: Partial<ClusterCachedDataObject>) =>
  object(
    name,
    {
      status: {
        phase: 'Running',
        conditions: [ready('False', since)],
        containerStatuses: [{ name: 'web', state: { waiting: { reason: 'CrashLoopBackOff' } } }],
      },
    },
    extra,
  );
const notReady = (name: string, since: string) =>
  object(name, {
    status: {
      phase: 'Running',
      conditions: [ready('False', since)],
      containerStatuses: [{ name: 'web', ready: false, state: { running: {} } }],
    },
  });
const succeeded = (name: string) => object(name, { status: { phase: 'Succeeded' } });

function event(uid: string, lastSeenMs: number, extra: Partial<ClusterCachedDataEvent> = {}): ClusterCachedDataEvent {
  return {
    uid,
    type: 'Warning',
    reason: 'BackOff',
    message: '',
    count: 5,
    firstSeen: null,
    lastSeen: new Date(lastSeenMs).toISOString(),
    involvedKind: 'Pod',
    involvedNamespace: 'shop',
    involvedName: 'web',
    ...extra,
  };
}

describe('nodeCounts', () => {
  it('counts the nodes whose Ready condition is True', () => {
    const node = (name: string, status: string) => object(name, { status: { conditions: [ready(status)] } });
    expect(nodeCounts([node('a', 'True'), node('b', 'False'), node('c', 'Unknown'), object('d', null)])).toEqual({
      ready: 1,
      total: 4,
    });
  });
});

describe('podSummary', () => {
  const pods = [
    running('web-1'),
    crashLooping('web-2', '2026-11-28T10:00:00Z'),
    notReady('web-3', '2026-11-28T11:00:00Z'),
    succeeded('job-1'),
    object('blank', {}),
  ];

  it('counts a crash-looping pod failing and a running pod that is not ready pending', () => {
    expect(podSummary(pods, undefined).counts).toEqual({ running: 1, failing: 1, pending: 1, total: 3 });
  });

  it('leaves a Succeeded pod out, and one with no status yet', () => {
    const bodiless = object('bodiless', null);
    expect(podSummary([succeeded('job-1'), object('blank', {}), bodiless], undefined).counts.total).toBe(0);
  });

  it('lists the problem pods failing first, then the longest-standing', () => {
    const older = notReady('old', '2026-11-28T09:00:00Z');
    const undated = object('undated', { status: { phase: 'Pending' } });
    const undated2 = object('undated-2', { status: { phase: 'Pending' } });
    const { problems } = podSummary([undated, older, ...pods, undated2], undefined);
    expect(problems.map((p) => p.object.name)).toEqual(['web-2', 'old', 'web-3', 'undated', 'undated-2']);
    expect(problems[0].status).toEqual({ label: 'CrashLoopBackOff', tone: 'failing' });
  });

  it('keeps to the namespace scope', () => {
    const other = crashLooping('api', '2026-11-28T10:00:00Z', { namespace: 'ops' });
    const { counts, problems } = podSummary([...pods, other], 'ops');
    expect(counts).toEqual({ running: 0, failing: 1, pending: 0, total: 1 });
    expect(problems.map((p) => p.object.name)).toEqual(['api']);
  });
});

describe('podSince', () => {
  it('reads the Ready condition’s last transition, then the start, then the creation', () => {
    const at = '2026-11-28T10:00:00Z';
    expect(podSince(object('a', { status: { conditions: [ready('False', at)], startTime: 'x' } }))).toBe(
      Date.parse(at),
    );
    expect(podSince(object('b', { status: { startTime: at } }))).toBe(Date.parse(at));
    expect(podSince(object('c', null, { creationTimestamp: at }))).toBe(Date.parse(at));
    expect(podSince(object('d', null))).toBeNaN();
  });
});

describe('podOwnerLine', () => {
  const owned = (kind: string, name: string, labels?: Record<string, string>) =>
    object('p', { metadata: { labels, ownerReferences: [{ kind, name, controller: true }] } });

  it('names the owner by its kind', () => {
    expect(podOwnerLine(owned('ReplicaSet', 'worker-5d9f', { 'pod-template-hash': '5d9f' }))).toBe(
      'Pod · Deployment worker',
    );
    expect(podOwnerLine(owned('ReplicaSet', 'loose'))).toBe('Pod · ReplicaSet loose');
    expect(podOwnerLine(owned('StatefulSet', 'db'))).toBe('Pod · StatefulSet db');
    expect(podOwnerLine(owned('DaemonSet', 'agent'))).toBe('Pod · DaemonSet agent');
    expect(podOwnerLine(owned('Job', 'backup'))).toBe('Pod · Job backup');
    expect(podOwnerLine(owned('CronJob', 'nightly'))).toBe('Pod · CronJob nightly');
  });

  it('writes an owner of another kind as ownerOf does, and none as "Pod"', () => {
    expect(podOwnerLine(owned('Node', 'kind-control-plane'))).toBe('Pod · node/kind-control-plane');
    expect(podOwnerLine(object('p', {}))).toBe('Pod');
  });
});

describe('workloadsByNamespace', () => {
  it('counts each workload in its namespace, leaving out the Jobs a CronJob owns', () => {
    const deploy = object('web', {});
    const sts = object('db', {}, { namespace: 'ops' });
    const job = object('backup', {});
    const cronJob = object('nightly-1', { metadata: { ownerReferences: [{ kind: 'CronJob', name: 'nightly' }] } });
    const counts = workloadsByNamespace([[deploy], [sts]], [job, cronJob, object('blank', null)]);
    expect(Object.fromEntries(counts)).toEqual({ shop: 3, ops: 1 });
  });
});

describe('namespaceRows', () => {
  const pods = [running('a'), succeeded('b'), running('c', { namespace: 'ops' })];
  const workloads = new Map([['shop', 2]]);

  it('lists every namespace, the most pods first, then by name', () => {
    expect(namespaceRows(['ops', 'shop', 'b-empty', 'a-empty'], undefined, pods, workloads)).toEqual([
      { name: 'shop', workloads: 2, pods: 2 },
      { name: 'ops', workloads: 0, pods: 1 },
      { name: 'a-empty', workloads: 0, pods: 0 },
      { name: 'b-empty', workloads: 0, pods: 0 },
    ]);
  });

  it('keeps to the namespace scope', () => {
    expect(namespaceRows(['ops', 'shop'], 'ops', pods, workloads)).toEqual([{ name: 'ops', workloads: 0, pods: 1 }]);
    expect(namespaceRows(['ops'], 'gone', pods, workloads)).toEqual([]);
  });
});

describe('recentWarnings', () => {
  it('keeps the warnings in scope seen within the last hour', () => {
    const inside = event('inside', NOW - 59 * MINUTE);
    const outside = event('outside', NOW - 61 * MINUTE);
    const normal = event('normal', NOW, { type: 'Normal' });
    const elsewhere = event('elsewhere', NOW, { involvedNamespace: 'ops' });
    const unseen = event('unseen', NOW, { lastSeen: null });
    const all = [inside, outside, normal, elsewhere, unseen];
    expect(recentWarnings(all, undefined, NOW).map((e) => e.uid)).toEqual(['inside', 'elsewhere']);
    expect(recentWarnings(all, 'shop', NOW).map((e) => e.uid)).toEqual(['inside']);
  });
});

describe('useOverview', () => {
  const live = (objects: ClusterCachedDataObject[] = []) => ({ objects, active: true, phase: 'live' });
  let watches: Record<string, ReturnType<typeof live>>;

  beforeEach(() => {
    vi.useFakeTimers({ now: NOW });
    scope.namespace = undefined;
    activeCluster.reason = 'Watching';
    watches = {};
    objectsMock.mockImplementation((kind: { resource: string }) => watches[kind.resource] ?? live());
    eventsMock.mockReturnValue({ events: [], active: true, phase: 'live' });
    namespacesMock.mockReturnValue({ namespaces: ['shop'], active: true, phase: 'live' });
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('subscribes every watch unshared', () => {
    renderHook(() => useOverview());
    expect(objectsMock.mock.calls.map(([kind, opts]) => [kind.resource, opts])).toEqual(
      ['nodes', 'pods', 'deployments', 'statefulsets', 'daemonsets', 'cronjobs', 'jobs'].map((r) => [
        r,
        { unshared: true },
      ]),
    );
    expect(eventsMock).toHaveBeenCalledWith({ unshared: true });
  });

  it('gives each section once its own watches have their snapshots', () => {
    watches.pods = { ...live(), phase: 'connecting' };
    const { result } = renderHook(() => useOverview());
    expect(result.current.active).toBe(true);
    expect(result.current.nodes).toEqual({ ready: 0, total: 0 });
    expect(result.current.pods).toBeUndefined();
    // The namespaces' pod counts wait for the pods too.
    expect(result.current.namespaces).toBeUndefined();
    expect(result.current.warnings).toEqual([]);
  });

  it('gives nothing without an active cache', () => {
    objectsMock.mockReturnValue({ objects: [], active: false, phase: 'connecting' });
    eventsMock.mockReturnValue({ events: [], active: false, phase: 'connecting' });
    const { result } = renderHook(() => useOverview());
    expect(result.current).toEqual({ active: false, syncing: false });
  });

  it.each([
    [null, true],
    ['Syncing', true],
    ['Unknown', true],
    ['Watching', false],
    ['Stale', false],
  ])('reads a cache whose health is %s as syncing: %s', (reason, syncing) => {
    activeCluster.reason = reason;
    const { result } = renderHook(() => useOverview());
    expect(result.current.syncing).toBe(syncing);
  });

  it('counts workloads and pods per namespace under the scope', () => {
    scope.namespace = 'shop';
    watches.pods = live([running('a'), running('b', { namespace: 'ops' })]);
    watches.deployments = live([object('web', {})]);
    namespacesMock.mockReturnValue({ namespaces: ['ops', 'shop'], active: true, phase: 'live' });
    const { result } = renderHook(() => useOverview());
    expect(result.current.namespaces).toEqual([{ name: 'shop', workloads: 1, pods: 1 }]);
    expect(result.current.pods?.counts.total).toBe(1);
  });

  it('lets a warning fall out of the hour on the next tick', async () => {
    eventsMock.mockReturnValue({ events: [event('w', NOW - 58.5 * MINUTE)], active: true, phase: 'live' });
    const { result } = renderHook(() => useOverview({ tickMs: MINUTE }));
    expect(result.current.warnings).toHaveLength(1);
    await act(() => vi.advanceTimersByTimeAsync(MINUTE));
    expect(result.current.warnings).toHaveLength(1);
    await act(() => vi.advanceTimersByTimeAsync(MINUTE));
    expect(result.current.warnings).toEqual([]);
  });
});
