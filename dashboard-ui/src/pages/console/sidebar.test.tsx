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

import { render, screen } from '@testing-library/react';
import { createStore, Provider } from 'jotai';
import { MemoryRouter } from 'react-router-dom';

import type { LogSourceFragmentFragment } from '@/lib/graphql/dashboard/__generated__/graphql';
import { Counter } from '@/lib/util';

import { Sidebar, generateMapKey, parseSourceArg } from './sidebar';
import { PageContext } from './shared';
import { sourceMapAtom } from './state';

// Mock useFacets hook from helpers
const mockUseFacets = vi.fn();

vi.mock('./helpers', () => ({
  useFacets: () => mockUseFacets(),
}));

vi.mock('@/components/widgets/SourcePickerModal', () => ({
  default: ({ open }: { open: boolean }) => (
    <div data-testid="source-picker-modal" data-open={open}>
      Source Picker Modal
    </div>
  ),
}));

// Helper to create a mock LogSourceFragmentFragment
const createMockSource = (
  namespace: string,
  podName: string,
  containerName: string,
  metadata: Partial<LogSourceFragmentFragment['metadata']> = {},
): LogSourceFragmentFragment => ({
  __typename: 'LogSource',
  namespace,
  podName,
  containerName,
  containerID: `${namespace}-${podName}-${containerName}`,
  metadata: {
    __typename: 'LogSourceMetadata',
    region: metadata.region ?? '',
    zone: metadata.zone ?? '',
    os: metadata.os ?? '',
    arch: metadata.arch ?? '',
    node: metadata.node ?? '',
  },
});

// Default page context value
const defaultPageContextValue = {
  kubeContext: null,
  shouldUseClusterAPI: undefined,
  logServerClient: undefined,
  grep: null,
  logViewerRef: { current: null },
  isSidebarOpen: true,
  setIsSidebarOpen: vi.fn(),
};

// Test wrapper that provides required providers
const TestWrapper = ({
  children,
  store,
  initialEntries = ['/'],
}: {
  children: React.ReactNode;
  store?: ReturnType<typeof createStore>;
  initialEntries?: string[];
}) => {
  const content = (
    <MemoryRouter initialEntries={initialEntries}>
      <PageContext.Provider value={defaultPageContextValue}>{children}</PageContext.Provider>
    </MemoryRouter>
  );

  if (store) {
    return <Provider store={store}>{content}</Provider>;
  }

  return content;
};

// Helper to create store with sources
const createStoreWithSources = (sources: LogSourceFragmentFragment[]) => {
  const store = createStore();
  const sourceMap = new Map<string, LogSourceFragmentFragment>();
  sources.forEach((source) => {
    const key = `${source.namespace}/${source.podName}/${source.containerName}`;
    sourceMap.set(key, source);
  });
  store.set(sourceMapAtom, sourceMap);
  return store;
};

// Default empty facets
const emptyFacets = () => ({
  region: new Counter(),
  zone: new Counter(),
  os: new Counter(),
  arch: new Counter(),
  node: new Counter(),
});

beforeEach(() => {
  mockUseFacets.mockReturnValue(emptyFacets());
});

describe('Sidebar', () => {
  it('renders the Kubetail logo', () => {
    render(
      <TestWrapper>
        <Sidebar />
      </TestWrapper>,
    );

    expect(screen.getByRole('link')).toBeInTheDocument();
  });

  it('displays cluster context when present in URL', () => {
    render(
      <TestWrapper initialEntries={['/?kubeContext=production-cluster']}>
        <Sidebar />
      </TestWrapper>,
    );

    expect(screen.getByText(/Cluster:/)).toBeInTheDocument();
    expect(screen.getByText(/production-cluster/)).toBeInTheDocument();
  });

  it('does not display cluster context when not in URL', () => {
    render(
      <TestWrapper>
        <Sidebar />
      </TestWrapper>,
    );

    expect(screen.queryByText(/Cluster:/)).not.toBeInTheDocument();
  });

  it('renders the source picker button', () => {
    render(
      <TestWrapper>
        <Sidebar />
      </TestWrapper>,
    );

    expect(screen.getByRole('button', { name: /open source picker/i })).toBeInTheDocument();
  });

  it('renders Pods/Containers section', () => {
    render(
      <TestWrapper>
        <Sidebar />
      </TestWrapper>,
    );

    expect(screen.getByText('Pods/Containers')).toBeInTheDocument();
  });
});

