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

import { renderHook } from '@testing-library/react';
import type { ReactNode } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import type { ServerKind } from './dashboard-resources';

// The hook composes `useFoldedSubscription` with the active-context → cluster/cache
// join and a cache-aware guard. Mock those seams so the test drives the collection watch +
// cache state directly, without a real GraphQL client. The mock stands in for the
// accumulator: it captures the reducer and returns the accumulated data, so `pushFrame`
// folds a delta through the real reducer just as the live hook would.
const { useFoldedSubscriptionMock } = vi.hoisted(() => ({ useFoldedSubscriptionMock: vi.fn() }));
const { useClustersMock, useActiveKubeContextMock } = vi.hoisted(() => ({
  useClustersMock: vi.fn(),
  useActiveKubeContextMock: vi.fn(),
}));
// Transport state as the stand-in reports it; `pushReset` models a reconnect (the real
// reset semantics are covered in use-folded-subscription.test.tsx).
const { statusState } = vi.hoisted(() => ({
  statusState: { snapshot: { connected: true, generation: 0 } },
}));

vi.mock('@/lib/graphql/use-folded-subscription', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/graphql/use-folded-subscription')>()),
  useFoldedSubscription: useFoldedSubscriptionMock,
}));
// The provider hook alone is mocked; the lookups stay real, so the collection
// accumulation under test runs unaltered.
vi.mock('@/lib/clusters', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/clusters')>()),
  useClusters: useClustersMock,
}));
vi.mock('@/lib/active-kube-context', () => ({ useActiveKubeContext: useActiveKubeContextMock }));
// Every namespace: the catalog counts the whole cache, and each frame says so with ''.
vi.mock('@/lib/namespace-scope', () => ({
  useNamespaceScope: () => ({ namespace: undefined, setNamespace: () => {} }),
}));

const { useDashboardNav, useDashboardNavPlace } = await import('./dashboard-nav');
const { buildDashboardNav } = await import('./dashboard-resources');
const { ClusterCachedDataKindsProvider } = await import('./cluster-cached-data-kinds');

// The catalog's subscription lives in its provider, as the layout mounts it.
function renderNav() {
  return renderHook(() => useDashboardNav(), {
    wrapper: ({ children }: { children: ReactNode }) => (
      <ClusterCachedDataKindsProvider enabled>{children}</ClusterCachedDataKindsProvider>
    ),
  });
}

// Two kinds not curated, so each lands under a `<group>/<resource>` id.
const HPA = {
  apiVersion: 'autoscaling/v2',
  kind: 'HorizontalPodAutoscaler',
  resource: 'horizontalpodautoscalers',
  scope: 'Namespaced',
  isCRD: false,
  count: 7,
};

const CONTROLLER_REVISION = {
  apiVersion: 'apps/v1',
  kind: 'ControllerRevision',
  resource: 'controllerrevisions',
  scope: 'Namespaced',
  isCRD: false,
  count: 2,
};

// A cluster fixture for context "prod" whose active cache has the given id/spec.serverUid
// and Synced condition — or no active cache when `synced` is null.
function clusterFixture(synced: { status: string; reason: string } | null, cacheId = 'c1', serverUid = 'uid-1') {
  return {
    id: '1',
    spec: { source: { kubeconfig: { context: 'prod' } } },
    activeCache: synced
      ? { id: cacheId, spec: { serverUid }, status: { conditions: [{ type: 'Synced', ...synced }] } }
      : null,
  };
}

type Nav = ReturnType<typeof buildDashboardNav>;
const CURATED_ONLY = buildDashboardNav(undefined);
// The discovered kinds in the tree: every kind a `<group>/<resource>` id names.
const workloadsExtra = (nav: Nav) =>
  nav.sections.flatMap((s) => s.groups.flatMap((g) => g.kinds)).filter((k) => k.id.includes('/'));
const hasDiscovered = (nav: Nav) => workloadsExtra(nav).length > 0;

// urql accumulator stand-in. `acc` is the reduced data the mock returns; `pushFrame`
// folds a delta through the reducer captured on the last render, exactly as urql's live
// handler would. Each frame carries its own cache id (its provenance) — defaulting to
// the currently-subscribed cache, but overridable to model a late frame from a
// superseded subscription.
let acc: unknown;
let lastArgs: { variables?: { cacheID?: string }; pause?: boolean } | undefined;
let lastReducer: ((prev: unknown, frames: unknown[]) => unknown) | undefined;

function pushFrame(type: string, kind: unknown, cacheID = lastArgs?.variables?.cacheID) {
  acc = lastReducer!(acc, [{ clusterCachedDataKindCollectionWatch: { type, cacheID, namespace: '', kind } }]);
}

