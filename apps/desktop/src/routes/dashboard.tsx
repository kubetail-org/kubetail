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

import { Spinner } from '@kubetail/ui/elements/spinner';
import { createRoute } from '@tanstack/react-router';
import { useCallback, useMemo, useState } from 'react';

import { DashboardBreadcrumbBar } from '@/components/dashboard/breadcrumb-bar';
import { EventsTable } from '@/components/dashboard/events-table';
import { ObjectsTable } from '@/components/dashboard/objects-table';
import { PageHeader } from '@/components/dashboard/page-header';
import { VisibleText } from '@/components/primitives/visible-text';
import { useActiveKubeContext } from '@/lib/active-kube-context';
import { useClusterCachedDataEvents } from '@/lib/cluster-cached-data-events';
import { useClusterCachedDataKinds } from '@/lib/cluster-cached-data-kinds';
import { useClusterCachedDataObjects } from '@/lib/cluster-cached-data-objects';
import type { ClusterCachedDataObject } from '@/lib/cluster-cached-data-objects';
import {
  dashboardResourceLabel,
  mayNameDiscoveredKind,
  resolveDashboardResource,
  serverKindForResource,
} from '@/lib/dashboard-resources';
import type { DashboardResource, ServerKind } from '@/lib/dashboard-resources';
import type { SubscriptionPhase } from '@/lib/graphql/use-folded-subscription';
import { useNamespaceScope } from '@/lib/namespace-scope';
import type { ObjectAction } from '@/lib/object-actions';
import { useObjectView } from '@/lib/object-view';
import { Route as appRoute } from '@/routes/_app';

// The focused kind lives in the `resource` search param, declared on `_app` so it
// survives the switch to chat and back (deep-linkable, each change a history entry —
// see docs/adr/2026-08-09-url-params-as-window-state.md).
export const Route = createRoute({
  getParentRoute: () => appRoute,
  path: '/dashboard',
  component: Dashboard,
});

// The kind a resource last resolved to, under the cache it came from.
type HeldKind = { cacheID: string | undefined; resource: string; kind: ServerKind };

// Each resource is a page under the breadcrumb bar, a note over a panel: `events` → `EventsPage`; a resolved
// discovered kind → `KindPage`; anything else → `PlaceholderPage`. Label and kind resolve
// against the same live catalog the sidebar uses. A window on no cluster, with contexts
// to pick from, draws one note in place of every page.
function Dashboard() {
  const { resource } = Route.useSearch();
  const { noneSelected } = useActiveKubeContext();
  const { kinds, active, cacheID, phase } = useClusterCachedDataKinds();
  const resolved = resolveDashboardResource(resource);
  // The catalog re-emits on count churn — memoize so a Modified frame doesn't rescan it
  // every render.
  const serverKind = useMemo(() => serverKindForResource(kinds, resolved), [kinds, resolved]);

  // A new connection replays the catalog onto a clean slate, so it is empty until the
  // next Bookmark. Holding the last resolved kind through that keeps `ObjectsTable`
  // mounted, with its scroll, sort and selection.
  const [held, setHeld] = useState<HeldKind>();
  if (serverKind && (held?.kind !== serverKind || held.cacheID !== cacheID || held.resource !== resolved)) {
    setHeld({ cacheID, resource: resolved, kind: serverKind });
  }
  const holding = held && active && phase === 'connecting' && held.cacheID === cacheID && held.resource === resolved;
  const kind = serverKind ?? (holding ? held.kind : undefined);
  // From the kind the page shows, so the label and the breadcrumb hold through a replay too.
  // Without one, `kinds` was just searched for the id, so neither searches it again.
  const label = dashboardResourceLabel(kind ? [kind] : [], resolved);

  let page;
  if (noneSelected) {
    page = <Note>No cluster selected. Pick one from the cluster menu at the top of the window.</Note>;
  } else if (resolved === 'events') {
    page = <EventsPage />;
  } else if (kind) {
    page = <KindPage kind={kind} label={label} />;
  } else {
    page = <PlaceholderPage label={label} resource={resolved} active={active} phase={phase} />;
  }
  return (
    <section className="flex min-h-0 min-w-0 flex-1 flex-col">
      <DashboardBreadcrumbBar resource={resolved} label={label} kind={kind} />
      <div className="flex min-h-0 min-w-0 flex-1 flex-col px-4 py-3">{page}</div>
    </section>
  );
}