describe('SidebarWorkloads', () => {
  it('displays workloads from URL sources', () => {
    render(
      <TestWrapper initialEntries={['/?source=default:deployments/nginx&source=default:deployments/app']}>
        <Sidebar />
      </TestWrapper>,
    );

    expect(screen.getByText('nginx')).toBeInTheDocument();
    expect(screen.getByText('app')).toBeInTheDocument();
  });

  it('groups workloads by kind', () => {
    render(
      <TestWrapper initialEntries={['/?source=default:deployments/nginx&source=kube-system:daemonsets/kube-proxy']}>
        <Sidebar />
      </TestWrapper>,
    );

    expect(screen.getByText('Deployments')).toBeInTheDocument();
    expect(screen.getByText('Daemon Sets')).toBeInTheDocument();
  });

  it('renders delete button for each workload', () => {
    render(
      <TestWrapper initialEntries={['/?source=default:deployments/nginx']}>
        <Sidebar />
      </TestWrapper>,
    );

    expect(screen.getAllByRole('button', { name: /delete source/i }).length).toBeGreaterThan(0);
  });

  it('sorts workloads alphabetically within each kind', () => {
    render(
      <TestWrapper initialEntries={['/?source=default:deployments/zebra&source=default:deployments/alpha']}>
        <Sidebar />
      </TestWrapper>,
    );

    const deploymentItems = screen.getAllByText(/alpha|zebra/);
    expect(deploymentItems[0]).toHaveTextContent('alpha');
    expect(deploymentItems[1]).toHaveTextContent('zebra');
  });

  it('handles workload names with wildcards', () => {
    render(
      <TestWrapper initialEntries={['/?source=default:deployments/nginx/*']}>
        <Sidebar />
      </TestWrapper>,
    );

    expect(screen.getByText('nginx')).toBeInTheDocument();
  });

  it('handles multiple workload types', () => {
    render(
      <TestWrapper
        initialEntries={['/?source=default:deployments/app1&source=default:statefulsets/db&source=default:jobs/task']}
      >
        <Sidebar />
      </TestWrapper>,
    );

    expect(screen.getByText('Deployments')).toBeInTheDocument();
    expect(screen.getByText('Stateful Sets')).toBeInTheDocument();
    expect(screen.getByText('Jobs')).toBeInTheDocument();
  });
});

describe('SidebarPodsAndContainers', () => {
  it('displays pod and container checkboxes', () => {
    const store = createStoreWithSources([
      createMockSource('default', 'nginx-pod', 'nginx'),
      createMockSource('default', 'nginx-pod', 'sidecar'),
    ]);

    render(
      <TestWrapper store={store}>
        <Sidebar />
      </TestWrapper>,
    );

    expect(screen.getByText('nginx-pod')).toBeInTheDocument();
    expect(screen.getByText('nginx')).toBeInTheDocument();
    expect(screen.getByText('sidecar')).toBeInTheDocument();
  });

  it('groups containers by pod', () => {
    const store = createStoreWithSources([
      createMockSource('default', 'pod1', 'container1'),
      createMockSource('default', 'pod1', 'container2'),
      createMockSource('default', 'pod2', 'container3'),
    ]);

    render(
      <TestWrapper store={store}>
        <Sidebar />
      </TestWrapper>,
    );

    expect(screen.getByText('pod1')).toBeInTheDocument();
    expect(screen.getByText('pod2')).toBeInTheDocument();
    expect(screen.getByText('container1')).toBeInTheDocument();
    expect(screen.getByText('container2')).toBeInTheDocument();
    expect(screen.getByText('container3')).toBeInTheDocument();
  });

  it('creates synthetic sources from container URL params', () => {
    render(
      <TestWrapper initialEntries={['/?container=default:synthetic-pod/synthetic-container']}>
        <Sidebar />
      </TestWrapper>,
    );

    expect(screen.getByText('synthetic-pod')).toBeInTheDocument();
    expect(screen.getByText('synthetic-container')).toBeInTheDocument();
  });

  it('sorts containers alphabetically within each pod', () => {
    const store = createStoreWithSources([
      createMockSource('default', 'pod1', 'zebra'),
      createMockSource('default', 'pod1', 'alpha'),
    ]);

    render(
      <TestWrapper store={store}>
        <Sidebar />
      </TestWrapper>,
    );

    const containerElements = screen.getAllByText(/alpha|zebra/);
    expect(containerElements[0]).toHaveTextContent('alpha');
    expect(containerElements[1]).toHaveTextContent('zebra');
  });

  it('renders container checkbox when present in URL', () => {
    const store = createStoreWithSources([createMockSource('default', 'pod1', 'container1')]);

    render(
      <TestWrapper store={store} initialEntries={['/?container=default:pod1/container1']}>
        <Sidebar />
      </TestWrapper>,
    );

    const containerCheckbox = screen.getByRole('checkbox', { name: /container1/i });
    expect(containerCheckbox).toBeInTheDocument();
    expect(containerCheckbox).toBeChecked();
  });

  it('handles pods from different namespaces', () => {
    const store = createStoreWithSources([
      createMockSource('default', 'pod1', 'container1'),
      createMockSource('kube-system', 'pod1', 'container1'),
    ]);

    render(
      <TestWrapper store={store}>
        <Sidebar />
      </TestWrapper>,
    );

    const podElements = screen.getAllByText('pod1');
    expect(podElements).toHaveLength(2);
  });
});

