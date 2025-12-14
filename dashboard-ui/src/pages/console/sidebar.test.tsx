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

import { render, screen } from '@testing-library/react';
import { createMemoryRouter, RouterProvider } from 'react-router-dom';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { Counter } from '@/lib/util';

import { Sidebar, generateMapKey, parseSourceArg } from './sidebar';

// Create mock functions
const mockUseSources = vi.fn();
const mockUseViewerFacets = vi.fn();

// Mock dependencies
vi.mock('@/components/widgets/SourcePickerModal', () => ({
  default: ({ open }: { open: boolean }) => (
    <div data-testid="source-picker-modal" data-open={open}>
      Source Picker Modal
    </div>
  ),
}));

vi.mock('./viewer', () => ({
  useSources: () => mockUseSources(),
  useViewerFacets: () => mockUseViewerFacets(),
}));

// Helper to render sidebar with router
const renderSidebar = (searchParams = '') => {
  const router = createMemoryRouter(
    [
      {
        path: '/',
        element: <Sidebar />,
      },
    ],
    {
      initialEntries: [`/${searchParams}`],
    },
  );

  return render(<RouterProvider router={router} />);
};

describe('Sidebar', () => {
  beforeEach(() => {
    // Reset mocks before each test
    mockUseSources.mockReturnValue({ sources: [] });
    mockUseViewerFacets.mockReturnValue({
      region: new Counter(),
      zone: new Counter(),
      os: new Counter(),
      arch: new Counter(),
      node: new Counter(),
    });
  });

  it('renders the Kubetail logo', () => {
    renderSidebar();
    expect(screen.getByRole('link')).toBeInTheDocument();
  });

  it('displays cluster context when present in URL', () => {
    renderSidebar('?kubeContext=production-cluster');
    expect(screen.getByText(/Cluster:/)).toBeInTheDocument();
    // The cluster name and "Cluster:" are in the same element
    expect(screen.getByText(/production-cluster/)).toBeInTheDocument();
  });

  it('does not display cluster context when not in URL', () => {
    renderSidebar();
    expect(screen.queryByText(/Cluster:/)).not.toBeInTheDocument();
  });

  it('renders the source picker button', () => {
    renderSidebar();
    const button = screen.getByRole('button', { name: /open source picker/i });
    expect(button).toBeInTheDocument();
  });

  it('renders Pods/Containers section', () => {
    renderSidebar();
    expect(screen.getByText('Pods/Containers')).toBeInTheDocument();
  });
});

describe('SidebarWorkloads', () => {
  beforeEach(() => {
    mockUseSources.mockReturnValue({ sources: [] });
    mockUseViewerFacets.mockReturnValue({
      region: new Counter(),
      zone: new Counter(),
      os: new Counter(),
      arch: new Counter(),
      node: new Counter(),
    });
  });

  it('displays workloads from URL sources', () => {
    renderSidebar('?source=default:deployments/nginx&source=default:deployments/app');
    expect(screen.getByText('nginx')).toBeInTheDocument();
    expect(screen.getByText('app')).toBeInTheDocument();
  });

  it('groups workloads by kind', () => {
    renderSidebar('?source=default:deployments/nginx&source=kube-system:daemonsets/kube-proxy');
    expect(screen.getByText('Deployments')).toBeInTheDocument();
    expect(screen.getByText('Daemon Sets')).toBeInTheDocument();
  });

  it('renders delete button for each workload', () => {
    renderSidebar('?source=default:deployments/nginx');
    const deleteButtons = screen.getAllByRole('button', { name: /delete source/i });
    expect(deleteButtons.length).toBeGreaterThan(0);
  });

  it('sorts workloads alphabetically within each kind', () => {
    renderSidebar('?source=default:deployments/zebra&source=default:deployments/alpha');
    const deploymentItems = screen.getAllByText(/alpha|zebra/);
    expect(deploymentItems[0]).toHaveTextContent('alpha');
    expect(deploymentItems[1]).toHaveTextContent('zebra');
  });

  it('handles workload names with wildcards', () => {
    renderSidebar('?source=default:deployments/nginx/*');
    expect(screen.getByText('nginx')).toBeInTheDocument();
  });

  it('handles multiple workload types', () => {
    renderSidebar('?source=default:deployments/app1&source=default:statefulsets/db&source=default:jobs/task');
    expect(screen.getByText('Deployments')).toBeInTheDocument();
    expect(screen.getByText('Stateful Sets')).toBeInTheDocument();
    expect(screen.getByText('Jobs')).toBeInTheDocument();
  });
});