// The rows the namespace scope shows: all of them without one.
function inScope<T>(rows: T[], namespace: string | undefined, namespaceOf: (row: T) => string): T[] {
  return namespace === undefined ? rows : rows.filter((r) => namespaceOf(r) === namespace);
}

// A kind's page, over its objects watch. The watch is cluster-wide; the scope filters it
// here, a namespaced kind's alone.
function KindPage({ kind, label }: { kind: ServerKind; label: string }) {
  const { namespace } = useNamespaceScope();
  const { objects, active, phase } = useClusterCachedDataObjects({
    apiVersion: kind.apiVersion,
    resource: kind.resource,
  });
  const namespaced = kind.scope === 'Namespaced';
  const scope = namespaced ? namespace : undefined;
  const shown = useMemo(() => inScope(objects, scope, (o) => o.namespace), [objects, scope]);

  const { set: focusObject } = useObjectView();
  const onAction = useCallback(
    (action: ObjectAction, o: ClusterCachedDataObject) => {
      const { uid, namespace: ns, name } = o;
      focusObject(
        { apiVersion: kind.apiVersion, resource: kind.resource, kind: kind.kind, uid, namespace: ns, name },
        action,
      );
    },
    [focusObject, kind],
  );

  return (
    <>
      <PageHeader>
        {!namespaced && namespace !== undefined && (
          <>
            Cluster-scoped, so the <VisibleText text={namespace} /> filter isn&apos;t applied.
          </>
        )}
      </PageHeader>
      {/* Keyed by kind, so a new kind starts unsorted. */}
      <ObjectsTable
        key={`${kind.apiVersion}/${kind.resource}`}
        apiVersion={kind.apiVersion}
        resource={kind.resource}
        kind={kind.kind}
        label={label}
        namespaced={namespaced}
        printerColumns={kind.printerColumns}
        objects={shown}
        active={active}
        phase={phase}
        namespace={scope}
        onAction={onAction}
      />
    </>
  );
}

// The Events page, over the events watch as `KindPage` is over its objects. An event is
// in the namespace of the object it is about.
function EventsPage() {
  const { namespace } = useNamespaceScope();
  const { events, active, phase } = useClusterCachedDataEvents();
  const shown = useMemo(() => inScope(events, namespace, (e) => e.involvedNamespace), [events, namespace]);

  return (
    <>
      <EventsNote count={active && phase !== 'connecting' ? shown.length : undefined} namespace={namespace} />
      <EventsTable events={shown} active={active} phase={phase} namespace={namespace} />
    </>
  );
}

// How many events the scope holds, once the snapshot has landed. The table's Count column
// is each event's repetitions, so this is the page's one total.
function EventsNote({ count, namespace }: { count?: number; namespace?: string }) {
  if (count === undefined && namespace === undefined) return null;
  const noun = count === undefined ? 'Events' : `${count} ${count === 1 ? 'event' : 'events'}`;
  return (
    <PageHeader>
      {noun}
      {namespace !== undefined && (
        <>
          {' in '}
          <VisibleText text={namespace} />
        </>
      )}
      .
    </PageHeader>
  );
}

type PlaceholderPageProps = { label: string; resource: DashboardResource; active: boolean; phase: SubscriptionPhase };

// A page with nothing to list. Overview reads "coming soon" at once. A resource the
// catalog could name (`mayNameDiscoveredKind`) and does not reads "not synced" with no
// active cache, waits behind a spinner for the Bookmark, and after it is not served by
// this cluster.
function PlaceholderPage({ label, resource, active, phase }: PlaceholderPageProps) {
  const name = label.toLowerCase();
  let note;
  if (!mayNameDiscoveredKind(resource)) {
    note = <Note>The {name} view is coming soon.</Note>;
  } else if (!active) {
    note = <Note>No synced cache for this cluster yet — {name} appear once syncing has started.</Note>;
  } else if (phase === 'connecting') {
    note = (
      <div className="flex items-center gap-2 text-sm text-muted-foreground">
        <Spinner size="sm" />
        Loading resources…
      </div>
    );
  } else {
    note = <Note>This cluster does not serve {name}, or your credentials cannot list them.</Note>;
  }
  return note;
}

function Note({ children }: { children: React.ReactNode }) {
  return <p className="text-sm text-muted-foreground">{children}</p>;
}
