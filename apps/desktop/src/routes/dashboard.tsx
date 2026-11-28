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
import { OverviewPage } from '@/components/dashboard/overview';
import { PageHeader } from '@/components/dashboard/page-header';
import { VisibleText } from '@/components/primitives/visible-text';
import { useActiveKubeContext } from '@/lib/active-kube-context';
import { useClusterCachedDataEvents } from '@/lib/cluster-cached-data-events';
import { useClusterCachedDataKinds } from '@/lib/cluster-cached-data-kinds';
import { useClusterCachedDataObjects } from '@/lib/cluster-cached-data-objects';
import type { ClusterCachedDataObject } from '@/lib/cluster-cached-data-objects';
import {
  DEFAULT_DASHBOARD_RESOURCE,
  dashboardResourceLabel,
  resolveDashboardResource,
  serverKindForResource,
} from '@/lib/dashboard-resources';
import type { ServerKind } from '@/lib/dashboard-resources';
import type { SubscriptionPhase } from '@/lib/graphql/use-folded-subscription';
import { useLastSettled } from '@/lib/graphql/use-last-settled';
import { gvrKey } from '@/lib/gvr';
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

// The object open in a kind page's detail, under the page and namespace scope it was opened
// in. Its namespace and name are for the breadcrumb, which lies outside the page.
type OpenObject = { scope: string; uid: string; namespace: string; name: string };

// Each resource is a page under the breadcrumb bar, a note over a panel: `overview` →
// `OverviewPage`; `events` → `EventsPage`; a resolved discovered kind → `KindPage`; anything
// else → `MissingKindPage`. Label and kind resolve against the same live catalog the
// sidebar uses. A window on no cluster, with contexts to pick from, draws one note in
// place of every page.
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

  // The open object is a selection inside the page, as the focused object is, so it stays out
  // of the URL and Back leaves the page rather than stepping through the objects viewed. A
  // new kind, cache or namespace scope closes it.
  const { namespace } = useNamespaceScope();
  const pageKey = kind ? `${cacheID}/${gvrKey(kind)}` : undefined;
  const openScope = `${pageKey ?? ''}|${namespace ?? ''}`;
  const [open, setOpen] = useState<OpenObject>();
  if (open && open.scope !== openScope) setOpen(undefined);
  const onOpen = useCallback(
    (o: ClusterCachedDataObject | undefined) =>
      setOpen(o && { scope: openScope, uid: o.uid, namespace: o.namespace, name: o.name }),
    [openScope],
  );

  let page;
  if (noneSelected) {
    page = <Note>No cluster selected. Pick one from the cluster menu at the top of the window.</Note>;
  } else if (resolved === DEFAULT_DASHBOARD_RESOURCE) {
    page = <OverviewPage />;
  } else if (resolved === 'events') {
    // Keyed by cache, so a cluster switch closes the open event.
    page = <EventsPage key={cacheID} />;
  } else if (kind) {
    // Keyed by cache and kind, so a new one starts unsorted, at the top, with its own search.
    page = <KindPage key={pageKey} kind={kind} label={label} openUid={open?.uid} onOpen={onOpen} />;
  } else {
    page = <MissingKindPage label={label} active={active} phase={phase} />;
  }
  return (
    <section className="flex min-h-0 min-w-0 flex-1 flex-col">
      <DashboardBreadcrumbBar resource={resolved} label={label} kind={kind} object={noneSelected ? undefined : open} />
      <div className="flex min-h-0 min-w-0 flex-1 flex-col px-4 py-3">{page}</div>
    </section>
  );
}

// The rows the namespace scope shows: all of them without one.
function inScope<T>(rows: T[], namespace: string | undefined, namespaceOf: (row: T) => string): T[] {
  return namespace === undefined ? rows : rows.filter((r) => namespaceOf(r) === namespace);
}

type KindPageProps = {
  kind: ServerKind;
  label: string;
  openUid: string | undefined;
  onOpen: (object: ClusterCachedDataObject | undefined) => void;
};

// A kind's page, over its objects watch. The watch is cluster-wide; the scope filters it
// here, a namespaced kind's alone.
function KindPage({ kind, label, openUid, onOpen }: KindPageProps) {
  const { namespace } = useNamespaceScope();
  const watch = useClusterCachedDataObjects({ apiVersion: kind.apiVersion, resource: kind.resource });
  // A reconnect replays the watch onto a clean slate. The rows and phase as they last stood
  // whole hold through it, so the table, and an open object's detail, stay mounted.
  const settled = watch.phase !== 'connecting';
  const objects = useLastSettled(watch.objects, settled);
  const phase = useLastSettled(watch.phase, settled);
  const { active } = watch;
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
  // The press names the object by uid; the breadcrumb, outside the page, needs its name too.
  const onOpenUid = useCallback((uid: string | undefined) => onOpen(shown.find((o) => o.uid === uid)), [onOpen, shown]);

  return (
    <>
      <PageHeader>
        {!namespaced && namespace !== undefined && (
          <>
            Cluster-scoped, so the <VisibleText text={namespace} /> filter isn&apos;t applied.
          </>
        )}
      </PageHeader>
      <ObjectsTable
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
        openUid={openUid}
        onOpen={onOpenUid}
      />
    </>
  );
}

// The Events page, over the events watch as `KindPage` is over its objects. An event is
// in the namespace of the object it is about.
function EventsPage() {
  const { namespace } = useNamespaceScope();
  const watch = useClusterCachedDataEvents();
  // A reconnect replays the watch onto a clean slate. The rows and phase as they last stood
  // whole hold through it, so the table, and an open event's detail, stay mounted.
  const settled = watch.phase !== 'connecting';
  const events = useLastSettled(watch.events, settled);
  const phase = useLastSettled(watch.phase, settled);
  const shown = useMemo(() => inScope(events, namespace, (e) => e.involvedNamespace), [events, namespace]);

  // The open event is a selection inside the page, as the focused object is, so it stays out
  // of the URL and Back leaves the page rather than stepping through the events viewed. A new
  // namespace scope closes it.
  const [openUid, setOpenUid] = useState<string>();
  const [openScope, setOpenScope] = useState(namespace);
  if (openScope !== namespace) {
    setOpenScope(namespace);
    setOpenUid(undefined);
  }

  return (
    <EventsTable
      events={shown}
      active={watch.active}
      phase={phase}
      namespace={namespace}
      openUid={openUid}
      onOpen={setOpenUid}
    />
  );
}

type MissingKindPageProps = { label: string; active: boolean; phase: SubscriptionPhase };

// A resource the catalog does not name: "not synced" with no active cache, a spinner until
// the Bookmark, and after it not served by this cluster.
function MissingKindPage({ label, active, phase }: MissingKindPageProps) {
  const name = label.toLowerCase();
  if (!active) return <Note>No synced cache for this cluster yet — {name} appear once syncing has started.</Note>;
  if (phase === 'connecting') {
    return (
      <div className="flex items-center gap-2 text-sm text-muted-foreground">
        <Spinner size="sm" />
        Loading resources…
      </div>
    );
  }
  return <Note>This cluster does not serve {name}, or your credentials cannot list them.</Note>;
}

function Note({ children }: { children: React.ReactNode }) {
  return <p className="text-sm text-muted-foreground">{children}</p>;
}
