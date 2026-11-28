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
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { ContainersCard, PodDetailsCard, ResourcesCard } from './pods';
import { objectWith } from './testutil';

const NOW = Date.parse('2026-11-22T11:14:00Z');

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] });
  vi.setSystemTime(NOW);
});
afterEach(() => vi.useRealTimers());

const card = (title: string) => screen.getByRole('heading', { level: 3, name: title }).closest('section')!;
const value = (label: string) => within(card('Details')).getByText(label).nextElementSibling as HTMLElement;
const cells = (title: string) =>
  within(card(title))
    .getAllByRole('row')
    .map((r) => Array.from(r.children).map((c) => c.textContent));

const POD = {
  metadata: { ownerReferences: [{ kind: 'ReplicaSet', name: 'worker-6b7c9d8f5', controller: true }] },
  spec: {
    nodeName: 'pool-a-2',
    serviceAccountName: 'worker',
    priorityClassName: 'high',
    containers: [
      {
        name: 'worker',
        image: 'ghcr.io/acme/worker:1.4',
        resources: { requests: { cpu: '100m', memory: '128Mi' }, limits: { cpu: '500m' } },
      },
      { name: 'proxy', image: 'envoy:1.30' },
    ],
    initContainers: [{ name: 'migrate', image: 'migrate:2' }],
  },
  status: {
    podIP: '10.1.2.3',
    qosClass: 'Burstable',
    containerStatuses: [
      {
        name: 'worker',
        ready: false,
        restartCount: 4,
        state: { waiting: { reason: 'CrashLoopBackOff' } },
        lastState: { terminated: { reason: 'Error', exitCode: 1, finishedAt: '2026-11-22T11:04:00Z' } },
      },
      { name: 'proxy', ready: true, restartCount: 0, state: { running: {} } },
    ],
    initContainerStatuses: [{ name: 'migrate', restartCount: 0, state: { terminated: { reason: 'Completed' } } }],
  },
};

describe('PodDetailsCard', () => {
  it('lists the pod’s rows in order', () => {
    render(<PodDetailsCard object={objectWith(POD)} />);
    const labels = Array.from(card('Details').querySelectorAll('dt')).map((dt) => dt.textContent);
    expect(labels).toEqual([
      'Namespace',
      'Controlled by',
      'Node',
      'Pod IP',
      'Created',
      'Ready',
      'Restarts',
      'Last termination',
      'Service account',
      'Priority class',
    ]);
    expect(value('Node')).toHaveTextContent('pool-a-2');
    expect(value('Node')).toHaveClass('font-mono');
    expect(value('Pod IP')).toHaveTextContent('10.1.2.3');
    expect(value('Ready')).toHaveTextContent('1/2');
    expect(value('Restarts')).toHaveTextContent('4');
    expect(value('Restarts')).toHaveClass('text-destructive');
    expect(value('Last termination')).toHaveTextContent('Error · exit 1 · 10m ago');
    expect(value('Service account')).toHaveTextContent('worker');
    expect(value('Priority class')).toHaveTextContent('high');
  });

  it('dashes every missing value, and leaves no restarts in red', () => {
    render(<PodDetailsCard object={objectWith(undefined)} />);
    for (const label of [
      'Controlled by',
      'Node',
      'Pod IP',
      'Ready',
      'Restarts',
      'Last termination',
      'Priority class',
    ]) {
      expect(value(label)).toHaveTextContent('—');
    }
    expect(value('Restarts')).not.toHaveClass('text-destructive');
  });

  it('draws a termination’s time alone when it names no reason or exit code', () => {
    const b = {
      status: { containerStatuses: [{ lastState: { terminated: { finishedAt: '2026-11-22T11:04:00Z' } } }] },
    };
    render(<PodDetailsCard object={objectWith(b)} />);
    expect(value('Last termination')).toHaveTextContent(/^10m ago$/);
  });
});

describe('ContainersCard', () => {
  it('lists the containers, then the init containers, with their state and restarts', () => {
    render(<ContainersCard object={objectWith(POD)} />);
    expect(cells('Containers')).toEqual([
      ['Name', 'Image', 'State', 'Restarts'],
      ['worker', 'ghcr.io/acme/worker:1.4', 'CrashLoopBackOff', '4'],
      ['proxy', 'envoy:1.30', 'Running', '0'],
      ['migrate init', 'migrate:2', 'Completed', '0'],
    ]);
    expect(within(card('Containers')).getByText('3')).toBeInTheDocument();
    const [, worker] = within(card('Containers')).getAllByRole('row');
    expect(worker.querySelector('[data-tone]')).toHaveAttribute('data-tone', 'failing');
    expect(within(worker).getByText('4')).toHaveClass('text-destructive');
  });

  it('dashes a container with no status yet, and one with no name', () => {
    render(<ContainersCard object={objectWith({ spec: { containers: [{ name: 'app' }, {}] } })} />);
    expect(cells('Containers')[1]).toEqual(['app', '—', '—', '—']);
    expect(cells('Containers')[2]).toEqual(['—', '—', '—', '—']);
  });
});

describe('ResourcesCard', () => {
  it('lists each container’s requests and limits under its QoS class', () => {
    render(<ResourcesCard object={objectWith(POD)} />);
    expect(cells('Resources')).toEqual([
      ['Container', 'CPU request / limit', 'Memory request / limit'],
      ['worker', '100m / 500m', '128Mi / —'],
      ['proxy', '— / —', '— / —'],
    ]);
    expect(within(card('Resources')).getByText('QoS Burstable')).toBeInTheDocument();
  });

  it('names no class the pod lacks', () => {
    render(<ResourcesCard object={objectWith({ spec: { containers: [{}] } })} />);
    expect(within(card('Resources')).queryByText(/QoS/)).not.toBeInTheDocument();
    expect(cells('Resources')[1]).toEqual(['—', '— / —', '— / —']);
  });
});
