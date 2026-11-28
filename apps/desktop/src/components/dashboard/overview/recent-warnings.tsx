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

// The last hour's warnings, newest first, and the way to the Events page.
import { Elapsed } from '@/components/primitives/elapsed';
import { VisibleText } from '@/components/primitives/visible-text';
import type { ClusterCachedDataEvent } from '@/lib/cluster-cached-data-events';
import { TONE } from '@/lib/cluster-health';
import { visibleTitle } from '@/lib/visible-text';

import { OverviewCard } from './card';
import type { CardColumn } from './card';

// "Pod shop/web"; a cluster-scoped object has no namespace to name.
function involvedRef(e: ClusterCachedDataEvent): string {
  const path = e.involvedNamespace ? `${e.involvedNamespace}/${e.involvedName}` : e.involvedName;
  return `${e.involvedKind} ${path}`;
}

const COLUMNS: CardColumn<ClusterCachedDataEvent>[] = [
  {
    key: 'reason',
    header: 'Reason',
    cell: ({ reason }) => (
      <span className={TONE.error.text} title={visibleTitle(reason)}>
        <VisibleText text={reason} />
      </span>
    ),
  },
  {
    key: 'object',
    header: 'Object',
    cell: (e) => (
      <span className="font-mono text-primary" title={visibleTitle(involvedRef(e))}>
        <VisibleText text={e.involvedName} />
      </span>
    ),
  },
  {
    key: 'lastSeen',
    header: 'Last seen',
    cell: (e) => (e.lastSeen ? <Elapsed since={Date.parse(e.lastSeen)} /> : '—'),
  },
];

type RecentWarningsProps = { warnings?: readonly ClusterCachedDataEvent[]; syncing?: boolean };

export function RecentWarnings({ warnings, syncing = false }: RecentWarningsProps) {
  return (
    <OverviewCard
      title="Recent warnings"
      count={`Last hour · ${warnings?.length ?? 0}`}
      grid="grid-cols-[96px_minmax(0,1fr)_60px]"
      columns={COLUMNS}
      rows={warnings}
      rowKey={(e) => e.uid}
      empty={syncing ? 'No warnings found yet' : 'No warnings in the last hour'}
      link={{ label: 'Open Events', resource: 'events', always: true }}
    />
  );
}
