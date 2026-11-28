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

import { createMemoryHistory, createRouter, RouterProvider } from '@tanstack/react-router';
import type { AnyRoute } from '@tanstack/react-router';
import { act, render } from '@testing-library/react';
import { expect, vi } from 'vitest';

import type { Cluster } from '@/lib/clusters';
import { setSubscriptionFlushScheduler } from '@/lib/graphql/use-folded-subscription';
import type { Incident, IncidentOccurrence, MonitorTarget } from '@/lib/incidents';
import type { LogWindow } from '@/lib/log-windows';

export type FakeChannel = { onmessage?: (raw: string) => void };

// Shared fake for '@tauri-apps/api/core'. Usage:
//
//   const { invokeMock, channels, liveChannel, factory } = mockTauriCore();
//   vi.mock('@tauri-apps/api/core', () => factory());
//
// `vi.mock` is hoisted, but its factory runs lazily (at the dynamic `await
// import` of the unit under test), by which point this helper has initialised.
export function mockTauriCore() {
  const invokeMock = vi.fn();
  const channels: FakeChannel[] = [];
  // The live fake Channel for the subscription whose query contains `queryPart`.
  // subscribe-exchange news one Channel per `graphql_subscribe` in call order,
  // so the Nth matching subscribe maps to `channels[N]`; the *last* match is the
  // live one, since a reconnect opens a fresh subscription for the same query.
  // `findLastIndex` is ES2023, outside the app's TS lib — hence `lastIndexOf`.
  const channelFor = (queryPart: string): FakeChannel => {
    const subs = invokeMock.mock.calls.filter(([cmd]) => cmd === 'graphql_subscribe');
    const idx = subs.map(([, arg]) => (arg as { query: string }).query.includes(queryPart)).lastIndexOf(true);
    if (idx < 0) throw new Error(`no subscription for ${queryPart}`);
    return channels[idx];
  };
  return {
    invokeMock,
    channels,
    channelFor,
    liveChannel: () => channels.at(-1)!,
    factory: () => ({
      invoke: (...args: unknown[]) => invokeMock(...args),
      Channel: function FakeChannelCtor(this: FakeChannel, onmessage?: (raw: string) => void) {
        // oxlint-disable-next-line react/no-this-in-sfc, unicorn/prefer-add-event-listener -- a constructor, called with `new` as Tauri's `Channel` is, and no `EventTarget`
        if (onmessage) this.onmessage = onmessage;
        channels.push(this);
      },
    }),
  };
}

// Watches publish once per animation frame. A suite that pushes a frame and asserts on
// the very next line wants each frame visible at once: call this at module scope.
export function flushSubscriptionsSynchronously() {
  setSubscriptionFlushScheduler((flush) => {
    flush();
    return () => {};
  });
}

// Cluster test helpers ------ ------------------------------------------
//
// Seed the ClustersProvider by pushing `Added` frames onto `clusterCollectionWatch`.

// Cluster/user metadata derive from `name` so a resolved context has predictable
// fields to assert on.
export type ClusterRow = {
  id: string;
  name: string;
  syncEnabled?: boolean;
  enabled?: boolean;
  present?: boolean;
  // The kubeconfig's current context.
  current?: boolean;
  // The user's default cluster.
  isDefault?: boolean;
  // The cluster entry the context names; null puts the record in the state a
  // kubeconfig naming an undefined entry produces.
  server?: string;
  insecureSkipTLSVerify?: boolean;
  // Set to mark the record as being torn down; the boundary streams these through.
  deleting?: boolean;
};

