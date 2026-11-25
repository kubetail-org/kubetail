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

import type { ClusterCachedDataObject } from '@/lib/cluster-cached-data-objects';

import type { ObjectColumn } from './common';
import { columnsForKind, toneForKind } from './registry';
import { cellText, objectWith, pillTone } from './testutil';

const texts = (columns: ObjectColumn[], o: ClusterCachedDataObject) => columns.map((c) => cellText(c, o));
const visible = (columns: ObjectColumn[]) => columns.filter((c) => !c.hiddenByDefault).map((c) => c.header);
const hidden = (columns: ObjectColumn[]) => columns.filter((c) => c.hiddenByDefault).map((c) => c.header);

describe('node columns', () => {
  const gvr = { apiVersion: 'v1', resource: 'nodes' };
  const columns = columnsForKind(gvr);
  const tone = toneForKind(gvr)!;
  const node = (ready: string | undefined, unschedulable = false) =>
    objectWith({
      spec: { unschedulable },
      status: { conditions: ready ? [{ type: 'Ready', status: ready }] : [] },
    });

  it('shows kubectl’s columns and hides its wide ones', () => {
    expect(visible(columns)).toEqual(['Status', 'Roles', 'Version']);
    expect(hidden(columns)).toEqual([
      'Internal IP',
      'External IP',
      'OS image',
      'Kernel',
      'Container runtime',
      'CPU capacity',
      'Memory capacity',
      'Pod limit',
      'Taints',
    ]);
  });

  it('reads the status off the Ready condition and a cordon', () => {
    const cases = [node('True'), node('True', true), node('False'), node(undefined)];
    expect(cases.map((o) => cellText(columns[0], o))).toEqual([
      'Ready',
      'Ready,SchedulingDisabled',
      'NotReady',
      'Unknown',
    ]);
    expect(cases.map(tone)).toEqual(['ok', 'pending', 'failing', 'failing']);
    expect(pillTone(columns[0], node('False', true))).toBe('failing');
  });

  it('reads roles from both role labels', () => {
    const o = objectWith({
      metadata: {
        labels: {
          'node-role.kubernetes.io/worker': '',
          'node-role.kubernetes.io/control-plane': '',
          'node-role.kubernetes.io/': '',
          'kubernetes.io/role': 'worker',
          zone: 'a',
        },
      },
    });
    expect(cellText(columns[1], o)).toBe('control-plane, worker');
    expect(cellText(columns[1], objectWith({ metadata: { labels: { 'kubernetes.io/role': '' } } }))).toBe('—');
    expect(cellText(columns[1], objectWith({}))).toBe('—');
  });

  it('reads the node’s info, addresses and capacity', () => {
    const o = objectWith({
      spec: { taints: [{}, {}] },
      status: {
        addresses: [
          { type: 'InternalIP', address: '10.0.0.1' },
          { type: 'ExternalIP', address: '1.2.3.4' },
        ],
        capacity: { cpu: '4', memory: '16Gi', pods: '110' },
        nodeInfo: {
          kubeletVersion: 'v1.34.1',
          osImage: 'Ubuntu',
          kernelVersion: '6.8',
          containerRuntimeVersion: 'containerd://2.0',
        },
      },
    });
    expect(texts(columns.slice(2), o)).toEqual([
      'v1.34.1',
      '10.0.0.1',
      '1.2.3.4',
      'Ubuntu',
      '6.8',
      'containerd://2.0',
      '4',
      '16Gi',
      '110',
      '2',
    ]);
    expect(texts(columns.slice(2), objectWith({}))).toEqual([...Array<string>(9).fill('—'), '0']);
  });
});

describe('namespace columns', () => {
  const gvr = { apiVersion: 'v1', resource: 'namespaces' };
  const [status] = columnsForKind(gvr);
  const tone = toneForKind(gvr)!;

  it('reads Active as healthy and anything else as pending', () => {
    const active = objectWith({ status: { phase: 'Active' } });
    const terminating = objectWith({ status: { phase: 'Terminating' } });
    expect([cellText(status, active), cellText(status, terminating), cellText(status, objectWith({}))]).toEqual([
      'Active',
      'Terminating',
      '—',
    ]);
    expect([tone(active), tone(terminating), tone(objectWith({}))]).toEqual(['ok', 'pending', 'pending']);
  });
});

