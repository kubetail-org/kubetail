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

// What a node's columns and its detail read off its body.

export type NodeBody = {
  metadata?: { labels?: Record<string, string> };
  spec?: { unschedulable?: boolean; taints?: unknown[]; podCIDR?: string };
  status?: {
    conditions?: { type?: string; status?: string }[];
    addresses?: { type?: string; address?: string }[];
    capacity?: NodeResources;
    allocatable?: NodeResources;
    nodeInfo?: {
      kubeletVersion?: string;
      osImage?: string;
      kernelVersion?: string;
      containerRuntimeVersion?: string;
      architecture?: string;
    };
  };
};

type NodeResources = { cpu?: string; memory?: string; pods?: string };

const ROLE_PREFIX = 'node-role.kubernetes.io/';

// kubectl's: every `node-role.kubernetes.io/<role>` label, and `kubernetes.io/role`'s value.
export function nodeRoles(b: NodeBody): string[] {
  const roles = new Set<string>();
  for (const [key, value] of Object.entries(b.metadata?.labels ?? {})) {
    if (key.startsWith(ROLE_PREFIX) && key.length > ROLE_PREFIX.length) roles.add(key.slice(ROLE_PREFIX.length));
    else if (key === 'kubernetes.io/role' && value) roles.add(value);
  }
  return [...roles].toSorted();
}

export function nodeAddress(b: NodeBody, type: string): string | undefined {
  return b.status?.addresses?.find((a) => a.type === type)?.address;
}

export type NodeCapacity = { label: string; value: string };

// CPU, memory and pods, each `allocatable / capacity`, a dash for a side unset.
export function nodeCapacity(b: NodeBody): NodeCapacity[] {
  const { allocatable, capacity } = b.status ?? {};
  const pair = (key: keyof NodeResources) => `${allocatable?.[key] || '—'} / ${capacity?.[key] || '—'}`;
  return [
    { label: 'CPU', value: pair('cpu') },
    { label: 'Memory', value: pair('memory') },
    { label: 'Pods', value: pair('pods') },
  ];
}