describe('SidebarFacets', () => {
  it('renders facets with counts', () => {
    const regionCounter = new Counter<string>();
    regionCounter.update('us-west-1');
    regionCounter.update('us-west-1');
    regionCounter.update('us-east-1');

    mockUseFacets.mockReturnValue({ ...emptyFacets(), region: regionCounter });

    render(
      <TestWrapper>
        <Sidebar />
      </TestWrapper>,
    );

    expect(screen.getByText('Region')).toBeInTheDocument();
    expect(screen.getByText('us-west-1')).toBeInTheDocument();
    expect(screen.getByText('(2)')).toBeInTheDocument();
    expect(screen.getByText('us-east-1')).toBeInTheDocument();
    expect(screen.getByText('(1)')).toBeInTheDocument();
  });

  it('does not render facets with no entries', () => {
    render(
      <TestWrapper>
        <Sidebar />
      </TestWrapper>,
    );

    expect(screen.queryByText('Region')).not.toBeInTheDocument();
    expect(screen.queryByText('Zone')).not.toBeInTheDocument();
  });

  it('does not render facets with only empty entry', () => {
    const emptyCounter = new Counter<string>();
    emptyCounter.update('');

    mockUseFacets.mockReturnValue({ ...emptyFacets(), region: emptyCounter });

    render(
      <TestWrapper>
        <Sidebar />
      </TestWrapper>,
    );

    expect(screen.queryByText('Region')).not.toBeInTheDocument();
  });

  it('renders facet checkbox when present in URL', () => {
    const regionCounter = new Counter<string>();
    regionCounter.update('us-west-1');

    mockUseFacets.mockReturnValue({ ...emptyFacets(), region: regionCounter });

    render(
      <TestWrapper initialEntries={['/?region=us-west-1']}>
        <Sidebar />
      </TestWrapper>,
    );

    const regionCheckbox = screen.getByRole('checkbox', { name: /us-west-1/i });
    expect(regionCheckbox).toBeInTheDocument();
    expect(regionCheckbox).toBeChecked();
  });

  it('renders all facet categories when they have values', () => {
    const counter = new Counter<string>();
    counter.update('value1');

    mockUseFacets.mockReturnValue({
      region: counter,
      zone: counter,
      os: counter,
      arch: counter,
      node: counter,
    });

    render(
      <TestWrapper>
        <Sidebar />
      </TestWrapper>,
    );

    expect(screen.getByText('Region')).toBeInTheDocument();
    expect(screen.getByText('Zone')).toBeInTheDocument();
    expect(screen.getByText('OS')).toBeInTheDocument();
    expect(screen.getByText('Arch')).toBeInTheDocument();
    expect(screen.getByText('Node')).toBeInTheDocument();
  });

  it('orders facets by count descending', () => {
    const regionCounter = new Counter<string>();
    regionCounter.update('us-west-1');
    regionCounter.update('us-east-1');
    regionCounter.update('us-east-1');
    regionCounter.update('us-east-1');

    mockUseFacets.mockReturnValue({ ...emptyFacets(), region: regionCounter });

    render(
      <TestWrapper>
        <Sidebar />
      </TestWrapper>,
    );

    const facetLabels = screen.getAllByText(/us-(west|east)-1/);
    expect(facetLabels[0]).toHaveTextContent('us-east-1');
    expect(facetLabels[1]).toHaveTextContent('us-west-1');
  });
});

describe('parseSourceArg logic', () => {
  it('parses valid source format', () => {
    const { namespace, workloadType, workloadName } = parseSourceArg('default:deployments/nginx');

    expect(namespace).toBe('default');
    expect(workloadType).toBe('deployments');
    expect(workloadName).toBe('nginx');
  });

  it('handles complex workload names', () => {
    const { namespace, workloadType, workloadName } = parseSourceArg(
      'kube-system:deployments/nginx-deployment-5d59d67564-abcde',
    );

    expect(namespace).toBe('kube-system');
    expect(workloadType).toBe('deployments');
    expect(workloadName).toBe('nginx-deployment-5d59d67564-abcde');
  });

  it('rejects invalid format missing namespace', () => {
    expect(() => parseSourceArg('deployments/nginx')).toThrow();
  });

  it('rejects invalid format missing workload type', () => {
    expect(() => parseSourceArg('default:nginx')).toThrow();
  });
});

describe('generateMapKey logic', () => {
  it('generates consistent keys', () => {
    expect(generateMapKey('default', 'nginx-pod')).toBe('default/nginx-pod');
    expect(generateMapKey('kube-system', 'kube-proxy')).toBe('kube-system/kube-proxy');
  });

  it('handles complex pod names', () => {
    expect(generateMapKey('default', 'nginx-deployment-5d59d67564-abcde')).toBe(
      'default/nginx-deployment-5d59d67564-abcde',
    );
  });
});
