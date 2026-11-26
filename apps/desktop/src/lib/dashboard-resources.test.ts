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

import { describe, expect, it } from 'vitest';

import {
  apiGroupOf,
  buildDashboardNav,
  dashboardResourceLabel,
  DEFAULT_DASHBOARD_RESOURCE,
  filterNav,
  groupCount,
  mayNameDiscoveredKind,
  navPlaceOf,
  resolveDashboardResource,
  serverKindForResource,
} from './dashboard-resources';
import type { DashboardNav, ServerKind } from './dashboard-resources';

// Each section's groups, each as `label: kind ids`.
function shape(nav: DashboardNav) {
  return nav.sections.map((s) => ({
    section: s.id,
    groups: s.groups.map((g) => `${g.label}: ${g.kinds.map((k) => k.id).join(' ')}`),
  }));
}

describe('buildDashboardNav before the catalog lands', () => {
  it('draws the curated kinds by section and group, in order, without counts', () => {
    const nav = buildDashboardNav(undefined);
    expect(shape(nav)).toEqual([
      {
        section: 'namespace',
        groups: [
          'Workloads: pods deployments statefulsets daemonsets replicasets jobs cronjobs',
          'Network: services ingresses networkpolicies',
          'Config: configmaps secrets',
          'Storage: persistentvolumeclaims',
          'Access control: serviceaccounts roles rolebindings',
        ],
      },
      {
        section: 'cluster',
        groups: [
          'Infrastructure: nodes namespaces',
          'Storage: persistentvolumes storageclasses',
          'Access control: clusterroles clusterrolebindings',
          'Extensions: customresourcedefinitions apiservices',
        ],
      },
    ]);
    expect(nav.sections.flatMap((s) => s.groups.flatMap((g) => g.kinds)).some((k) => k.count !== undefined)).toBe(
      false,
    );
    expect(nav.eventsCount).toBeUndefined();
  });
});

const served = (
  apiVersion: string,
  k: string,
  resource: string,
  count = 1,
  extra: Partial<ServerKind> = {},
): ServerKind => ({
  apiVersion,
  kind: k,
  resource,
  scope: 'Namespaced',
  isCRD: false,
  count,
  printerColumns: [],
  ...extra,
});

// Where a kind landed, as `section/group`, or undefined when it is not in the tree.
function placeOf(nav: DashboardNav, id: string): string | undefined {
  for (const s of nav.sections) {
    for (const g of s.groups) {
      if (g.kinds.some((k) => k.id === id)) return `${s.id}/${g.id}`;
    }
  }
  return undefined;
}

function kindOf(nav: DashboardNav, id: string) {
  return nav.sections.flatMap((s) => s.groups.flatMap((g) => g.kinds)).find((k) => k.id === id);
}

describe('buildDashboardNav once the catalog lands', () => {
  it('lists only the kinds the catalog serves, with their counts', () => {
    const nav = buildDashboardNav([served('v1', 'Pod', 'pods', 42), served('apps/v1', 'Deployment', 'deployments', 0)]);
    expect(shape(nav)).toEqual([
      { section: 'namespace', groups: ['Workloads: pods deployments'] },
      { section: 'cluster', groups: [] },
    ]);
    expect(kindOf(nav, 'pods')).toEqual({ id: 'pods', label: 'Pods', count: 42 });
    expect(kindOf(nav, 'deployments')?.count).toBe(0);
  });

  it('does not let a CRD reusing a curated plural in another group take its row', () => {
    const nav = buildDashboardNav([
      served('v1', 'Pod', 'pods', 42),
      served('example.com/v1', 'Pod', 'pods', 999, { isCRD: true }),
    ]);
    expect(kindOf(nav, 'pods')?.count).toBe(42);
    expect(kindOf(nav, 'example.com/pods')?.count).toBe(999);
  });
});