describe('priority class columns', () => {
  const columns = columnsForKind({ apiVersion: 'scheduling.k8s.io/v1', resource: 'priorityclasses' });

  it('reads the value, default, policy and description', () => {
    const o = objectWith({ value: 1000, globalDefault: true, preemptionPolicy: 'Never', description: 'High' });
    expect(texts(columns, o)).toEqual(['1000', 'true', 'Never', 'High']);
    expect(hidden(columns)).toEqual(['Description']);
    expect(texts(columns, objectWith({}))).toEqual(['—', 'false', '—', '—']);
  });
});

describe('runtime class columns', () => {
  const columns = columnsForKind({ apiVersion: 'node.k8s.io/v1', resource: 'runtimeclasses' });

  it('reads the handler and node selector', () => {
    const o = objectWith({ handler: 'runsc', scheduling: { nodeSelector: { sandbox: 'gvisor' } } });
    expect(texts(columns, o)).toEqual(['runsc', 'sandbox=gvisor']);
    expect(texts(columns, objectWith({}))).toEqual(['—', '—']);
  });
});

describe('lease columns', () => {
  const columns = columnsForKind({ apiVersion: 'coordination.k8s.io/v1', resource: 'leases' });

  it('reads the holder and its renewals', () => {
    const renewed = new Date(Date.now() - 5 * 60_000).toISOString();
    const o = objectWith({
      spec: { holderIdentity: 'node-a', renewTime: renewed, leaseDurationSeconds: 40, leaseTransitions: 2 },
    });
    expect(texts(columns, o)).toEqual(['node-a', '5m', '40s', '2']);
    expect(visible(columns)).toEqual(['Holder']);
  });
});

describe('component status columns', () => {
  const gvr = { apiVersion: 'v1', resource: 'componentstatuses' };
  const columns = columnsForKind(gvr);
  const tone = toneForKind(gvr)!;
  const healthy = (status: string) =>
    objectWith({ conditions: [{ type: 'Healthy', status, message: 'ok', error: 'boom' }] });

  it('reads the Healthy condition', () => {
    expect(texts(columns, healthy('True'))).toEqual(['Healthy', 'ok', 'boom']);
    expect(cellText(columns[0], healthy('False'))).toBe('Unhealthy');
    expect(texts(columns, objectWith({}))).toEqual(['—', '—', '—']);
    expect([tone(healthy('True')), tone(healthy('False')), tone(objectWith({}))]).toEqual(['ok', 'failing', 'pending']);
  });
});

describe('flow schema columns', () => {
  const columns = columnsForKind({ apiVersion: 'flowcontrol.apiserver.k8s.io/v1', resource: 'flowschemas' });

  it('reads the priority level, precedence, distinguisher and whether its level is missing', () => {
    const o = objectWith({
      spec: {
        priorityLevelConfiguration: { name: 'workload-low' },
        matchingPrecedence: 9000,
        distinguisherMethod: { type: 'ByUser' },
      },
      status: { conditions: [{ type: 'Dangling', status: 'False' }] },
    });
    expect(texts(columns, o)).toEqual(['workload-low', '9000', 'ByUser', 'False']);
  });
});

describe('priority level columns', () => {
  const columns = columnsForKind({
    apiVersion: 'flowcontrol.apiserver.k8s.io/v1',
    resource: 'prioritylevelconfigurations',
  });

  it('reads the type, shares and queuing', () => {
    const o = objectWith({
      spec: {
        type: 'Limited',
        limited: {
          nominalConcurrencyShares: 30,
          limitResponse: { queuing: { queues: 64, handSize: 6, queueLengthLimit: 50 } },
        },
      },
    });
    expect(texts(columns, o)).toEqual(['Limited', '30', '64', '6', '50']);
    expect(texts(columns, objectWith({ spec: { type: 'Exempt' } }))).toEqual(['Exempt', '—', '—', '—', '—']);
  });
});
