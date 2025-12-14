// Copyright 2024-2025 Andres Morey
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

import { act, render, screen, waitFor } from '@testing-library/react';
import { createStore, Provider } from 'jotai';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { WatchEventType } from '@/lib/graphql/dashboard/__generated__/graphql';

import { isFollowAtom, isLoadingAtom, isReadyAtom } from './state';
import { ViewerProvider, useNodes, useViewerFacets, useViewerMetadata } from './viewer';

const mockUseSubscription = vi.fn();
const mockUseQuery = vi.fn();
const mockUseListQueryWithSubscription = vi.fn();
const mockUseIsClusterAPIEnabled = vi.fn();

vi.mock('@apollo/client', () => ({
  useSubscription: (query: any, options: any) => mockUseSubscription(query, options),
  useQuery: (...args: any[]) => mockUseQuery(...args),
}));

vi.mock('@/lib/hooks', async () => {
  const actual = await vi.importActual<typeof import('@/lib/hooks')>('@/lib/hooks');

  return {
    ...actual,
    useListQueryWithSubscription: (opts: any) => mockUseListQueryWithSubscription(opts),
    useIsClusterAPIEnabled: (kubeContext: string | null) => mockUseIsClusterAPIEnabled(kubeContext),
    // Keep nextTick synchronous to simplify scheduling in tests.
    useNextTick: () => (fn: () => void) => fn(),
  };
});

vi.mock('@/components/utils/LoadingPage', () => ({
  default: () => <div data-testid="loading-page" />,
}));

const defaultViewerProviderProps = {
  kubeContext: 'ctx',
  sources: [] as string[],
  sourceFilter: {},
  grep: null,
};

beforeEach(() => {
  mockUseIsClusterAPIEnabled.mockReturnValue(true);
  mockUseSubscription.mockReturnValue({ loading: false });
  mockUseQuery.mockReturnValue({ refetch: vi.fn(), subscribeToMore: vi.fn() });
});

describe('useNodes', () => {
  it('returns nodes and loading flag from the list query', () => {
    const node = {
      __typename: 'CoreV1Node',
      id: '1',
      metadata: {
        name: 'node-a',
        uid: 'uid-1',
        creationTimestamp: '2024-01-01T00:00:00Z',
        deletionTimestamp: null,
        resourceVersion: '1',
        labels: null,
        annotations: null,
      },
    };

    mockUseListQueryWithSubscription.mockReturnValue({
      fetching: false,
      data: { coreV1NodesList: { items: [node] } },
    });

    const NodesConsumer = () => {
      const { loading, nodes } = useNodes();
      return (
        <div>
          <div data-testid="loading">{loading ? 'true' : 'false'}</div>
          <div data-testid="nodes">{nodes.map((n) => n.metadata.name).join(',')}</div>
        </div>
      );
    };

    render(
      <ViewerProvider {...defaultViewerProviderProps}>
        <NodesConsumer />
      </ViewerProvider>,
    );

    expect(screen.getByTestId('loading').textContent).toBe('false');
    expect(screen.getByTestId('nodes').textContent).toBe('node-a');
  });
});

describe('useViewerMetadata', () => {
  it('returns atom values and search flag', () => {
    const store = createStore();
    store.set(isReadyAtom, true);
    store.set(isLoadingAtom, false);
    store.set(isFollowAtom, false);

    const MetadataConsumer = () => {
      const metadata = useViewerMetadata();
      return (
        <div>
          <div data-testid="ready">{String(metadata.isReady)}</div>
          <div data-testid="loading">{String(metadata.isLoading)}</div>
          <div data-testid="follow">{String(metadata.isFollow)}</div>
          <div data-testid="search">{String(metadata.isSearchEnabled)}</div>
        </div>
      );
    };

    render(
      <Provider store={store}>
        <ViewerProvider {...defaultViewerProviderProps}>
          <MetadataConsumer />
        </ViewerProvider>
      </Provider>,
    );

    expect(screen.getByTestId('ready').textContent).toBe('true');
    expect(screen.getByTestId('loading').textContent).toBe('false');
    expect(screen.getByTestId('follow').textContent).toBe('false');
    expect(screen.getByTestId('search').textContent).toBe('true');
  });
});