describe('where a discovered kind lands', () => {
  it.each([
    // API group → group; the kind's scope picks the section.
    ['apps/v1', 'controllerrevisions', 'Namespaced', false, 'namespace/workloads'],
    ['autoscaling/v2', 'horizontalpodautoscalers', 'Namespaced', false, 'namespace/workloads'],
    ['policy/v1', 'poddisruptionbudgets', 'Namespaced', false, 'namespace/workloads'],
    ['node.k8s.io/v1', 'runtimeclasses', 'Cluster', false, 'cluster/infrastructure'],
    ['scheduling.k8s.io/v1', 'priorityclasses', 'Cluster', false, 'cluster/infrastructure'],
    ['coordination.k8s.io/v1', 'leases', 'Namespaced', false, 'namespace/infrastructure'],
    ['flowcontrol.apiserver.k8s.io/v1', 'flowschemas', 'Cluster', false, 'cluster/infrastructure'],
    ['resource.k8s.io/v1', 'deviceclasses', 'Cluster', false, 'cluster/infrastructure'],
    ['networking.k8s.io/v1', 'ingressclasses', 'Cluster', false, 'cluster/network'],
    ['discovery.k8s.io/v1', 'endpointslices', 'Namespaced', false, 'namespace/network'],
    ['storage.k8s.io/v1', 'csidrivers', 'Cluster', false, 'cluster/storage'],
    ['storage.k8s.io/v1', 'csistoragecapacities', 'Namespaced', false, 'namespace/storage'],
    ['certificates.k8s.io/v1', 'certificatesigningrequests', 'Cluster', false, 'cluster/access-control'],
    ['admissionregistration.k8s.io/v1', 'validatingwebhookconfigurations', 'Cluster', false, 'cluster/extensions'],
    // A CRD in a mapped API group stays in that group.
    ['gateway.networking.k8s.io/v1', 'gateways', 'Namespaced', true, 'namespace/network'],
    // Any other CRD is a custom resource; any other non-CRD kind an extension.
    ['example.com/v1', 'widgets', 'Namespaced', true, 'namespace/custom-resources'],
    ['example.com/v1', 'clusterwidgets', 'Cluster', true, 'cluster/custom-resources'],
    ['metrics.k8s.io/v1beta1', 'nodes', 'Cluster', false, 'cluster/extensions'],
  ])('%s %s (%s, CRD %s) → %s', (apiVersion, resource, scope, isCRD, where) => {
    const nav = buildDashboardNav([served(apiVersion, 'Kind', resource, 1, { scope, isCRD })]);
    expect(placeOf(nav, `${apiVersion.split('/')[0]}/${resource}`)).toBe(where);
  });

  it.each([
    ['replicationcontrollers', 'Namespaced', 'namespace/workloads'],
    ['podtemplates', 'Namespaced', 'namespace/workloads'],
    ['componentstatuses', 'Cluster', 'cluster/infrastructure'],
    ['endpoints', 'Namespaced', 'namespace/network'],
    ['resourcequotas', 'Namespaced', 'namespace/config'],
    ['limitranges', 'Namespaced', 'namespace/config'],
  ])('core %s (%s) lands by its plural, its id the plural alone → %s', (resource, scope, where) => {
    const nav = buildDashboardNav([served('v1', 'Kind', resource, 1, { scope })]);
    expect(placeOf(nav, resource)).toBe(where);
  });

  it('keeps Events out of the sections and gives the core count to the pinned row', () => {
    const nav = buildDashboardNav([
      served('v1', 'Event', 'events', 128),
      served('events.k8s.io/v1', 'Event', 'events', 128),
    ]);
    expect(nav.sections.every((s) => s.groups.length === 0)).toBe(true);
    expect(nav.eventsCount).toBe(128);
  });
});

describe('order within a group', () => {
  it('draws curated kinds in their order, then discovered kinds by label', () => {
    const nav = buildDashboardNav([
      served('policy/v1', 'PodDisruptionBudget', 'poddisruptionbudgets'),
      served('batch/v1', 'Job', 'jobs'),
      served('autoscaling/v2', 'HorizontalPodAutoscaler', 'horizontalpodautoscalers'),
      served('v1', 'Pod', 'pods'),
    ]);
    expect(shape(nav)[0].groups).toEqual([
      'Workloads: pods jobs autoscaling/horizontalpodautoscalers policy/poddisruptionbudgets',
    ]);
  });
});