export function clusterOf(r: ClusterRow) {
  return {
    id: r.id,
    deletionRequestedAt: r.deleting ? '2026-08-16T00:00:00Z' : null,
    spec: {
      name: r.name,
      syncEnabled: r.syncEnabled ?? true,
      enabled: r.enabled ?? true,
      isDefault: r.isDefault ?? false,
      source: { kubeconfig: { context: r.name } },
    },
    status: {
      source: {
        kubeconfig: {
          cluster: {
            name: `${r.name}-cluster`,
            entry: {
              server: r.server ?? `https://${r.name}.example:6443`,
              insecureSkipTLSVerify: r.insecureSkipTLSVerify ?? false,
            },
          },
          user: { name: `${r.name}-user` },
          isPresent: r.present ?? true,
          isDefault: r.current ?? false,
        },
      },
      server: { uid: `uid-${r.id}` },
      conditions: [],
    },
  };
}

// A joined `Cluster` as `useClusters` serves it, for the pure readers over one: a
// connected record whose cache is watching, unless told otherwise.
export function clusterRecord({
  context,
  connected = 'True',
  reason = 'Watching',
}: {
  context: string;
  connected?: 'True' | 'False' | 'Unknown';
  // The cache's folded sync verdict; null for a record with no verdict yet.
  reason?: string | null;
}): Cluster {
  const id = `id-${context}`;
  return {
    id,
    deletionRequestedAt: null,
    spec: { name: '', syncEnabled: true, enabled: true, isDefault: false, source: { kubeconfig: { context } } },
    status: {
      source: {
        kubeconfig: {
          cluster: { name: context, entry: { server: 'https://example:6443', insecureSkipTLSVerify: false } },
          user: { name: context },
          isPresent: true,
          isDefault: false,
        },
      },
      server: { uid: `uid-${context}` },
    },
    conditions: [
      {
        type: 'Connected',
        status: connected,
        reason: '',
        message: '',
        liveness: true,
        unconfirmed: false,
        transitionedAt: '2026-10-29T00:00:00Z',
      },
    ],
    activeCache: {
      id: `cache-${context}`,
      clusterID: id,
      spec: { serverUid: `uid-${context}` },
      health:
        reason === null
          ? null
          : {
              cacheID: `cache-${context}`,
              status: reason === 'Watching' ? 'True' : 'False',
              reason,
              unhealthyKindRefs: [],
              failure: '',
              totalKinds: 1,
              unhealthyKinds: 0,
              pausedKinds: 0,
              lastUpdateAt: null,
              lastLiveAt: null,
            },
    },
  };
}

// A monitor target as `monitorTargetCollectionWatch` delivers it: on, judged everything,
// and reading the server `uid-1`.
export function monitorTargetRecord(over: Partial<MonitorTarget> = {}): MonitorTarget {
  return {
    id: 't1',
    integration: 'Kubernetes',
    label: 'prod',
    clusterID: 'id-prod',
    currentUID: 'uid-1',
    enabled: true,
    readState: 'Ok',
    unknownSignals: [],
    ...over,
  };
}

// An open incident on `t1`, opened under the server `uid-1`.
export function incidentRecord(over: Partial<Incident> = {}): Incident {
  return {
    id: 'i1',
    targetID: 't1',
    targetUID: 'uid-1',
    subjectLabel: 'Deployment default/web',
    subjectAttributes: [{ label: 'kind', value: 'Deployment' }],
    signal: 'kubernetes.crash_loop',
    signalLabel: 'Crash loop',
    severity: 'Critical',
    firstSeenAt: '2026-11-01T00:00:00Z',
    open: true,
    occurrenceCount: 1,
    lastStartedAt: '2026-11-01T00:00:00Z',
    lastSeenAt: null,
    lastEndReason: null,
    ...over,
  };
}

// An occurrence of `i1` that has not ended.
export function occurrenceRecord(over: Partial<IncidentOccurrence> = {}): IncidentOccurrence {
  return {
    id: 'o1',
    incidentID: 'i1',
    number: 1,
    startedAt: '2026-11-01T00:00:00Z',
    endedAt: null,
    endReason: null,
    detail: [],
    ...over,
  };
}

