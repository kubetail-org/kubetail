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

// The cards every kind's detail draws: Details, Conditions and Labels, and the rows a kind's
// own Details card shares.
import { Age, timestampMs } from '@/components/dashboard/age';
import { DetailCard } from '@/components/dashboard/detail-card';
import { SeenRow } from '@/components/dashboard/seen-row';
import { VisibleText } from '@/components/primitives/visible-text';
import type { ClusterCachedDataObject } from '@/lib/cluster-cached-data-objects';
import { conditionsOf, controllerOf, labelsOf } from '@/lib/object-detail';
import type { ObjectCondition } from '@/lib/object-detail';
import { visibleTitle } from '@/lib/visible-text';

import { SmallTable } from './small-table';
import type { SmallColumn } from './small-table';
import { TextRow } from './text-row';

export function DetailsCard({ object, namespaced }: { object: ClusterCachedDataObject; namespaced: boolean }) {
  return (
    <DetailCard title="Details">
      <dl className="flex flex-col gap-2 p-3">
        {namespaced && <TextRow label="Namespace" text={object.namespace} />}
        <TextRow label="Controlled by" text={controllerOf(object.rawJSON)} mono />
        <SeenRow label="Created" ms={timestampMs(object.creationTimestamp)} />
      </dl>
    </DetailCard>
  );
}

const text = (value: string | undefined) => <VisibleText text={value || '—'} />;

const CONDITION_COLUMNS: SmallColumn<ObjectCondition>[] = [
  { header: 'Type', className: 'text-foreground', cell: (c) => text(c.type) },
  { header: 'Status', className: 'text-foreground', cell: (c) => text(c.status) },
  { header: 'Reason', className: 'font-mono text-muted-foreground', cell: (c) => text(c.reason) },
  {
    header: 'Changed',
    className: 'text-muted-foreground tabular-nums',
    cell: (c) => <Age ms={timestampMs(c.lastTransitionTime)} className="" />,
  },
];

// Only for a kind that keeps its conditions at `status.conditions`.
export function ConditionsCard({ object }: { object: ClusterCachedDataObject }) {
  const conditions = conditionsOf(object.rawJSON);
  if (!conditions) return null;
  return (
    <DetailCard title="Conditions">
      <SmallTable
        grid="grid-cols-[minmax(0,150px)_70px_minmax(0,190px)_minmax(56px,1fr)]"
        columns={CONDITION_COLUMNS}
        rows={conditions}
        rowKey={(_, i) => String(i)}
      />
    </DetailCard>
  );
}

export function LabelsCard({ object }: { object: ClusterCachedDataObject }) {
  const labels = labelsOf(object.rawJSON);
  return (
    <DetailCard title="Labels" note={labels.length}>
      {labels.length === 0 ? (
        <p className="p-3 text-xs text-muted-foreground">No labels.</p>
      ) : (
        <ul className="flex flex-wrap gap-1.5 p-3">
          {labels.map(([key, value]) => {
            const label = `${key}=${value}`;
            return (
              <li
                key={key}
                className="max-w-full truncate rounded-md bg-muted px-2 py-0.75 font-mono text-[11px]/4.25"
                title={visibleTitle(label)}
              >
                <VisibleText text={label} />
              </li>
            );
          })}
        </ul>
      )}
    </DetailCard>
  );
}
