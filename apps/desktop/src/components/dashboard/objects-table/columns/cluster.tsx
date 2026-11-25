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

// The cluster's own kinds: nodes, namespaces, and the scheduling, lease and API priority
// kinds behind them.
import {
  ageColumn,
  conditionStatus,
  countColumn,
  durationColumn,
  kind,
  labelsText,
  listColumn,
  quantityColumn,
  statusColumn,
  textColumn,
} from './common';
import type { Status } from './common';

type Conditions = { type?: string; status?: string; message?: string; error?: string }[];

type NodeBody = {
  metadata?: { labels?: Record<string, string> };
  spec?: { unschedulable?: boolean; taints?: unknown[] };
  status?: {
    conditions?: Conditions;
    addresses?: { type?: string; address?: string }[];
    capacity?: { cpu?: string; memory?: string; pods?: string };
    nodeInfo?: { kubeletVersion?: string; osImage?: string; kernelVersion?: string; containerRuntimeVersion?: string };
  };
};

// kubectl's: the Ready condition, then SchedulingDisabled for a cordoned node.
function nodeStatus(b: NodeBody): Status {
  const ready = conditionStatus(b.status?.conditions, 'Ready');
  const cordoned = Boolean(b.spec?.unschedulable);
  const suffix = cordoned ? ',SchedulingDisabled' : '';
  if (ready === 'True') return { label: `Ready${suffix}`, tone: cordoned ? 'pending' : 'ok' };
  return { label: `${ready === 'False' ? 'NotReady' : 'Unknown'}${suffix}`, tone: 'failing' };
}

const ROLE_PREFIX = 'node-role.kubernetes.io/';

// kubectl's: every `node-role.kubernetes.io/<role>` label, and `kubernetes.io/role`'s value.
function nodeRoles(b: NodeBody): string[] {
  const roles = new Set<string>();
  for (const [key, value] of Object.entries(b.metadata?.labels ?? {})) {
    if (key.startsWith(ROLE_PREFIX) && key.length > ROLE_PREFIX.length) roles.add(key.slice(ROLE_PREFIX.length));
    else if (key === 'kubernetes.io/role' && value) roles.add(value);
  }
  return [...roles].toSorted();
}

const address = (b: NodeBody, type: string) => b.status?.addresses?.find((a) => a.type === type)?.address;

type NamespaceBody = { status?: { phase?: string } };

function namespaceStatus(b: NamespaceBody): Status | undefined {
  const phase = b.status?.phase;
  return phase ? { label: phase, tone: phase === 'Active' ? 'ok' : 'pending' } : undefined;
}

type PriorityClassBody = { value?: number; globalDefault?: boolean; preemptionPolicy?: string; description?: string };

type RuntimeClassBody = { handler?: string; scheduling?: { nodeSelector?: Record<string, string> } };

type LeaseBody = {
  spec?: { holderIdentity?: string; leaseDurationSeconds?: number; renewTime?: string; leaseTransitions?: number };
};

type ComponentStatusBody = { conditions?: Conditions };

const healthy = (b: ComponentStatusBody) => b.conditions?.find((c) => c.type === 'Healthy');

function componentStatus(b: ComponentStatusBody): Status | undefined {
  const status = healthy(b)?.status;
  if (!status) return undefined;
  return status === 'True' ? { label: 'Healthy', tone: 'ok' } : { label: 'Unhealthy', tone: 'failing' };
}

type FlowSchemaBody = {
  spec?: {
    priorityLevelConfiguration?: { name?: string };
    matchingPrecedence?: number;
    distinguisherMethod?: { type?: string };
  };
  status?: { conditions?: Conditions };
};

type PriorityLevelBody = {
  spec?: {
    type?: string;
    limited?: {
      nominalConcurrencyShares?: number;
      limitResponse?: { queuing?: { queues?: number; handSize?: number; queueLengthLimit?: number } };
    };
  };
};

const queuing = (b: PriorityLevelBody) => b.spec?.limited?.limitResponse?.queuing;