// Close a collection watch's snapshot the way the server does: one Bookmark carrying no
// item. A consumer treats the stream as still loading until it lands, so a test
// asserting a loaded state — an empty one especially — has to send it.
//
// itemField is the frame's own item selection (`cluster` on clusterCollectionWatch, `cache`
// on clusterCacheCollectionWatch): it is null on this frame alone, and each watch names it
// differently, so the caller says which.
export function pushCollectionWatchBookmark(
  channelFor: (queryPart: string) => FakeChannel,
  watchField: string,
  itemField: string,
) {
  channelFor(watchField).onmessage!(
    JSON.stringify({ type: 'next', payload: { data: { [watchField]: { type: 'Bookmark', [itemField]: null } } } }),
  );
}

// Pass the `channelFor` from `mockTauriCore()`. Sends the rows as the snapshot and
// closes it, so the registry reads as loaded.
export function pushClusters(channelFor: (queryPart: string) => FakeChannel, rows: ClusterRow[]) {
  const ch = channelFor('clusterCollectionWatch');
  rows.forEach((r) => {
    ch.onmessage!(
      JSON.stringify({
        type: 'next',
        payload: { data: { clusterCollectionWatch: { type: 'Added', cluster: clusterOf(r) } } },
      }),
    );
  });
  pushCollectionWatchBookmark(channelFor, 'clusterCollectionWatch', 'cluster');
}

// Sends one row's change on `clusterCollectionWatch`, after its snapshot.
export function pushClusterModified(channelFor: (queryPart: string) => FakeChannel, row: ClusterRow) {
  channelFor('clusterCollectionWatch').onmessage!(
    JSON.stringify({
      type: 'next',
      payload: { data: { clusterCollectionWatch: { type: 'Modified', cluster: clusterOf(row) } } },
    }),
  );
}

// Shared fake for '@tauri-apps/api/window', reached via `getCurrentWindow`.
// Usage mirrors `mockTauriCore`:
//
//   const { windowMock, factory } = mockTauriWindow();
//   vi.mock('@tauri-apps/api/window', () => factory());
export function mockTauriWindow() {
  const windowMock = {
    minimize: vi.fn(() => Promise.resolve()),
    toggleMaximize: vi.fn(() => Promise.resolve()),
    close: vi.fn(() => Promise.resolve()),
    isMaximized: vi.fn(() => Promise.resolve(false)),
    // Matches Tauri's `onResized`: resolves to an unlisten fn.
    onResized: vi.fn(() => Promise.resolve(() => {})),
  };
  return {
    windowMock,
    factory: () => ({ getCurrentWindow: () => windowMock }),
  };
}

// Platform-branched UI (`isMacOS`/`isLinux`) keys off the WebView's per-OS UA,
// so tests flip it to pick a platform. `NON_MAC_USER_AGENT` is Linux
// (frameless + transparent); `WINDOWS_USER_AGENT` is frameless but opaque.
// See docs/adr/2026-08-09-per-platform-window-chrome.md
export const MAC_USER_AGENT = 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15';
export const NON_MAC_USER_AGENT = 'Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36';
export const WINDOWS_USER_AGENT = 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36';

const originalUserAgent = window.navigator.userAgent;

export function setUserAgent(value: string) {
  Object.defineProperty(window.navigator, 'userAgent', { value, configurable: true });
}

export function restoreUserAgent() {
  setUserAgent(originalUserAgent);
}

// Shared fake for '@tauri-apps/api/event': captures `listen` registrations and
// fires events to them wrapped in `act`. Usage mirrors `mockTauriCore`:
//
//   const { emitEvent, factory } = mockTauriEvent();
//   vi.mock('@tauri-apps/api/event', () => factory());
//   ...
//   emitEvent('host-file-updated', { colorSchemePreference: 'dark' });
export function mockTauriEvent() {
  const listeners = new Map<string, Set<(event: { payload: unknown }) => void>>();
  return {
    emitEvent: (name: string, payload: unknown) => {
      act(() => {
        listeners.get(name)?.forEach((cb) => cb({ payload }));
      });
    },
    factory: () => ({
      listen: (name: string, cb: (event: { payload: unknown }) => void) => {
        let set = listeners.get(name);
        if (!set) {
          set = new Set();
          listeners.set(name, set);
        }
        set.add(cb);
        return Promise.resolve(() => listeners.get(name)?.delete(cb));
      },
    }),
  };
}

