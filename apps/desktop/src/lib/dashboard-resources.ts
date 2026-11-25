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

// The dashboard's resource tree: two sections by scope, each holding groups of kinds.
// A curated kind keeps its place; a discovered kind joins a group by its API group.
// See docs/adr/2026-11-12-the-dashboard-nav-groups-kinds-by-scope-and-api-group.md

type Scope = 'Cluster' | 'Namespaced';

type GroupId =
  | 'workloads'
  | 'infrastructure'
  | 'network'
  | 'config'
  | 'storage'
  | 'access-control'
  | 'extensions'
  | 'custom-resources';

type CuratedKind = { resource: string; label: string; apiGroup: string; scope: Scope };

const kind = (resource: string, label: string, apiGroup: string, scope: Scope): CuratedKind => ({
  resource,
  label,
  apiGroup,
  scope,
});

// Each section draws its groups, and each group its curated kinds, in this order.
const GROUPS: readonly { id: GroupId; label: string; kinds: readonly CuratedKind[] }[] = [
  {
    id: 'workloads',
    label: 'Workloads',
    kinds: [
      kind('pods', 'Pods', '', 'Namespaced'),
      kind('deployments', 'Deployments', 'apps', 'Namespaced'),
      kind('statefulsets', 'StatefulSets', 'apps', 'Namespaced'),
      kind('daemonsets', 'DaemonSets', 'apps', 'Namespaced'),
      kind('replicasets', 'ReplicaSets', 'apps', 'Namespaced'),
      kind('jobs', 'Jobs', 'batch', 'Namespaced'),
      kind('cronjobs', 'CronJobs', 'batch', 'Namespaced'),
    ],
  },
  {
    id: 'infrastructure',
    label: 'Infrastructure',
    kinds: [kind('nodes', 'Nodes', '', 'Cluster'), kind('namespaces', 'Namespaces', '', 'Cluster')],
  },
  {
    id: 'network',
    label: 'Network',
    kinds: [
      kind('services', 'Services', '', 'Namespaced'),
      kind('ingresses', 'Ingresses', 'networking.k8s.io', 'Namespaced'),
      kind('networkpolicies', 'NetworkPolicies', 'networking.k8s.io', 'Namespaced'),
    ],
  },
  {
    id: 'config',
    label: 'Config',
    kinds: [kind('configmaps', 'ConfigMaps', '', 'Namespaced'), kind('secrets', 'Secrets', '', 'Namespaced')],
  },
  {
    id: 'storage',
    label: 'Storage',
    kinds: [
      kind('persistentvolumes', 'PersistentVolumes', '', 'Cluster'),
      kind('storageclasses', 'StorageClasses', 'storage.k8s.io', 'Cluster'),
      kind('persistentvolumeclaims', 'PersistentVolumeClaims', '', 'Namespaced'),
    ],
  },
  {
    id: 'access-control',
    label: 'Access control',
    kinds: [
      kind('serviceaccounts', 'ServiceAccounts', '', 'Namespaced'),
      kind('roles', 'Roles', 'rbac.authorization.k8s.io', 'Namespaced'),
      kind('rolebindings', 'RoleBindings', 'rbac.authorization.k8s.io', 'Namespaced'),
      kind('clusterroles', 'ClusterRoles', 'rbac.authorization.k8s.io', 'Cluster'),
      kind('clusterrolebindings', 'ClusterRoleBindings', 'rbac.authorization.k8s.io', 'Cluster'),
    ],
  },
  {
    id: 'extensions',
    label: 'Extensions',
    kinds: [
      kind('customresourcedefinitions', 'CustomResourceDefinitions', 'apiextensions.k8s.io', 'Cluster'),
      kind('apiservices', 'APIServices', 'apiregistration.k8s.io', 'Cluster'),
    ],
  },
  { id: 'custom-resources', label: 'Custom resources', kinds: [] },
];

// The group of a kind not curated, by its API group. A kind in an unlisted API group is a
// custom resource when a CRD backs it, else an extension. Both tables are maps, since the
// cluster names the key and a plain object would answer `constructor`.
const API_GROUP_TO_GROUP = new Map<string, GroupId>([
  ['apps', 'workloads'],
  ['batch', 'workloads'],
  ['autoscaling', 'workloads'],
  ['policy', 'workloads'],
  ['node.k8s.io', 'infrastructure'],
  ['scheduling.k8s.io', 'infrastructure'],
  ['coordination.k8s.io', 'infrastructure'],
  ['flowcontrol.apiserver.k8s.io', 'infrastructure'],
  ['resource.k8s.io', 'infrastructure'],
  ['networking.k8s.io', 'network'],
  ['discovery.k8s.io', 'network'],
  ['gateway.networking.k8s.io', 'network'],
  ['storage.k8s.io', 'storage'],
  ['rbac.authorization.k8s.io', 'access-control'],
  ['certificates.k8s.io', 'access-control'],
  ['apiextensions.k8s.io', 'extensions'],
  ['apiregistration.k8s.io', 'extensions'],
  ['admissionregistration.k8s.io', 'extensions'],
]);

