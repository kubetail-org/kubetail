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

import { describe, expect, it } from 'vitest';

import { nodeAddress, nodeCapacity, nodeRoles } from './node-detail';

describe('nodeRoles', () => {
  it('reads each role label, and kubernetes.io/role’s value, sorted and once each', () => {
    const labels = {
      'node-role.kubernetes.io/worker': '',
      'node-role.kubernetes.io/control-plane': '',
      'node-role.kubernetes.io/': '',
      'kubernetes.io/role': 'worker',
      app: 'x',
    };
    expect(nodeRoles({ metadata: { labels } })).toEqual(['control-plane', 'worker']);
  });

  it('reads none without role labels', () => {
    expect(nodeRoles({ metadata: { labels: { 'kubernetes.io/role': '' } } })).toEqual([]);
    expect(nodeRoles({})).toEqual([]);
  });
});

describe('nodeAddress', () => {
  it('reads the address of a type', () => {
    const b = {
      status: {
        addresses: [
          { type: 'Hostname', address: 'pool-a-1' },
          { type: 'InternalIP', address: '10.0.0.4' },
        ],
      },
    };
    expect(nodeAddress(b, 'InternalIP')).toBe('10.0.0.4');
    expect(nodeAddress(b, 'ExternalIP')).toBeUndefined();
  });
});

describe('nodeCapacity', () => {
  it('reads CPU, memory and pods as allocatable over capacity, a dash for a side unset', () => {
    const b = {
      status: {
        allocatable: { cpu: '3800m', memory: '15Gi' },
        capacity: { cpu: '4', memory: '16Gi', pods: '110' },
      },
    };
    expect(nodeCapacity(b)).toEqual([
      { label: 'CPU', value: '3800m / 4' },
      { label: 'Memory', value: '15Gi / 16Gi' },
      { label: 'Pods', value: '— / 110' },
    ]);
    expect(nodeCapacity({})[0]).toEqual({ label: 'CPU', value: '— / —' });
  });
});