// Controllable `window.matchMedia`, returning a `setMatches(next)` that flips
// `matches` and fires `change` (in `act`) like the browser — for the
// color-scheme query and the sidebar breakpoint. `matches` is a live getter so a
// captured `MediaQueryList` reflects later flips. jsdom's default,
// non-controllable stub lives in `vitest.setup.ts`.
export function mockMatchMedia(initialMatches = false) {
  let matches = initialMatches;
  const listeners = new Set<(e: MediaQueryListEvent) => void>();
  window.matchMedia = ((query: string) => ({
    get matches() {
      return matches;
    },
    media: query,
    onchange: null,
    addEventListener: (_type: string, cb: (e: MediaQueryListEvent) => void) => listeners.add(cb),
    removeEventListener: (_type: string, cb: (e: MediaQueryListEvent) => void) => listeners.delete(cb),
    addListener: () => {},
    removeListener: () => {},
    dispatchEvent: () => false,
  })) as unknown as typeof window.matchMedia;
  return (next: boolean) => {
    matches = next;
    act(() => {
      listeners.forEach((cb) => cb({ matches } as MediaQueryListEvent));
    });
  };
}

export async function renderWithRouter(routeTree: AnyRoute, path: string) {
  const router = createRouter({
    routeTree,
    history: createMemoryHistory({ initialEntries: [path] }),
  });

  await router.load();

  let result!: ReturnType<typeof render>;
  await act(async () => {
    result = render(<RouterProvider router={router} />);
  });

  return {
    ...result,
    router,
  };
}

// jsdom has no `ResizeObserver`. Gives one that reports once, on `observe`. Call at module
// scope in any suite that renders a component measuring itself (the app bar).
export function stubResizeObserver() {
  vi.stubGlobal(
    'ResizeObserver',
    class {
      constructor(private readonly cb: (entries: unknown[], observer: unknown) => void) {}

      observe() {
        // With a real callback's arguments: a popover's positioning reads the entries.
        this.cb([], this);
      }

      disconnect() {}
    },
  );
}

// jsdom has no layout: every element is 0px tall, so a `VirtualTable` renders no rows. Gives
// every element `clientHeight`, and a `ResizeObserver`. Call at module scope in any suite
// that renders rows.
export function stubLayout(clientHeight: number) {
  Object.defineProperty(HTMLElement.prototype, 'clientHeight', { configurable: true, get: () => clientHeight });
  stubResizeObserver();
}

// A text box with the webview's suggestions off (`components/primitives/text-entry.tsx`).
function expectNoSuggestions(el: HTMLElement) {
  expect(el).toHaveAttribute('autocomplete', 'off');
  expect(el).toHaveAttribute('autocorrect', 'off');
  expect(el).toHaveAttribute('autocapitalize', 'off');
  expect(el).toHaveAttribute('writingsuggestions', 'false');
}

export function expectCodeText(el: HTMLElement) {
  expectNoSuggestions(el);
  expect(el).toHaveAttribute('spellcheck', 'false');
}

export function expectProseText(el: HTMLElement) {
  expectNoSuggestions(el);
  expect(el).toHaveAttribute('spellcheck', 'true');
}

// A `LogWindowsContext` value for a suite that draws what reads the app window's log
// windows: `windows` holds the calls that have one; `open` and `focus` record calls.
export function fakeLogWindows() {
  const windows: LogWindow[] = [];
  return {
    windows,
    value: {
      ready: true,
      windowOf: (chatId: string, callId: string) => windows.find((w) => w.chatId === chatId && w.callId === callId),
      open: vi.fn(),
      focus: vi.fn(),
      shownWindowOf: () => null,
    },
  };
}