// The core group spans every group, so its kinds not curated map by plural.
const CORE_KIND_TO_GROUP = new Map<string, GroupId>([
  ['replicationcontrollers', 'workloads'],
  ['podtemplates', 'workloads'],
  ['componentstatuses', 'infrastructure'],
  ['endpoints', 'network'],
  ['resourcequotas', 'config'],
  ['limitranges', 'config'],
]);

// Namespaced kinds first: they are the ones most visited, and the ones the namespace
// picker scopes.
const SECTIONS = [
  { id: 'namespace', label: 'Namespace', scope: 'Namespaced' },
  { id: 'cluster', label: 'Cluster', scope: 'Cluster' },
] as const;

// A curated or core kind's id is its plural; any other's is `<group>/<resource>`.
export type DashboardResource = string;

export type NavKind = { readonly id: DashboardResource; readonly label: string; readonly count?: number };
export type NavGroup = { readonly id: string; readonly label: string; readonly kinds: readonly NavKind[] };
export type NavSection = {
  readonly id: (typeof SECTIONS)[number]['id'];
  readonly label: string;
  readonly groups: readonly NavGroup[];
};
export type DashboardNav = { readonly eventsCount?: number; readonly sections: readonly NavSection[] };

// Kinds and printer columns are declared here rather than imported from `@/gql`, so this
// module and the ones rendering from it stay free of codegen output.

// One of a CRD's declared columns: a header and where to read its value from.
export type PrinterColumn = {
  name: string;
  type: string;
  jsonPath: string;
  priority: number;
};

// A row of the kinds catalog.
export type ServerKind = {
  apiVersion: string;
  kind: string;
  resource: string;
  scope: string;
  isCRD: boolean;
  count: number;
  // Empty for a built-in. Carried here because it is static per kind; the objects watch
  // carries none.
  printerColumns: readonly PrinterColumn[];
};

// The API group of a "group/version"; "" for the core group's bare "v1".
export function apiGroupOf(apiVersion: string): string {
  const slash = apiVersion.indexOf('/');
  return slash === -1 ? '' : apiVersion.slice(0, slash);
}

const CURATED = GROUPS.flatMap((g) => g.kinds.map((k) => ({ ...k, group: g.id })));

// Keyed by API group and plural, so a CRD reusing a built-in's plural in another group
// never takes its row.
const CURATED_INDEX = new Map(CURATED.map((c, i) => [`${c.apiGroup}/${c.resource}`, i]));

function curatedIndex(k: ServerKind): number | undefined {
  return CURATED_INDEX.get(`${apiGroupOf(k.apiVersion)}/${k.resource}`);
}

// Events get the pinned row, not a place in a section.
function isEvents(k: ServerKind): boolean {
  const group = apiGroupOf(k.apiVersion);
  return k.resource === 'events' && (group === '' || group === 'events.k8s.io');
}

function navIdOf(k: ServerKind): DashboardResource {
  const group = apiGroupOf(k.apiVersion);
  return group === '' || curatedIndex(k) !== undefined ? k.resource : `${group}/${k.resource}`;
}

// A kind with where it goes. `order` is a curated kind's place; discovered kinds share
// one past the last, then sort by label.
type Placed = NavKind & { group: GroupId; scope: Scope; order: number };

function placeCurated(i: number, count?: number): Placed {
  const c = CURATED[i];
  return { id: c.resource, label: c.label, count, group: c.group, scope: c.scope, order: i };
}

function place(k: ServerKind): Placed {
  const i = curatedIndex(k);
  if (i !== undefined) return placeCurated(i, k.count);

  const apiGroup = apiGroupOf(k.apiVersion);
  const group = apiGroup === '' ? CORE_KIND_TO_GROUP.get(k.resource) : API_GROUP_TO_GROUP.get(apiGroup);
  return {
    id: navIdOf(k),
    label: k.kind,
    count: k.count,
    group: group ?? (k.isCRD ? 'custom-resources' : 'extensions'),
    scope: k.scope === 'Cluster' ? 'Cluster' : 'Namespaced',
    order: CURATED.length,
  };
}

