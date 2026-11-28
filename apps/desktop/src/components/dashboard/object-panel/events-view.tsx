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

// The object panel's Events tab: the Kubernetes Events about the focused object, newest
// first, live. An event names its object by kind, namespace and name alone, so an earlier
// object of the same name shares its events.
import { Spinner } from '@kubetail/ui/elements/spinner';
import { useMemo } from 'react';
import ReactTimeAgo from 'react-timeago';

import { VisibleText } from '@/components/primitives/visible-text';
import { useClusterCachedDataEvents } from '@/lib/cluster-cached-data-events';
import type { ClusterCachedDataEvent } from '@/lib/cluster-cached-data-events';
import type { FocusedObject } from '@/lib/object-view';

import { ObjectPanelNote } from './note';

// `type` is an open Kubernetes string — a cluster may omit it or supply a custom
// value, so everything but Warning stays muted (empty renders "Unknown").
function TypeBadge({ type }: { type: ClusterCachedDataEvent['type'] }) {
  const warning = type === 'Warning';
  return (
    <span
      className={`inline-block rounded px-1.5 py-0.5 text-xs font-medium ${
        warning ? 'bg-amber-100 text-amber-800 dark:bg-amber-950 dark:text-amber-300' : 'bg-muted text-muted-foreground'
      }`}
    >
      {type || 'Unknown'}
    </span>
  );
}

export function ObjectEventsView({ object }: { object: FocusedObject }) {
  // Its own connection, as the YAML tab's: the Events page may hold the same watch.
  const { events, active, phase } = useClusterCachedDataEvents({ unshared: true });
  const about = useMemo(
    () =>
      events.filter(
        (e) =>
          e.involvedKind === object.kind && e.involvedNamespace === object.namespace && e.involvedName === object.name,
      ),
    [events, object],
  );

  if (!active) return <ObjectPanelNote>No synced cache for this cluster.</ObjectPanelNote>;
  if (phase === 'connecting') {
    return (
      <ObjectPanelNote>
        <Spinner size="sm" />
        Loading…
      </ObjectPanelNote>
    );
  }
  if (about.length === 0) return <ObjectPanelNote>No events.</ObjectPanelNote>;
  return (
    <ul className="min-h-0 flex-1 divide-y overflow-auto">
      {about.map((e) => {
        const lastSeen = e.lastSeen ? Date.parse(e.lastSeen) : Number.NaN;
        return (
          <li key={e.uid} className="flex flex-col gap-1 px-4 py-2.5 text-xs">
            <div className="flex items-center gap-2">
              <TypeBadge type={e.type} />
              <span className="min-w-0 flex-1 truncate font-medium">
                <VisibleText text={e.reason || '—'} />
              </span>
              <span className="shrink-0 text-muted-foreground tabular-nums">
                {e.count > 1 && `×${e.count} · `}
                {Number.isNaN(lastSeen) ? '—' : <ReactTimeAgo date={lastSeen} component="span" maxPeriod={60} />}
              </span>
            </div>
            {e.message && (
              <p className="wrap-break-word text-muted-foreground">
                <VisibleText text={e.message} />
              </p>
            )}
          </li>
        );
      })}
    </ul>
  );
}