describe('apiGroupOf', () => {
  it('drops the version, and the core group is empty', () => {
    expect(apiGroupOf('apps/v1')).toBe('apps');
    expect(apiGroupOf('v1')).toBe('');
  });
});

describe('resolveDashboardResource', () => {
  it('falls back to Overview when absent or empty, and passes any other id through', () => {
    expect(DEFAULT_DASHBOARD_RESOURCE).toBe('overview');
    expect(resolveDashboardResource(undefined)).toBe('overview');
    expect(resolveDashboardResource('')).toBe('overview');
    expect(resolveDashboardResource('apps/controllerrevisions')).toBe('apps/controllerrevisions');
  });
});

describe('mayNameDiscoveredKind', () => {
  it('is false for Overview alone, which no kind backs', () => {
    expect(mayNameDiscoveredKind('overview')).toBe(false);
    expect(mayNameDiscoveredKind('pods')).toBe(true);
    expect(mayNameDiscoveredKind('workloads')).toBe(true);
  });
});

describe('serverKindForResource', () => {
  const kinds = [
    served('v1', 'Pod', 'pods'),
    served('v1', 'ResourceQuota', 'resourcequotas'),
    served('apps/v1', 'ControllerRevision', 'controllerrevisions'),
  ];

  it('finds a kind by the id the tree gives it', () => {
    expect(serverKindForResource(kinds, 'pods')?.kind).toBe('Pod');
    expect(serverKindForResource(kinds, 'resourcequotas')?.kind).toBe('ResourceQuota');
    expect(serverKindForResource(kinds, 'apps/controllerrevisions')?.kind).toBe('ControllerRevision');
    expect(serverKindForResource(kinds, 'overview')).toBeUndefined();
  });
});

describe('dashboardResourceLabel', () => {
  it('names a pinned row and a curated kind with or without the catalog, and a discovered kind by its kind', () => {
    const kinds = [served('apps/v1', 'ControllerRevision', 'controllerrevisions')];
    expect(dashboardResourceLabel([], 'overview')).toBe('Overview');
    expect(dashboardResourceLabel([], 'events')).toBe('Events');
    expect(dashboardResourceLabel([], 'pods')).toBe('Pods');
    expect(dashboardResourceLabel(kinds, 'apps/controllerrevisions')).toBe('ControllerRevision');
  });

  it('falls back to the id for a kind the catalog lacks', () => {
    expect(dashboardResourceLabel([], 'example.com/widgets')).toBe('example.com/widgets');
  });
});

describe('filterNav without a query', () => {
  const nav = buildDashboardNav([
    served('v1', 'Pod', 'pods', 3),
    served('apps/v1', 'Deployment', 'deployments', 0),
    served('v1', 'ConfigMap', 'configmaps', 0),
    served('v1', 'Node', 'nodes', 2, { scope: 'Cluster' }),
  ]);
  const ids = (sections: ReturnType<typeof filterNav>['sections']) =>
    sections.map((s) => s.groups.map((g) => `${g.id}: ${g.kinds.map((k) => k.id).join(' ')}`));

  it('hides kinds with no objects, and a group left with none', () => {
    const view = filterNav(nav, { active: 'overview', query: '' });
    expect(ids(view.sections)).toEqual([['workloads: pods'], ['infrastructure: nodes']]);
  });

  it('keeps the active kind even while it is empty', () => {
    const view = filterNav(nav, { active: 'deployments', query: '' });
    expect(ids(view.sections)[0]).toEqual(['workloads: pods deployments']);
  });

  it('hides nothing while counts are unknown', () => {
    const view = filterNav(buildDashboardNav(undefined), { active: 'overview', query: '' });
    expect(view.sections).toEqual(buildDashboardNav(undefined).sections);
  });
});