// `kinds` undefined means the catalog has not landed: the curated kinds, without counts.
export function buildDashboardNav(kinds: readonly ServerKind[] | undefined): DashboardNav {
  const placed =
    kinds === undefined ? CURATED.map((_, i) => placeCurated(i)) : kinds.filter((k) => !isEvents(k)).map(place);
  placed.sort((a, b) => a.order - b.order || a.label.localeCompare(b.label));

  return {
    // `events.k8s.io` serves the same objects as the core group; count them once.
    eventsCount: kinds?.find((k) => isEvents(k) && apiGroupOf(k.apiVersion) === '')?.count,
    sections: SECTIONS.map((s) => ({
      id: s.id,
      label: s.label,
      groups: GROUPS.map((g) => ({
        id: g.id,
        label: g.label,
        kinds: placed
          .filter((k) => k.scope === s.scope && k.group === g.id)
          .map(({ id, label, count }) => ({ id, label, count })),
      })).filter((g) => g.kinds.length > 0),
    })),
  };
}

// The rows above the sections.
export const PINNED = [
  { id: 'overview', label: 'Overview' },
  { id: 'events', label: 'Events' },
] as const;

// Shown when the URL names no resource.
export const DEFAULT_DASHBOARD_RESOURCE: DashboardResource = 'overview';

// Any id passes through, even one naming a kind the catalog has not delivered yet.
export function resolveDashboardResource(resource: DashboardResource | undefined): DashboardResource {
  return resource || DEFAULT_DASHBOARD_RESOURCE;
}

// Whether an id could name a kind once the catalog lands. Only Overview never will.
export function mayNameDiscoveredKind(id: DashboardResource): boolean {
  return id !== DEFAULT_DASHBOARD_RESOURCE;
}

// The catalog row behind a tree id.
export function serverKindForResource(kinds: readonly ServerKind[], id: DashboardResource): ServerKind | undefined {
  return kinds.find((k) => navIdOf(k) === id);
}

const KNOWN_LABELS = new Map<DashboardResource, string>([
  ...PINNED.map((p) => [p.id, p.label] as const),
  ...CURATED.map((c) => [c.resource, c.label] as const),
]);

// A pinned or curated row's label even before the catalog lands, else the catalog's kind,
// else the id itself.
export function dashboardResourceLabel(kinds: readonly ServerKind[], id: DashboardResource): string {
  return KNOWN_LABELS.get(id) ?? serverKindForResource(kinds, id)?.kind ?? id;
}

// A kind's place in the nav: its section, with its label, and its group's label.
export type NavPlace = { section: NavSection['id']; sectionLabel: string; group: string };

// Where a kind sits in the tree `buildDashboardNav(kinds)` builds. Undefined for a pinned
// page or a kind the tree does not hold. It places the one kind, so a caller needing no
// more than this need not build the tree.
export function navPlaceOf(kinds: readonly ServerKind[] | undefined, id: DashboardResource): NavPlace | undefined {
  if (PINNED.some((p) => p.id === id)) return undefined;
  let placed: Placed | undefined;
  if (kinds === undefined) {
    const i = CURATED.findIndex((c) => c.resource === id);
    if (i >= 0) placed = placeCurated(i);
  } else {
    const k = serverKindForResource(kinds, id);
    if (k && !isEvents(k)) placed = place(k);
  }
  if (!placed) return undefined;
  const { scope, group } = placed;
  const section = SECTIONS.find((s) => s.scope === scope)!;
  return { section: section.id, sectionLabel: section.label, group: GROUPS.find((g) => g.id === group)!.label };
}

function kindsOf(sections: readonly NavSection[]): NavKind[] {
  return sections.flatMap((s) => s.groups.flatMap((g) => g.kinds));
}

type NavView = {
  readonly sections: readonly NavSection[];
  // How many kinds are shown, out of every kind in the tree.
  readonly matches: number;
  readonly total: number;
};

// A query keeps the kinds whose label starts with it, empty ones included. Without one, a
// kind with no objects hides unless it is active; an unknown count is never empty.
export function filterNav(nav: DashboardNav, { active, query }: { active: DashboardResource; query: string }): NavView {
  const q = query.trim().toLowerCase();
  const keep = (k: NavKind) => (q ? k.label.toLowerCase().startsWith(q) : k.count !== 0 || k.id === active);

  const sections = nav.sections.map((s) => ({
    ...s,
    groups: s.groups.map((g) => ({ ...g, kinds: g.kinds.filter(keep) })).filter((g) => g.kinds.length > 0),
  }));
  return { sections, matches: kindsOf(sections).length, total: kindsOf(nav.sections).length };
}

// The sum of a group's known counts; undefined when none is known.
export function groupCount(group: NavGroup): number | undefined {
  let total: number | undefined;
  for (const k of group.kinds) {
    if (k.count !== undefined) total = (total ?? 0) + k.count;
  }
  return total;
}