describe('useViewerFacets', () => {
  it('aggregates facet counters from sources and nodes', async () => {
    const nodes = [
      {
        __typename: 'CoreV1Node',
        id: 'node-0',
        metadata: {
          name: 'node-0',
          uid: 'uid-0',
          creationTimestamp: '2024-01-01T00:00:00Z',
          deletionTimestamp: null,
          resourceVersion: '1',
          labels: null,
          annotations: null,
        },
      },
    ];

    mockUseListQueryWithSubscription.mockReturnValue({
      fetching: false,
      data: { coreV1NodesList: { items: nodes } },
    });

    const sourceWest = {
      __typename: 'LogSource',
      namespace: 'default',
      podName: 'app',
      containerName: 'main',
      containerID: 'cid-1',
      metadata: { region: 'us-west', zone: 'zone-a', os: 'linux', arch: 'amd64', node: 'node-1' },
    };

    const sourceEast = {
      __typename: 'LogSource',
      namespace: 'kube-system',
      podName: 'agent',
      containerName: 'sidecar',
      containerID: 'cid-2',
      metadata: { region: 'us-east', zone: 'zone-b', os: 'linux', arch: 'arm64', node: 'node-2' },
    };

    let onData: ((args: any) => void) | undefined;
    mockUseSubscription.mockImplementation((_, options) => {
      onData = options?.onData;
      return { loading: false };
    });

    const FacetsConsumer = () => {
      const facets = useViewerFacets();
      return (
        <div>
          <div data-testid="region-west">{facets.region.get('us-west') ?? 0}</div>
          <div data-testid="region-east">{facets.region.get('us-east') ?? 0}</div>
          <div data-testid="node-zero">{facets.node.get('node-0') ?? 0}</div>
          <div data-testid="node-one">{facets.node.get('node-1') ?? 0}</div>
          <div data-testid="node-two">{facets.node.get('node-2') ?? 0}</div>
        </div>
      );
    };

    render(
      <ViewerProvider {...defaultViewerProviderProps}>
        <FacetsConsumer />
      </ViewerProvider>,
    );

    act(() => {
      onData?.({ data: { data: { logSourcesWatch: { type: WatchEventType.Added, object: sourceWest } } } });
      onData?.({ data: { data: { logSourcesWatch: { type: WatchEventType.Added, object: sourceEast } } } });
    });

    await waitFor(() => {
      expect(screen.getByTestId('region-west').textContent).toBe('1');
      expect(screen.getByTestId('region-east').textContent).toBe('1');
      expect(screen.getByTestId('node-zero').textContent).toBe('0');
      expect(screen.getByTestId('node-one').textContent).toBe('1');
      expect(screen.getByTestId('node-two').textContent).toBe('1');
    });
  });
});

describe('ViewerProvider', () => {
  it('shows loading page while cluster API status is pending', () => {
    mockUseIsClusterAPIEnabled.mockReturnValueOnce(undefined);

    render(
      <ViewerProvider {...defaultViewerProviderProps}>
        <div data-testid="child">ready</div>
      </ViewerProvider>,
    );

    expect(screen.getByTestId('loading-page')).toBeInTheDocument();
    expect(screen.queryByTestId('child')).toBeNull();
  });

  it('renders children when cluster API status is resolved', () => {
    mockUseIsClusterAPIEnabled.mockReturnValueOnce(true);

    render(
      <ViewerProvider {...defaultViewerProviderProps}>
        <div data-testid="child">ready</div>
      </ViewerProvider>,
    );

    expect(screen.getByTestId('child')).toBeInTheDocument();
    expect(screen.queryByTestId('loading-page')).toBeNull();
  });
});
