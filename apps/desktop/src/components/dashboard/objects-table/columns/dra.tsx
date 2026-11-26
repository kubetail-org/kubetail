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

// Dynamic resource allocation's kinds (resource.k8s.io/v1): the devices drivers publish, the
// classes that select them, and the claims pods make on them.
import { countColumn, kind, listColumn, statusColumn, textColumn } from './common';
import type { KindColumn, Status } from './common';

// A request asks for exactly one kind of device, or names alternatives that each do.
type DeviceRequest = {
  exactly?: { deviceClassName?: string };
  firstAvailable?: { deviceClassName?: string }[];
};

type ClaimSpec = { devices?: { requests?: DeviceRequest[] } };

function deviceClasses(spec: ClaimSpec | undefined): string[] | undefined {
  return spec?.devices?.requests?.flatMap((r) =>
    r.exactly ? [r.exactly.deviceClassName ?? ''] : (r.firstAvailable ?? []).map((a) => a.deviceClassName ?? ''),
  );
}

type ResourceClaimBody = {
  metadata?: { deletionTimestamp?: string };
  spec?: ClaimSpec;
  status?: {
    allocation?: { devices?: { results?: { device?: string }[] } };
    reservedFor?: unknown[];
  };
};

// kubectl's State: what holds of allocated, reserved and deleted, else pending.
function claimState(b: ResourceClaimBody): Status {
  const states = [
    ...(b.status?.allocation ? ['allocated'] : []),
    ...(b.status?.reservedFor?.length ? ['reserved'] : []),
    ...(b.metadata?.deletionTimestamp ? ['deleted'] : []),
  ];
  if (states.length === 0) return { label: 'pending', tone: 'pending' };
  return { label: states.join(','), tone: b.status?.allocation && !b.metadata?.deletionTimestamp ? 'ok' : 'pending' };
}

const resourceClaimColumns: KindColumn<ResourceClaimBody>[] = [
  statusColumn('State', 160, claimState),
  listColumn('Device classes', 176, (b) => deviceClasses(b.spec), { hidden: true }),
  listColumn('Devices', 224, (b) => b.status?.allocation?.devices?.results?.map((r) => r.device), {
    hidden: true,
    mono: true,
  }),
  countColumn('Consumers', 104, (b) => b.status?.reservedFor?.length ?? 0, { hidden: true }),
];

type ResourceClaimTemplateBody = { spec?: { spec?: ClaimSpec } };

const resourceClaimTemplateColumns: KindColumn<ResourceClaimTemplateBody>[] = [
  listColumn('Device classes', 176, (b) => deviceClasses(b.spec?.spec)),
  countColumn('Requests', 80, (b) => b.spec?.spec?.devices?.requests?.length ?? 0),
];

type ResourceSliceBody = {
  spec?: {
    nodeName?: string;
    allNodes?: boolean;
    driver?: string;
    pool?: { name?: string; generation?: number };
    devices?: unknown[];
  };
};

const resourceSliceColumns: KindColumn<ResourceSliceBody>[] = [
  // A slice serves one node, every node, or those a selector picks.
  textColumn('Node', 144, (b) => b.spec?.nodeName ?? (b.spec?.allNodes ? '<all>' : undefined), {
    mono: true,
  }),
  textColumn('Driver', 192, (b) => b.spec?.driver, { mono: true }),
  textColumn('Pool', 160, (b) => b.spec?.pool?.name),
  countColumn('Devices', 80, (b) => b.spec?.devices?.length ?? 0),
  countColumn('Generation', 96, (b) => b.spec?.pool?.generation, { hidden: true }),
];

type DeviceClassBody = { spec?: { selectors?: unknown[]; config?: unknown[] } };

const deviceClassColumns: KindColumn<DeviceClassBody>[] = [
  countColumn('Selectors', 88, (b) => b.spec?.selectors?.length ?? 0),
  countColumn('Config', 72, (b) => b.spec?.config?.length ?? 0, { hidden: true }),
];

const RESOURCE = 'resource.k8s.io/v1';

export const draKinds = [
  kind(RESOURCE, 'resourceclaims', resourceClaimColumns),
  kind(RESOURCE, 'resourceclaimtemplates', resourceClaimTemplateColumns),
  kind(RESOURCE, 'resourceslices', resourceSliceColumns),
  kind(RESOURCE, 'deviceclasses', deviceClassColumns),
];