describe('filterNav with a query', () => {
  const nav = buildDashboardNav([
    served('v1', 'Pod', 'pods', 3),
    served('policy/v1', 'PodDisruptionBudget', 'poddisruptionbudgets', 0),
    served('apps/v1', 'Deployment', 'deployments', 5),
    served('v1', 'Node', 'nodes', 2, { scope: 'Cluster' }),
  ]);

  it('keeps kinds whose label starts with it, any case, empty ones included', () => {
    const view = filterNav(nav, { active: 'overview', query: ' POD' });
    expect(view.sections.map((s) => s.groups.flatMap((g) => g.kinds.map((k) => k.id)))).toEqual([
      ['pods', 'policy/poddisruptionbudgets'],
      [],
    ]);
    expect(view.matches).toBe(2);
    expect(view.total).toBe(4);
  });

  it('matches the start of the label, not the middle', () => {
    const view = filterNav(nav, { active: 'overview', query: 'ode' });
    expect(view.matches).toBe(0);
  });
});

describe('groupCount', () => {
  it('sums the known counts of a group, and is undefined when none is known', () => {
    expect(
      groupCount({
        id: 'g',
        label: 'G',
        kinds: [
          { id: 'a', label: 'A', count: 2 },
          { id: 'b', label: 'B', count: 5 },
        ],
      }),
    ).toBe(7);
    expect(groupCount({ id: 'g', label: 'G', kinds: [{ id: 'a', label: 'A' }] })).toBeUndefined();
  });
});

describe('navPlaceOf', () => {
  it('names a kind’s section and group', () => {
    const kinds = [
      served('v1', 'Node', 'nodes', 1, { scope: 'Cluster' }),
      served('apps/v1', 'Deployment', 'deployments'),
    ];
    expect(navPlaceOf(kinds, 'nodes')).toEqual({
      section: 'cluster',
      sectionLabel: 'Cluster',
      group: 'Infrastructure',
    });
    expect(navPlaceOf(kinds, 'deployments')).toEqual({
      section: 'namespace',
      sectionLabel: 'Namespace',
      group: 'Workloads',
    });
  });

  it('places a kind where the tree does', () => {
    const kinds = [
      served('v1', 'Pod', 'pods'),
      served('example.com/v1', 'Widget', 'widgets', 1, { isCRD: true }),
      served('example.com/v1', 'Gadget', 'gadgets', 1, { scope: 'Cluster' }),
    ];
    const nav = buildDashboardNav(kinds);
    for (const s of nav.sections) {
      for (const g of s.groups) {
        for (const k of g.kinds)
          expect(navPlaceOf(kinds, k.id)).toEqual({ section: s.id, sectionLabel: s.label, group: g.label });
      }
    }
  });

  // The cluster names the API group, so it can name an object's own properties.
  it('places a kind whose API group is named after an object property by its fallback', () => {
    const kinds = [
      served('constructor/v1', 'Widget', 'widgets', 1, { isCRD: true }),
      served('toString/v1', 'Gadget', 'gadgets'),
    ];
    expect(navPlaceOf(kinds, 'constructor/widgets')?.group).toBe('Custom resources');
    expect(navPlaceOf(kinds, 'toString/gadgets')?.group).toBe('Extensions');
  });

  it('places a curated kind before the catalog lands', () => {
    expect(navPlaceOf(undefined, 'pods')).toEqual({
      section: 'namespace',
      sectionLabel: 'Namespace',
      group: 'Workloads',
    });
  });

  it('places nothing for a pinned page or a kind the tree does not hold', () => {
    for (const kinds of [undefined, [served('v1', 'Event', 'events'), served('events.k8s.io/v1', 'Event', 'events')]]) {
      expect(navPlaceOf(kinds, 'overview')).toBeUndefined();
      expect(navPlaceOf(kinds, 'events')).toBeUndefined();
      expect(navPlaceOf(kinds, 'example.com/widgets')).toBeUndefined();
    }
  });

  it('never reads the catalog for a pinned page', () => {
    const unread = new Proxy([] as ServerKind[], {
      get: () => {
        throw new Error('read the catalog');
      },
    });
    expect(navPlaceOf(unread, 'overview')).toBeUndefined();
    expect(navPlaceOf(unread, 'events')).toBeUndefined();
  });
});
