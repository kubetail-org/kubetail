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

// A pod's own cards: its Details, Containers and Resources.
import { Age, timestampMs } from '@/components/dashboard/age';
import { DetailCard } from '@/components/dashboard/detail-card';
import { DetailRow } from '@/components/dashboard/detail-row';
import { SeenRow } from '@/components/dashboard/seen-row';
import { StatusPill } from '@/components/dashboard/status-pill';
import { VisibleText } from '@/components/primitives/visible-text';
import type { ClusterCachedDataObject } from '@/lib/cluster-cached-data-objects';
import { controllerOf } from '@/lib/object-detail';
import { podContainers, podLastTermination, podReady, podResources, podRestarts } from '@/lib/pod-detail';
import type { ContainerResources, PodContainer } from '@/lib/pod-detail';
import type { PodBody } from '@/lib/pod-status';
import { visibleTitle } from '@/lib/visible-text';

import { SmallTable } from './small-table';
import type { SmallColumn } from './small-table';
import { TextRow } from './text-row';

const podBody = (o: ClusterCachedDataObject) => (o.rawJSON ?? {}) as PodBody;

function restartsClass(n: number | undefined) {
  return n !== undefined && n > 0 ? 'text-destructive' : undefined;
}

function LastTermination({ body }: { body: PodBody }) {
  const t = podLastTermination(body);
  if (!t) return <DetailRow label="Last termination">—</DetailRow>;
  const head = [t.reason ?? '', t.exitCode === undefined ? '' : `exit ${t.exitCode}`].filter(Boolean).join(' · ');
  return (
    <DetailRow label="Last termination" title={visibleTitle(head)}>
      {head && (
        <>
          <VisibleText text={head} />
          {' · '}
        </>
      )}
      <Age ms={timestampMs(t.finishedAt)} className="" /> ago
    </DetailRow>
  );
}

export function PodDetailsCard({ object }: { object: ClusterCachedDataObject }) {
  const b = podBody(object);
  const ready = podReady(b);
  const restarts = podRestarts(b);
  return (
    <DetailCard title="Details">
      <dl className="flex flex-col gap-2 p-3">
        <TextRow label="Namespace" text={object.namespace} />
        <TextRow label="Controlled by" text={controllerOf(object.rawJSON)} mono />
        <TextRow label="Node" text={b.spec?.nodeName} mono />
        <TextRow label="Pod IP" text={b.status?.podIP} mono />
        <SeenRow label="Created" ms={timestampMs(object.creationTimestamp)} />
        <DetailRow label="Ready">{ready ? `${ready.ready}/${ready.total}` : '—'}</DetailRow>
        <DetailRow label="Restarts" className={restartsClass(restarts)}>
          {restarts ?? '—'}
        </DetailRow>
        <LastTermination body={b} />
        <TextRow label="Service account" text={b.spec?.serviceAccountName} mono />
        <TextRow label="Priority class" text={b.spec?.priorityClassName} />
      </dl>
    </DetailCard>
  );
}

const CONTAINER_COLUMNS: SmallColumn<PodContainer>[] = [
  {
    header: 'Name',
    className: 'font-mono text-foreground',
    cell: (c) => (
      <span title={visibleTitle(c.name)}>
        <VisibleText text={c.name || '—'} />
        {c.init && <span className="font-sans text-muted-foreground"> init</span>}
      </span>
    ),
  },
  {
    header: 'Image',
    className: 'font-mono text-muted-foreground',
    cell: (c) => (
      <span title={c.image ? visibleTitle(c.image) : undefined}>
        <VisibleText text={c.image || '—'} />
      </span>
    ),
  },
  { header: 'State', cell: (c) => (c.state ? <StatusPill label={c.state.label} tone={c.state.tone} /> : '—') },
  {
    header: 'Restarts',
    className: 'tabular-nums',
    cell: (c) => <span className={restartsClass(c.restarts)}>{c.restarts ?? '—'}</span>,
  },
];

export function ContainersCard({ object }: { object: ClusterCachedDataObject }) {
  const containers = podContainers(podBody(object));
  return (
    <DetailCard title="Containers" note={containers.length}>
      <SmallTable
        grid="grid-cols-[minmax(0,90px)_minmax(80px,1fr)_minmax(0,150px)_70px]"
        columns={CONTAINER_COLUMNS}
        rows={containers}
        rowKey={(c, i) => `${c.init ? 'init' : 'main'}:${i}`}
      />
    </DetailCard>
  );
}

const text = (value: string) => <VisibleText text={value} />;

const RESOURCE_COLUMNS: SmallColumn<ContainerResources>[] = [
  { header: 'Container', className: 'font-mono text-foreground', cell: (c) => text(c.name || '—') },
  { header: 'CPU request / limit', className: 'font-mono text-foreground', cell: (c) => text(c.cpu) },
  { header: 'Memory request / limit', className: 'font-mono text-foreground', cell: (c) => text(c.memory) },
];

export function ResourcesCard({ object }: { object: ClusterCachedDataObject }) {
  const b = podBody(object);
  const qos = b.status?.qosClass;
  return (
    <DetailCard title="Resources" note={qos ? <VisibleText text={`QoS ${qos}`} /> : undefined}>
      <SmallTable
        grid="grid-cols-[minmax(0,90px)_minmax(0,1fr)_minmax(0,1fr)]"
        columns={RESOURCE_COLUMNS}
        rows={podResources(b)}
        rowKey={(_, i) => String(i)}
      />
    </DetailCard>
  );
}