// The Bookmark closing the snapshot: what flips the watch from connecting to live.
function pushBookmark(cacheID = lastArgs?.variables?.cacheID) {
  acc = lastReducer!(acc, [
    { clusterCachedDataKindCollectionWatch: { type: 'Bookmark', cacheID, namespace: '', kind: null } },
  ]);
}

// A transport reconnect: the exchange bumps the op's generation on the new
// connection's `open`, which is what makes useFoldedSubscription reset its
// accumulator (fold onto a clean slate, and mask any not-yet-refolded state).
function pushReset() {
  acc = undefined;
  statusState.snapshot = { connected: true, generation: statusState.snapshot.generation + 1 };
}

beforeEach(() => {
  vi.clearAllMocks();
  acc = undefined;
  lastArgs = undefined;
  lastReducer = undefined;
  statusState.snapshot = { connected: true, generation: 0 };
  useActiveKubeContextMock.mockReturnValue({ context: 'prod' });
  useFoldedSubscriptionMock.mockImplementation((args: typeof lastArgs, reducer: typeof lastReducer) => {
    lastArgs = args;
    lastReducer = reducer;
    return { data: acc, connected: statusState.snapshot.connected };
  });
});

describe('useDashboardNav', () => {
  it('builds discovered kinds with live counts from the snapshot delta burst', () => {
    useClustersMock.mockReturnValue({ clusters: [clusterFixture({ status: 'True', reason: 'Watching' })] });
    const { result, rerender } = renderNav();

    // Before any frame: curated-only.
    expect(result.current.nav).toEqual(CURATED_ONLY);

    // The snapshot's kinds wait for its Bookmark: until then the tree stays curated, with
    // no counts, so no kind reads as empty or missing.
    pushFrame('Added', HPA);
    rerender();
    expect(result.current.nav).toEqual(CURATED_ONLY);

    pushBookmark();
    rerender();
    const extra = workloadsExtra(result.current.nav);
    expect(extra.map((c) => c.id)).toEqual(['autoscaling/horizontalpodautoscalers']);
    expect(extra[0].count).toBe(7);
  });

  it('updates a kind’s count live on a Modified frame', () => {
    useClustersMock.mockReturnValue({ clusters: [clusterFixture({ status: 'True', reason: 'Watching' })] });
    const { result, rerender } = renderNav();

    pushFrame('Added', HPA);
    pushBookmark();
    rerender();
    expect(workloadsExtra(result.current.nav)[0].count).toBe(7);

    // A later object write bumps the count → Modified re-emits the same kind.
    pushFrame('Modified', { ...HPA, count: 12 });
    rerender();
    expect(workloadsExtra(result.current.nav)[0].count).toBe(12);
  });

  it('reveals a newly-discovered kind on an Added frame and removes one on Deleted', () => {
    useClustersMock.mockReturnValue({ clusters: [clusterFixture({ status: 'True', reason: 'Watching' })] });
    const { result, rerender } = renderNav();

    pushFrame('Added', HPA);
    pushFrame('Added', CONTROLLER_REVISION);
    pushBookmark();
    rerender();
    expect(
      workloadsExtra(result.current.nav)
        .map((c) => c.id)
        .toSorted(),
    ).toEqual(['apps/controllerrevisions', 'autoscaling/horizontalpodautoscalers']);

    // A kind leaving the catalog is dropped.
    pushFrame('Deleted', HPA);
    rerender();
    expect(workloadsExtra(result.current.nav).map((c) => c.id)).toEqual(['apps/controllerrevisions']);
  });

  it('moves the subscription key on a cache swap and drops the old cache’s kinds until the new cache streams', () => {
    useClustersMock.mockReturnValue({
      clusters: [clusterFixture({ status: 'True', reason: 'Watching' }, 'c1', 'uid-1')],
    });
    const { result, rerender } = renderNav();
    expect(lastArgs?.variables?.cacheID).toBe('c1');

    pushFrame('Added', HPA);
    pushBookmark();
    rerender();
    expect(hasDiscovered(result.current.nav)).toBe(true);

    // A cache swap (repoint / UID switch) moves the subscription key. urql retains the
    // prior cache's accumulated data until the new cache's first frame, so the
    // cache-aware guard must reject it → curated-only in the meantime.
    useClustersMock.mockReturnValue({
      clusters: [clusterFixture({ status: 'True', reason: 'Watching' }, 'c2', 'uid-2')],
    });
    rerender();
    expect(lastArgs?.variables?.cacheID).toBe('c2');
    expect(hasDiscovered(result.current.nav)).toBe(false);

    // Once the new cache streams, its kinds appear (and the old cache's are gone).
    pushFrame('Added', CONTROLLER_REVISION);
    pushBookmark();
    rerender();
    expect(workloadsExtra(result.current.nav).map((c) => c.id)).toEqual(['apps/controllerrevisions']);
  });

  it('rejects a late frame from a superseded cache instead of mis-tagging it as the active one', () => {
    useClustersMock.mockReturnValue({
      clusters: [clusterFixture({ status: 'True', reason: 'Watching' }, 'c1', 'uid-1')],
    });
    const { result, rerender } = renderNav();
    expect(lastArgs?.variables?.cacheID).toBe('c1');

    pushFrame('Added', HPA);
    pushBookmark();
    rerender();
    expect(hasDiscovered(result.current.nav)).toBe(true);

    // Swap to c2. urql keeps the c1 subscription alive until effect cleanup, so a frame
    // from c1 can still arrive while the render already targets c2. It carries its own
    // (c1) provenance, so it must NOT be attributed to c2 — even though c2 has streamed
    // nothing yet, the nav stays curated-only rather than showing c1's kind.
    useClustersMock.mockReturnValue({
      clusters: [clusterFixture({ status: 'True', reason: 'Watching' }, 'c2', 'uid-2')],
    });
    rerender();
    expect(lastArgs?.variables?.cacheID).toBe('c2');

    pushFrame('Added', CONTROLLER_REVISION, 'c1'); // a straggler from the old subscription
    rerender();
    expect(hasDiscovered(result.current.nav)).toBe(false);
  });

  it('preserves the active cache’s catalog when a late old-cache straggler arrives after the new cache has streamed', () => {
    useClustersMock.mockReturnValue({
      clusters: [clusterFixture({ status: 'True', reason: 'Watching' }, 'c1', 'uid-1')],
    });
    const { result, rerender } = renderNav();
    pushFrame('Added', HPA); // c1's snapshot
    pushBookmark();
    rerender();

    // Swap to c2 and let its snapshot stream two kinds.
    useClustersMock.mockReturnValue({
      clusters: [clusterFixture({ status: 'True', reason: 'Watching' }, 'c2', 'uid-2')],
    });
    rerender();
    pushFrame('Added', HPA, 'c2');
    pushFrame('Added', CONTROLLER_REVISION, 'c2');
    pushBookmark();
    rerender();
    expect(
      workloadsExtra(result.current.nav)
        .map((c) => c.id)
        .toSorted(),
    ).toEqual(['apps/controllerrevisions', 'autoscaling/horizontalpodautoscalers']);

    // A late straggler from the superseded c1 subscription must NOT wipe c2's catalog:
    // it's dropped, and c2's fully-accumulated kinds stay put.
    pushFrame('Added', { ...HPA, resource: 'stragglers' }, 'c1');
    rerender();
    expect(
      workloadsExtra(result.current.nav)
        .map((c) => c.id)
        .toSorted(),
    ).toEqual(['apps/controllerrevisions', 'autoscaling/horizontalpodautoscalers']);

    // A subsequent legitimate c2 delta still patches the intact catalog (it isn't reset
    // to a singleton by the straggler), so the count updates live.
    pushFrame('Modified', { ...HPA, count: 99 }, 'c2');
    rerender();
    expect(workloadsExtra(result.current.nav).find((c) => c.id === 'autoscaling/horizontalpodautoscalers')?.count).toBe(
      99,
    );
  });

  it('resets the catalog on a transport reset so kinds deleted during an outage are gone after the replay', () => {
    useClustersMock.mockReturnValue({ clusters: [clusterFixture({ status: 'True', reason: 'Watching' })] });
    const { result, rerender } = renderNav();

    pushFrame('Added', HPA);
    pushFrame('Added', CONTROLLER_REVISION);
    pushBookmark();
    rerender();
    expect(
      workloadsExtra(result.current.nav)
        .map((c) => c.id)
        .toSorted(),
    ).toEqual(['apps/controllerrevisions', 'autoscaling/horizontalpodautoscalers']);

    // The transport reconnects: replicasets was deleted during the outage, so
    // the replayed snapshot carries only controllerrevisions — no Deleted
    // frame ever arrives for the kind that vanished.
    pushReset();
    pushFrame('Added', CONTROLLER_REVISION);
    pushBookmark();
    rerender();
    expect(workloadsExtra(result.current.nav).map((c) => c.id)).toEqual(['apps/controllerrevisions']);
  });

  it('falls back to curated-only after a transport reset when the replayed snapshot is empty', () => {
    useClustersMock.mockReturnValue({ clusters: [clusterFixture({ status: 'True', reason: 'Watching' })] });
    const { result, rerender } = renderNav();

    pushFrame('Added', HPA);
    pushBookmark();
    rerender();
    expect(hasDiscovered(result.current.nav)).toBe(true);

    // Everything was deleted during the outage: nothing is replayed, so the
    // reset alone must clear the accumulated catalog.
    pushReset();
    rerender();
    expect(hasDiscovered(result.current.nav)).toBe(false);
  });

  it('pauses the subscription and falls back to curated-only when there is no active cluster or cache', () => {
    // No cluster matches "prod" (departed/disabled); also covers a cluster with no
    // active cache (cacheID undefined ⇒ subscription paused).
    useClustersMock.mockReturnValue({ clusters: [] });
    const { result } = renderNav();
    expect(hasDiscovered(result.current.nav)).toBe(false);
    expect(lastArgs?.pause).toBe(true);
  });

  // The transport phase (for the sidebar's reconnecting/loading hint) — and, crucially,
  // `active`, which gates it so a paused watch never reads as "reconnecting".
  it('reports active=false while paused, so the paused false `connected` is not "reconnecting"', () => {
    // A cluster with no active cache ⇒ subscription paused ⇒ `connected` is false, but
    // that must not surface as a connection problem.
    useClustersMock.mockReturnValue({ clusters: [clusterFixture(null)] });
    statusState.snapshot = { connected: false, generation: 0 };
    const { result } = renderNav();
    expect(result.current.active).toBe(false);
    expect(lastArgs?.pause).toBe(true);
  });

  it('reports active + live once the catalog has streamed on a healthy connection', () => {
    useClustersMock.mockReturnValue({ clusters: [clusterFixture({ status: 'True', reason: 'Watching' })] });
    const { result, rerender } = renderNav();
    expect(result.current.active).toBe(true);

    pushFrame('Added', HPA);
    pushBookmark();
    rerender();
    expect(result.current.phase).toBe('live');
  });

  it('reports reconnecting when a live watch drops but keeps its last-known catalog', () => {
    useClustersMock.mockReturnValue({ clusters: [clusterFixture({ status: 'True', reason: 'Watching' })] });
    const { result, rerender } = renderNav();
    pushFrame('Added', HPA);
    pushBookmark();
    rerender();
    expect(result.current.phase).toBe('live');

    // Transport drops (same generation → data held). Phase flips, kinds stay.
    statusState.snapshot = { connected: false, generation: statusState.snapshot.generation };
    rerender();
    expect(result.current.phase).toBe('reconnecting');
    expect(hasDiscovered(result.current.nav)).toBe(true);
  });

  it('reports connecting when the active watch has dialed but no frame has landed', () => {
    useClustersMock.mockReturnValue({ clusters: [clusterFixture({ status: 'True', reason: 'Watching' })] });
    statusState.snapshot = { connected: false, generation: 0 };
    const { result } = renderNav();
    expect(result.current.active).toBe(true);
    expect(result.current.phase).toBe('connecting');
  });
});