export const clusterKinds = [
  kind<NodeBody>('v1', 'nodes', [
    statusColumn('Status', 200, nodeStatus),
    listColumn('Roles', 128, nodeRoles),
    textColumn('Version', 112, (b) => b.status?.nodeInfo?.kubeletVersion),
    textColumn('Internal IP', 120, (b) => address(b, 'InternalIP'), { hidden: true, mono: true }),
    textColumn('External IP', 120, (b) => address(b, 'ExternalIP'), { hidden: true, mono: true }),
    textColumn('OS image', 192, (b) => b.status?.nodeInfo?.osImage, { hidden: true }),
    textColumn('Kernel', 160, (b) => b.status?.nodeInfo?.kernelVersion, { hidden: true }),
    textColumn('Container runtime', 176, (b) => b.status?.nodeInfo?.containerRuntimeVersion, {
      hidden: true,
    }),
    // Capacity, not allocatable: what the machine has, as kubectl describe leads with.
    quantityColumn('CPU capacity', 112, (b) => b.status?.capacity?.cpu, { hidden: true }),
    quantityColumn('Memory capacity', 136, (b) => b.status?.capacity?.memory, { hidden: true }),
    quantityColumn('Pod limit', 88, (b) => b.status?.capacity?.pods, { hidden: true }),
    countColumn('Taints', 72, (b) => b.spec?.taints?.length ?? 0, { hidden: true }),
  ]),
  kind('v1', 'namespaces', [statusColumn('Status', 136, namespaceStatus)]),
  kind<PriorityClassBody>('scheduling.k8s.io/v1', 'priorityclasses', [
    countColumn('Value', 112, (b) => b.value),
    textColumn('Global default', 120, (b) => String(b.globalDefault ?? false)),
    textColumn('Preemption policy', 160, (b) => b.preemptionPolicy),
    textColumn('Description', 240, (b) => b.description, { hidden: true }),
  ]),
  kind<RuntimeClassBody>('node.k8s.io/v1', 'runtimeclasses', [
    textColumn('Handler', 128, (b) => b.handler),
    textColumn('Node selector', 224, (b) => labelsText(b.scheduling?.nodeSelector), {
      hidden: true,
      mono: true,
    }),
  ]),
  kind<LeaseBody>('coordination.k8s.io/v1', 'leases', [
    textColumn('Holder', 224, (b) => b.spec?.holderIdentity),
    ageColumn('Renewed', 88, (b) => b.spec?.renewTime, { hidden: true }),
    durationColumn('Duration', 88, (b) => b.spec?.leaseDurationSeconds, { hidden: true }),
    countColumn('Transitions', 104, (b) => b.spec?.leaseTransitions, { hidden: true }),
  ]),
  kind<ComponentStatusBody>('v1', 'componentstatuses', [
    statusColumn('Status', 128, componentStatus),
    textColumn('Message', 224, (b) => healthy(b)?.message),
    textColumn('Error', 224, (b) => healthy(b)?.error),
  ]),
  kind<FlowSchemaBody>('flowcontrol.apiserver.k8s.io/v1', 'flowschemas', [
    textColumn('Priority level', 176, (b) => b.spec?.priorityLevelConfiguration?.name),
    countColumn('Precedence', 104, (b) => b.spec?.matchingPrecedence),
    textColumn('Distinguisher', 120, (b) => b.spec?.distinguisherMethod?.type),
    // kubectl's MISSINGPL: whether the priority level it names is missing.
    textColumn('Missing PL', 96, (b) => conditionStatus(b.status?.conditions, 'Dangling')),
  ]),
  kind<PriorityLevelBody>('flowcontrol.apiserver.k8s.io/v1', 'prioritylevelconfigurations', [
    textColumn('Type', 96, (b) => b.spec?.type),
    countColumn('Shares', 80, (b) => b.spec?.limited?.nominalConcurrencyShares),
    countColumn('Queues', 80, (b) => queuing(b)?.queues),
    countColumn('Hand size', 88, (b) => queuing(b)?.handSize),
    countColumn('Queue length', 112, (b) => queuing(b)?.queueLengthLimit),
  ]),
];
