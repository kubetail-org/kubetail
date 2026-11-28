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

// A node's own cards: its Details and Capacity.
import { timestampMs } from '@/components/dashboard/age';
import { DetailCard } from '@/components/dashboard/detail-card';
import { DetailRow } from '@/components/dashboard/detail-row';
import { SeenRow } from '@/components/dashboard/seen-row';
import { VisibleText } from '@/components/primitives/visible-text';
import type { ClusterCachedDataObject } from '@/lib/cluster-cached-data-objects';
import { nodeAddress, nodeCapacity, nodeRoles } from '@/lib/node-detail';
import type { NodeBody } from '@/lib/node-detail';

import { TextRow } from './text-row';

const nodeBody = (o: ClusterCachedDataObject) => (o.rawJSON ?? {}) as NodeBody;

export function NodeDetailsCard({ object }: { object: ClusterCachedDataObject }) {
  const b = nodeBody(object);
  const info = b.status?.nodeInfo;
  return (
    <DetailCard title="Details">
      <dl className="flex flex-col gap-2 p-3">
        <TextRow label="Roles" text={nodeRoles(b).join(', ')} />
        <TextRow label="Internal IP" text={nodeAddress(b, 'InternalIP')} mono />
        <TextRow label="Kubelet version" text={info?.kubeletVersion} />
        <TextRow label="OS image" text={info?.osImage} />
        <TextRow label="Container runtime" text={info?.containerRuntimeVersion} />
        <TextRow label="Architecture" text={info?.architecture} />
        <TextRow label="Pod CIDR" text={b.spec?.podCIDR} mono />
        <SeenRow label="Created" ms={timestampMs(object.creationTimestamp)} />
      </dl>
    </DetailCard>
  );
}

export function CapacityCard({ object }: { object: ClusterCachedDataObject }) {
  return (
    <DetailCard title="Capacity" note="allocatable / capacity">
      <dl className="flex flex-col gap-2 p-3">
        {nodeCapacity(nodeBody(object)).map(({ label, value }) => (
          <DetailRow key={label} label={label} className="font-mono text-foreground">
            <VisibleText text={value} />
          </DetailRow>
        ))}
      </dl>
    </DetailCard>
  );
}
