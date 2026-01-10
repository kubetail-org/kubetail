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
import { createStore, Provider, useAtomValue } from 'jotai';
import { MemoryRouter } from 'react-router-dom';

import type { LogSourceFragmentFragment } from '@/lib/graphql/dashboard/__generated__/graphql';
import { WatchEventType } from '@/lib/graphql/dashboard/__generated__/graphql';

import { PageContext } from './shared';
import { sourceMapAtom } from './state';
import { useNodes, useFacets, SourcesFetcher } from './helpers';

const mockUseListQueryWithSubscription = vi.fn();
const mockUseSubscription = vi.fn();

vi.mock('@apollo/client/react', () => ({
  useSubscription: (query: unknown, options: unknown) => mockUseSubscription(query, options),
}));

vi.mock('@/lib/hooks', async () => {
  const actual = await vi.importActual<typeof import('@/lib/hooks')>('@/lib/hooks');

  return {
    ...actual,
    useListQueryWithSubscription: (opts: unknown) => mockUseListQueryWithSubscription(opts),
  };
});

const defaultPageContextValue = {
  kubeContext: 'ctx',
  shouldUseClusterAPI: true,
  logServerClient: undefined,
  grep: null,
  logViewerRef: { current: null },
  isSidebarOpen: true,
  setIsSidebarOpen: vi.fn(),
};

// Wrapper that provides PageContext
const TestWrapper = ({
  children,
  store,
  contextValue = defaultPageContextValue,
}: {
  children: React.ReactNode;
  store?: ReturnType<typeof createStore>;
  contextValue?: typeof defaultPageContextValue;
}) => {
  const content = (
    <MemoryRouter>
      <PageContext.Provider value={contextValue}>{children}</PageContext.Provider>
    </MemoryRouter>
  );

  if (store) {
    return <Provider store={store}>{content}</Provider>;
  }

  return content;
};

beforeEach(() => {
  vi.clearAllMocks();
  mockUseListQueryWithSubscription.mockReturnValue({
    fetching: false,
    data: { coreV1NodesList: { items: [] } },
  });
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
      <TestWrapper>
        <NodesConsumer />
      </TestWrapper>,
    );

    expect(screen.getByTestId('loading').textContent).toBe('false');
    expect(screen.getByTestId('nodes').textContent).toBe('node-a');
  });

  it('returns empty nodes array when data is undefined', () => {
    mockUseListQueryWithSubscription.mockReturnValue({
      fetching: true,
      data: undefined,
    });

    const NodesConsumer = () => {
      const { loading, nodes } = useNodes();
      return (
        <div>
          <div data-testid="loading">{loading ? 'true' : 'false'}</div>
          <div data-testid="nodes-count">{nodes.length}</div>
        </div>
      );
    };

    render(
      <TestWrapper>
        <NodesConsumer />
      </TestWrapper>,
    );

    expect(screen.getByTestId('loading').textContent).toBe('true');
    expect(screen.getByTestId('nodes-count').textContent).toBe('0');
  });
});

describe('useFacets', () => {
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

    const sourceWest: LogSourceFragmentFragment = {
      __typename: 'LogSource',
      namespace: 'default',
      podName: 'app',
      containerName: 'main',
      containerID: 'cid-1',
      metadata: { region: 'us-west', zone: 'zone-a', os: 'linux', arch: 'amd64', node: 'node-1' },
    };

    const sourceEast: LogSourceFragmentFragment = {
      __typename: 'LogSource',
      namespace: 'kube-system',
      podName: 'agent',
      containerName: 'sidecar',
      containerID: 'cid-2',
      metadata: { region: 'us-east', zone: 'zone-b', os: 'linux', arch: 'arm64', node: 'node-2' },
    };

    // Set up Jotai store with sources
    const store = createStore();
    const sourcesMap = new Map<string, LogSourceFragmentFragment>();
    sourcesMap.set('default/app/main', sourceWest);
    sourcesMap.set('kube-system/agent/sidecar', sourceEast);
    store.set(sourceMapAtom, sourcesMap);

    const FacetsConsumer = () => {
      const facets = useFacets();
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
      <TestWrapper store={store}>
        <FacetsConsumer />
      </TestWrapper>,
    );

    await waitFor(() => {
      expect(screen.getByTestId('region-west').textContent).toBe('1');
      expect(screen.getByTestId('region-east').textContent).toBe('1');
      expect(screen.getByTestId('node-zero').textContent).toBe('0');
      expect(screen.getByTestId('node-one').textContent).toBe('1');
      expect(screen.getByTestId('node-two').textContent).toBe('1');
    });
  });

  it('returns empty counters when no sources', () => {
    mockUseListQueryWithSubscription.mockReturnValue({
      fetching: false,
      data: { coreV1NodesList: { items: [] } },
    });

    const store = createStore();
    store.set(sourceMapAtom, new Map());

    const FacetsConsumer = () => {
      const facets = useFacets();
      return (
        <div>
          <div data-testid="region-size">{facets.region.size}</div>
          <div data-testid="zone-size">{facets.zone.size}</div>
        </div>
      );
    };

    render(
      <TestWrapper store={store}>
        <FacetsConsumer />
      </TestWrapper>,
    );

    expect(screen.getByTestId('region-size').textContent).toBe('0');
    expect(screen.getByTestId('zone-size').textContent).toBe('0');
  });
});