describe('SidebarPodsAndContainers', () => {
  beforeEach(() => {
    mockUseViewerFacets.mockReturnValue({
      region: new Counter(),
      zone: new Counter(),
      os: new Counter(),
      arch: new Counter(),
      node: new Counter(),
    });
  });

  it('displays pod and container checkboxes', () => {
    mockUseSources.mockReturnValue({
      sources: [
        { namespace: 'default', podName: 'nginx-pod', containerName: 'nginx' },
        { namespace: 'default', podName: 'nginx-pod', containerName: 'sidecar' },
      ],
    });

    renderSidebar();
    expect(screen.getByText('nginx-pod')).toBeInTheDocument();
    expect(screen.getByText('nginx')).toBeInTheDocument();
    expect(screen.getByText('sidecar')).toBeInTheDocument();
  });

  it('groups containers by pod', () => {
    mockUseSources.mockReturnValue({
      sources: [
        { namespace: 'default', podName: 'pod1', containerName: 'container1' },
        { namespace: 'default', podName: 'pod1', containerName: 'container2' },
        { namespace: 'default', podName: 'pod2', containerName: 'container3' },
      ],
    });

    renderSidebar();
    expect(screen.getByText('pod1')).toBeInTheDocument();
    expect(screen.getByText('pod2')).toBeInTheDocument();
    expect(screen.getByText('container1')).toBeInTheDocument();
    expect(screen.getByText('container2')).toBeInTheDocument();
    expect(screen.getByText('container3')).toBeInTheDocument();
  });

  it('creates synthetic sources from container URL params', () => {
    mockUseSources.mockReturnValue({ sources: [] });

    renderSidebar('?container=default:synthetic-pod/synthetic-container');
    expect(screen.getByText('synthetic-pod')).toBeInTheDocument();
    expect(screen.getByText('synthetic-container')).toBeInTheDocument();
  });

  it('sorts containers alphabetically within each pod', () => {
    mockUseSources.mockReturnValue({
      sources: [
        { namespace: 'default', podName: 'pod1', containerName: 'zebra' },
        { namespace: 'default', podName: 'pod1', containerName: 'alpha' },
      ],
    });

    renderSidebar();
    const containerElements = screen.getAllByText(/alpha|zebra/);
    expect(containerElements[0]).toHaveTextContent('alpha');
    expect(containerElements[1]).toHaveTextContent('zebra');
  });

  it('renders container checkbox when present in URL', () => {
    mockUseSources.mockReturnValue({
      sources: [{ namespace: 'default', podName: 'pod1', containerName: 'container1' }],
    });

    renderSidebar('?container=default:pod1/container1');

    const containerCheckbox = screen.getByRole('checkbox', { name: /container1/i });
    expect(containerCheckbox).toBeInTheDocument();
    expect(containerCheckbox).toBeChecked();
  });

  it('handles pods from different namespaces', () => {
    mockUseSources.mockReturnValue({
      sources: [
        { namespace: 'default', podName: 'pod1', containerName: 'container1' },
        { namespace: 'kube-system', podName: 'pod1', containerName: 'container1' },
      ],
    });

    renderSidebar();
    // Should render both pods even though they have the same name
    const podElements = screen.getAllByText('pod1');
    expect(podElements).toHaveLength(2);
  });
});

describe('SidebarFacets', () => {
  beforeEach(() => {
    mockUseSources.mockReturnValue({ sources: [] });
  });

  it('renders facets with counts', () => {
    const regionCounter = new Counter<string>();
    regionCounter.update('us-west-1');
    regionCounter.update('us-west-1');
    regionCounter.update('us-east-1');

    mockUseViewerFacets.mockReturnValue({
      region: regionCounter,
      zone: new Counter(),
      os: new Counter(),
      arch: new Counter(),
      node: new Counter(),
    });

    renderSidebar();
    expect(screen.getByText('Region')).toBeInTheDocument();
    expect(screen.getByText('us-west-1')).toBeInTheDocument();
    expect(screen.getByText('(2)')).toBeInTheDocument();
    expect(screen.getByText('us-east-1')).toBeInTheDocument();
    expect(screen.getByText('(1)')).toBeInTheDocument();
  });

  it('does not render facets with no entries', () => {
    mockUseViewerFacets.mockReturnValue({
      region: new Counter(),
      zone: new Counter(),
      os: new Counter(),
      arch: new Counter(),
      node: new Counter(),
    });

    renderSidebar();
    expect(screen.queryByText('Region')).not.toBeInTheDocument();
    expect(screen.queryByText('Zone')).not.toBeInTheDocument();
  });

  it('does not render facets with only empty entry', () => {
    const emptyCounter = new Counter<string>();
    emptyCounter.update('');

    mockUseViewerFacets.mockReturnValue({
      region: emptyCounter,
      zone: new Counter(),
      os: new Counter(),
      arch: new Counter(),
      node: new Counter(),
    });

    renderSidebar();
    expect(screen.queryByText('Region')).not.toBeInTheDocument();
  });

  it('renders facet checkbox when present in URL', () => {
    const regionCounter = new Counter<string>();
    regionCounter.update('us-west-1');

    mockUseViewerFacets.mockReturnValue({
      region: regionCounter,
      zone: new Counter(),
      os: new Counter(),
      arch: new Counter(),
      node: new Counter(),
    });

    renderSidebar('?region=us-west-1');

    const regionCheckbox = screen.getByRole('checkbox', { name: /us-west-1/i });
    expect(regionCheckbox).toBeInTheDocument();
    expect(regionCheckbox).toBeChecked();
  });

  it('renders all facet categories when they have values', () => {
    const counter = new Counter<string>();
    counter.update('value1');

    mockUseViewerFacets.mockReturnValue({
      region: counter,
      zone: counter,
      os: counter,
      arch: counter,
      node: counter,
    });

    renderSidebar();
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

    mockUseViewerFacets.mockReturnValue({
      region: regionCounter,
      zone: new Counter(),
      os: new Counter(),
      arch: new Counter(),
      node: new Counter(),
    });

    renderSidebar();
    const facetLabels = screen.getAllByText(/us-(west|east)-1/);
    // us-east-1 has 3 counts, should appear first
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
