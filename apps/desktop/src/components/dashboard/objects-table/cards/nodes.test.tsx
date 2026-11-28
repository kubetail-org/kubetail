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

import { render, screen, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';

import { CapacityCard, NodeDetailsCard } from './nodes';
import { objectWith } from './testutil';

const card = (title: string) => screen.getByRole('heading', { level: 3, name: title }).closest('section')!;
const value = (title: string, label: string) => within(card(title)).getByText(label).nextElementSibling as HTMLElement;

const NODE = {
  metadata: { labels: { 'node-role.kubernetes.io/control-plane': '', 'node-role.kubernetes.io/worker': '' } },
  spec: { podCIDR: '10.244.0.0/24' },
  status: {
    addresses: [{ type: 'InternalIP', address: '172.18.0.2' }],
    nodeInfo: {
      kubeletVersion: 'v1.34.1',
      osImage: 'Debian GNU/Linux 12',
      containerRuntimeVersion: 'containerd://2.1.0',
      architecture: 'arm64',
    },
    allocatable: { cpu: '3800m', memory: '15Gi', pods: '110' },
    capacity: { cpu: '4', memory: '16Gi', pods: '110' },
  },
};

const node = (rawJSON: unknown) => objectWith(rawJSON, { kind: 'Node', namespace: '', name: 'pool-a-1' });

describe('NodeDetailsCard', () => {
  it('lists the node’s rows', () => {
    render(<NodeDetailsCard object={node(NODE)} />);
    expect(value('Details', 'Roles')).toHaveTextContent('control-plane, worker');
    expect(value('Details', 'Internal IP')).toHaveTextContent('172.18.0.2');
    expect(value('Details', 'Internal IP')).toHaveClass('font-mono');
    expect(value('Details', 'Kubelet version')).toHaveTextContent('v1.34.1');
    expect(value('Details', 'OS image')).toHaveTextContent('Debian GNU/Linux 12');
    expect(value('Details', 'Container runtime')).toHaveTextContent('containerd://2.1.0');
    expect(value('Details', 'Architecture')).toHaveTextContent('arm64');
    expect(value('Details', 'Pod CIDR')).toHaveTextContent('10.244.0.0/24');
    expect(value('Details', 'Created')).toHaveTextContent(/ · .* ago$/);
  });

  it('dashes what the node leaves out', () => {
    render(<NodeDetailsCard object={node(undefined)} />);
    expect(value('Details', 'Roles')).toHaveTextContent('—');
    expect(value('Details', 'Pod CIDR')).toHaveTextContent('—');
  });
});

describe('CapacityCard', () => {
  it('lists CPU, memory and pods as allocatable over capacity', () => {
    render(<CapacityCard object={node(NODE)} />);
    expect(value('Capacity', 'CPU')).toHaveTextContent('3800m / 4');
    expect(value('Capacity', 'Memory')).toHaveTextContent('15Gi / 16Gi');
    expect(value('Capacity', 'Pods')).toHaveTextContent('110 / 110');
    expect(value('Capacity', 'CPU')).toHaveClass('font-mono');
  });
});