describe('SourcesFetcher', () => {
  // Helper to render SourcesFetcher with router that has search params
  const renderSourcesFetcher = (
    store: ReturnType<typeof createStore>,
    searchParams = '',
    contextValue = defaultPageContextValue,
  ) => {
    const SourcesDisplay = () => {
      const sources = useAtomValue(sourceMapAtom);
      return <div data-testid="sources-count">{sources.size}</div>;
    };

    return render(
      <Provider store={store}>
        <MemoryRouter initialEntries={[`/?${searchParams}`]}>
          <PageContext.Provider value={contextValue}>
            <SourcesFetcher />
            <SourcesDisplay />
          </PageContext.Provider>
        </MemoryRouter>
      </Provider>,
    );
  };

  it('subscribes to LOG_SOURCES_WATCH with correct variables', () => {
    const store = createStore();

    renderSourcesFetcher(store, 'source=ns/pod/container', {
      ...defaultPageContextValue,
      kubeContext: 'my-cluster',
    });

    expect(mockUseSubscription).toHaveBeenCalledWith(
      expect.anything(),
      expect.objectContaining({
        variables: {
          kubeContext: 'my-cluster',
          sources: ['ns/pod/container'],
        },
      }),
    );
  });

  it('handles multiple source params', () => {
    const store = createStore();

    renderSourcesFetcher(store, 'source=ns1/pod1/c1&source=ns2/pod2/c2');

    expect(mockUseSubscription).toHaveBeenCalledWith(
      expect.anything(),
      expect.objectContaining({
        variables: {
          kubeContext: 'ctx',
          sources: ['ns1/pod1/c1', 'ns2/pod2/c2'],
        },
      }),
    );
  });

  it('adds source to sourceMapAtom on Added event', async () => {
    const store = createStore();
    store.set(sourceMapAtom, new Map());

    let capturedOnData: ((args: { data: { data?: { logSourcesWatch?: unknown } } }) => void) | undefined;

    mockUseSubscription.mockImplementation((_, options) => {
      capturedOnData = options?.onData;
      return { loading: false };
    });

    renderSourcesFetcher(store);

    const newSource: LogSourceFragmentFragment = {
      __typename: 'LogSource',
      namespace: 'default',
      podName: 'my-pod',
      containerName: 'my-container',
      containerID: 'cid-123',
      metadata: { region: 'us-west', zone: 'zone-a', os: 'linux', arch: 'amd64', node: 'node-1' },
    };

    act(() => {
      capturedOnData?.({
        data: {
          data: {
            logSourcesWatch: {
              type: WatchEventType.Added,
              object: newSource,
            },
          },
        },
      });
    });

    await waitFor(() => {
      expect(screen.getByTestId('sources-count').textContent).toBe('1');
    });

    const sources = store.get(sourceMapAtom);
    expect(sources.get('default/my-pod/my-container')).toEqual(newSource);
  });

  it('removes source from sourceMapAtom on Deleted event', async () => {
    const store = createStore();
    const existingSource: LogSourceFragmentFragment = {
      __typename: 'LogSource',
      namespace: 'default',
      podName: 'my-pod',
      containerName: 'my-container',
      containerID: 'cid-123',
      metadata: { region: 'us-west', zone: 'zone-a', os: 'linux', arch: 'amd64', node: 'node-1' },
    };
    const initialMap = new Map<string, LogSourceFragmentFragment>();
    initialMap.set('default/my-pod/my-container', existingSource);
    store.set(sourceMapAtom, initialMap);

    let capturedOnData: ((args: { data: { data?: { logSourcesWatch?: unknown } } }) => void) | undefined;

    mockUseSubscription.mockImplementation((_, options) => {
      capturedOnData = options?.onData;
      return { loading: false };
    });

    renderSourcesFetcher(store);

    // Verify initial state
    expect(screen.getByTestId('sources-count').textContent).toBe('1');

    act(() => {
      capturedOnData?.({
        data: {
          data: {
            logSourcesWatch: {
              type: WatchEventType.Deleted,
              object: existingSource,
            },
          },
        },
      });
    });

    await waitFor(() => {
      expect(screen.getByTestId('sources-count').textContent).toBe('0');
    });

    const sources = store.get(sourceMapAtom);
    expect(sources.has('default/my-pod/my-container')).toBe(false);
  });

  it('ignores events with missing data', () => {
    const store = createStore();
    store.set(sourceMapAtom, new Map());

    let capturedOnData: ((args: { data: { data?: { logSourcesWatch?: unknown } } }) => void) | undefined;

    mockUseSubscription.mockImplementation((_, options) => {
      capturedOnData = options?.onData;
      return { loading: false };
    });

    renderSourcesFetcher(store);

    // Call with missing logSourcesWatch
    act(() => {
      capturedOnData?.({ data: { data: undefined } });
    });

    expect(screen.getByTestId('sources-count').textContent).toBe('0');

    // Call with missing object
    act(() => {
      capturedOnData?.({
        data: {
          data: {
            logSourcesWatch: {
              type: WatchEventType.Added,
              object: null,
            },
          },
        },
      });
    });

    expect(screen.getByTestId('sources-count').textContent).toBe('0');
  });
});
