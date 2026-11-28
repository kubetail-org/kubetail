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
import { describe, expect, it, vi } from 'vitest';

import { navPlaceOf } from '@/lib/dashboard-resources';
import type { ServerKind } from '@/lib/dashboard-resources';

// Mocks ---------------------------------------------------------------

// The curated kinds, as before the catalog lands: enough to place a kind.
vi.mock('@/lib/dashboard-nav', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/dashboard-nav')>()),
  useDashboardNavPlace: (id: string, kind?: ServerKind) => navPlaceOf(kind ? [kind] : undefined, id),
}));
// The picker has its own suite, and reads the namespaces through a provider.
vi.mock('./namespace-picker', () => ({ NamespacePicker: () => <button type="button">namespace-picker</button> }));

const { DashboardBreadcrumbBar } = await import('./bar');

// Helpers -------------------------------------------------------------

const crumbs = () => screen.getByRole('navigation', { name: 'Breadcrumb' });
const crumbText = () => Array.from(crumbs().querySelectorAll('li')).map((li) => li.textContent);
const icon = () => crumbs().querySelector('svg');

// Tests ---------------------------------------------------------------

describe('DashboardBreadcrumbBar', () => {
  it('is a 40px bar underlined across the panel, crumbs left and the picker right', () => {
    render(<DashboardBreadcrumbBar resource="overview" label="Overview" />);
    const bar = screen.getByTestId('breadcrumb-bar');
    expect(bar).toHaveClass('h-10', 'shrink-0', 'px-4', 'border-b', 'border-border', 'justify-between');
    expect(Array.from(bar.children)).toEqual([crumbs(), screen.getByRole('button', { name: 'namespace-picker' })]);
  });

  it('names Overview behind its nav icon', () => {
    render(<DashboardBreadcrumbBar resource="overview" label="Overview" />);
    expect(crumbText()).toEqual(['Overview']);
    expect(icon()).toHaveClass('lucide-compass', 'size-4');
  });

  it('names Events behind its list icon', () => {
    render(<DashboardBreadcrumbBar resource="events" label="Events" />);
    expect(crumbText()).toEqual(['Events']);
    expect(icon()).toHaveClass('lucide-list');
  });

  it('names a cluster kind’s group, then the kind, behind a globe', () => {
    render(<DashboardBreadcrumbBar resource="nodes" label="Nodes" />);
    expect(crumbText()).toEqual(['Infrastructure', 'Nodes']);
    expect(icon()).toHaveClass('lucide-globe');
    expect(crumbs().querySelector('.lucide-chevron-right')).toHaveClass('size-3.5', 'text-muted-foreground');
  });

  it('names a namespaced kind’s group, then the kind, behind layers', () => {
    render(<DashboardBreadcrumbBar resource="pods" label="Pods" />);
    expect(crumbText()).toEqual(['Workloads', 'Pods']);
    expect(icon()).toHaveClass('lucide-layers');
  });

  it('names a kind’s section to a screen reader, which skips the icon', () => {
    const { rerender } = render(<DashboardBreadcrumbBar resource="nodes" label="Nodes" />);
    expect(crumbs().querySelector('.sr-only')).toHaveTextContent('Cluster');
    rerender(<DashboardBreadcrumbBar resource="pods" label="Pods" />);
    expect(crumbs().querySelector('.sr-only')).toHaveTextContent('Namespace');
    rerender(<DashboardBreadcrumbBar resource="overview" label="Overview" />);
    expect(crumbs().querySelector('.sr-only')).toBeNull();
  });

  it('mutes every crumb but the last', () => {
    render(<DashboardBreadcrumbBar resource="pods" label="Pods" />);
    const [group, kind] = crumbs().querySelectorAll('li');
    expect(group).toHaveClass('text-muted-foreground');
    expect(kind).toHaveClass('text-foreground');
    expect(kind).toHaveAttribute('aria-current', 'page');
  });

  it('makes the current crumb the page’s one heading', () => {
    render(<DashboardBreadcrumbBar resource="pods" label="Pods" />);
    expect(screen.getAllByRole('heading')).toEqual([screen.getByRole('heading', { level: 1, name: 'Pods' })]);
  });

  // The resource comes from the URL, so it can name an object's own properties.
  it('draws no icon for a resource named after an object property', () => {
    render(<DashboardBreadcrumbBar resource="__proto__" label="__proto__" />);
    expect(crumbText()).toEqual(['__proto__']);
    expect(icon()).toBeNull();
  });

  it('places the kind it is handed, as the page holds it through a replay', () => {
    const widget: ServerKind = {
      apiVersion: 'example.com/v1',
      kind: 'Widget',
      resource: 'widgets',
      scope: 'Cluster',
      isCRD: true,
      count: 1,
      printerColumns: [],
    };
    render(<DashboardBreadcrumbBar resource="example.com/widgets" label="Widget" kind={widget} />);
    expect(crumbText()).toEqual(['Custom resources', 'Widget']);
    expect(icon()).toHaveClass('lucide-globe');
  });

  it('names a resource the tree does not place by its label alone', () => {
    render(<DashboardBreadcrumbBar resource="example.com/widgets" label="Widget" />);
    expect(crumbText()).toEqual(['Widget']);
    expect(icon()).toBeNull();
  });

  // Keyed by text, the two crumbs would collide and React could leave a stale one.
  it('keeps each crumb in its slot when the kind shares its group’s label', () => {
    const error = vi.spyOn(console, 'error').mockImplementation(() => {});
    const { rerender } = render(<DashboardBreadcrumbBar resource="pods" label="Workloads" />);
    expect(crumbText()).toEqual(['Workloads', 'Workloads']);
    rerender(<DashboardBreadcrumbBar resource="deployments" label="Deployments" />);
    expect(crumbText()).toEqual(['Workloads', 'Deployments']);
    expect(error).not.toHaveBeenCalled();
    error.mockRestore();
  });

  // A discovered kind's label is cluster data.
  it('draws labels through VisibleText', () => {
    const label = 'Wid\u200bget';
    render(<DashboardBreadcrumbBar resource="example.com/widgets" label={label} />);
    expect(crumbText()).toEqual(['Wid\\u{200B}get']);
  });

  describe('with an object open', () => {
    it('names its namespace and name after the kind, as the current page', () => {
      render(
        <DashboardBreadcrumbBar resource="pods" label="Pods" object={{ namespace: 'default', name: 'worker-r7tnv' }} />,
      );
      expect(crumbText()).toEqual(['Workloads', 'Pods', 'default/worker-r7tnv']);
      const items = crumbs().querySelectorAll('li');
      expect(items[1]).not.toHaveAttribute('aria-current');
      expect(items[2]).toHaveAttribute('aria-current', 'page');
      expect(items[2].querySelector('.lucide-chevron-right')).toBeInTheDocument();
      expect(screen.getByText('default')).toHaveClass('text-muted-foreground');
      expect(screen.getByText('/')).toHaveClass('text-muted-foreground');
      // The kind stays the page's heading.
      expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent('Pods');
    });

    it('names a cluster-scoped object by its name alone', () => {
      render(<DashboardBreadcrumbBar resource="nodes" label="Nodes" object={{ namespace: '', name: 'pool-a-1' }} />);
      expect(crumbText()).toEqual(['Infrastructure', 'Nodes', 'pool-a-1']);
      expect(screen.queryByText('/')).not.toBeInTheDocument();
    });

    it('lets a long namespace and name both truncate, each whole on hover', () => {
      render(<DashboardBreadcrumbBar resource="pods" label="Pods" object={{ namespace: 'team-a', name: 'worker' }} />);
      expect(screen.getByText('team-a')).toHaveClass('min-w-0', 'truncate');
      expect(screen.getByText('team-a')).toHaveAttribute('title', 'team-a');
      expect(screen.getByText('worker')).toHaveClass('min-w-0', 'truncate');
      expect(screen.getByText('worker')).toHaveAttribute('title', 'worker');
    });

    it('spells invisible characters in the namespace and name', () => {
      render(
        <DashboardBreadcrumbBar resource="pods" label="Pods" object={{ namespace: 'a\u200bb', name: 'c\u200bd' }} />,
      );
      expect(crumbText()[2]).toBe('a\\u{200B}b/c\\u{200B}d');
      const titles = Array.from(crumbs().querySelectorAll('li:last-child span[title]')).map((e) =>
        e.getAttribute('title'),
      );
      expect(titles).toEqual(['a\\u{200B}b', 'c\\u{200B}d']);
    });

    it('leaves the kind the current page without one', () => {
      render(<DashboardBreadcrumbBar resource="pods" label="Pods" />);
      const items = crumbs().querySelectorAll('li');
      expect(items).toHaveLength(2);
      expect(items[1]).toHaveAttribute('aria-current', 'page');
    });
  });
});