describe('useDashboardNavPlace', () => {
  it('places a curated kind before the Bookmark, and after it only a kind it is handed', () => {
    useClustersMock.mockReturnValue({ clusters: [clusterFixture({ status: 'True', reason: 'Watching' })] });
    const wrapper = ({ children }: { children: ReactNode }) => (
      <ClusterCachedDataKindsProvider enabled>{children}</ClusterCachedDataKindsProvider>
    );
    const hpa = { ...HPA, printerColumns: [] };
    type Props = { id: string; kind?: ServerKind };
    const initialProps: Props = { id: 'autoscaling/horizontalpodautoscalers' };
    const { result, rerender } = renderHook(({ id, kind }: Props) => useDashboardNavPlace(id, kind), {
      wrapper,
      initialProps,
    });

    pushFrame('Added', HPA);
    rerender({ id: 'autoscaling/horizontalpodautoscalers' });
    expect(result.current).toBeUndefined();
    rerender({ id: 'pods' });
    expect(result.current).toEqual({ section: 'namespace', sectionLabel: 'Namespace', group: 'Workloads' });

    pushBookmark();
    rerender({ id: 'autoscaling/horizontalpodautoscalers', kind: hpa });
    expect(result.current).toEqual({ section: 'namespace', sectionLabel: 'Namespace', group: 'Workloads' });
    // The caller searched the landed catalog and missed: even a curated id has no place.
    rerender({ id: 'pods' });
    expect(result.current).toBeUndefined();
  });

  it('places a kind it is handed while the catalog replays', () => {
    useClustersMock.mockReturnValue({ clusters: [clusterFixture({ status: 'True', reason: 'Watching' })] });
    const { result } = renderHook(
      () => useDashboardNavPlace('autoscaling/horizontalpodautoscalers', { ...HPA, printerColumns: [] }),
      {
        wrapper: ({ children }: { children: ReactNode }) => (
          <ClusterCachedDataKindsProvider enabled>{children}</ClusterCachedDataKindsProvider>
        ),
      },
    );
    expect(result.current).toEqual({ section: 'namespace', sectionLabel: 'Namespace', group: 'Workloads' });
  });
});
