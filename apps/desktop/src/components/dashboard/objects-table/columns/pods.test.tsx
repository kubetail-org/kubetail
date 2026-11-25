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

import { render } from '@testing-library/react';
import { describe, expect, it } from 'vitest';

import type { ClusterCachedDataObject } from '@/lib/cluster-cached-data-objects';

import type { ObjectColumn } from './common';
import { podStatusTone } from './pods';
import { columnsForKind, toneForKind } from './registry';
import { cellText, objectWith } from './testutil';

describe('pod status', () => {
  const status = columnsForKind({ apiVersion: 'v1', resource: 'pods' })[0];
  const failedInit = (terminated: Record<string, unknown>) =>
    objectWith({
      spec: { initContainers: [{ name: 'setup' }] },
      status: { phase: 'Pending', initContainerStatuses: [{ name: 'setup', state: { terminated } }] },
    });

  // kubectl's printPod: the reason, else the signal, else the exit code.
  it('names a failed init container by its reason, signal or exit code', () => {
    expect(cellText(status, failedInit({ reason: 'Error', exitCode: 1 }))).toBe('Init:Error');
    expect(cellText(status, failedInit({ signal: 9, exitCode: 137 }))).toBe('Init:Signal:9');
    expect(cellText(status, failedInit({ exitCode: 2 }))).toBe('Init:ExitCode:2');
    expect(cellText(status, failedInit({}))).toBe('Init:ExitCode:0');
  });

  it('counts init containers off their statuses when the spec lists none', () => {
    const pod = objectWith({ status: { phase: 'Pending', initContainerStatuses: [{ name: 'setup', state: {} }] } });
    expect(cellText(status, pod)).toBe('Init:0/1');
  });

  it('reads past a native sidecar once it has started', () => {
    const pod = objectWith({
      spec: { initContainers: [{ name: 'proxy', restartPolicy: 'Always' }] },
      status: {
        phase: 'Running',
        initContainerStatuses: [{ name: 'proxy', started: true, state: { running: {} } }],
        containerStatuses: [{ name: 'web', ready: true, state: { running: {} } }],
      },
    });
    expect(cellText(status, pod)).toBe('Running');
  });

  const ended = (terminated: Record<string, unknown>) =>
    objectWith({ status: { phase: 'Running', containerStatuses: [{ name: 'web', state: { terminated } }] } });

  it('names a terminated container by its reason, signal or exit code', () => {
    expect(cellText(status, ended({ reason: 'OOMKilled', exitCode: 137 }))).toBe('OOMKilled');
    expect(cellText(status, ended({ signal: 9 }))).toBe('Signal:9');
    expect(cellText(status, ended({ exitCode: 2 }))).toBe('ExitCode:2');
    expect(cellText(status, ended({}))).toBe('ExitCode:0');
  });

  // A container that restarted and recovered still carries its last termination.
  it('reads a stale Completed beside a running container by the pod’s readiness', () => {
    const recovered = (ready: boolean) =>
      objectWith({
        status: {
          phase: 'Running',
          conditions: [{ type: 'Ready', status: ready ? 'True' : 'False' }],
          containerStatuses: [
            { name: 'job', state: { terminated: { reason: 'Completed', exitCode: 0 } } },
            { name: 'web', ready: true, state: { running: {} } },
          ],
        },
      });
    expect(cellText(status, recovered(true))).toBe('Running');
    expect(cellText(status, recovered(false))).toBe('NotReady');
  });

  it('reads a deleted pod as terminating, unless its node is lost or it has finished', () => {
    const deleted = (st: Record<string, unknown>) => objectWith({ metadata: { deletionTimestamp: 'now' }, status: st });
    expect(cellText(status, deleted({ phase: 'Running' }))).toBe('Terminating');
    expect(cellText(status, deleted({ phase: 'Running', reason: 'NodeLost' }))).toBe('Unknown');
    expect(cellText(status, deleted({ phase: 'Succeeded' }))).toBe('Succeeded');
  });
});

describe('podStatusTone', () => {
  it('reads a running or finished pod as ok', () => {
    expect(['Running', 'Completed', 'Succeeded'].map(podStatusTone)).toEqual(['ok', 'ok', 'ok']);
  });

  it('reads a broken pod as failing, an init container’s too', () => {
    const failing = ['CrashLoopBackOff', 'ImagePullBackOff', 'Init:Error', 'Init:Signal:9', 'ExitCode:2', 'Evicted'];
    expect(failing.map(podStatusTone)).toEqual(failing.map(() => 'failing'));
  });

  it('reads a pod on its way somewhere as pending', () => {
    const pending = ['Pending', 'ContainerCreating', 'Init:0/2', 'Terminating', 'NotReady', 'Unknown'];
    expect(pending.map(podStatusTone)).toEqual(pending.map(() => 'pending'));
  });
});

describe('pod columns hidden by default', () => {
  const columns = columnsForKind({ apiVersion: 'v1', resource: 'pods' }).filter((c) => c.hiddenByDefault);
  const pod = objectWith({
    metadata: { ownerReferences: [{ kind: 'StatefulSet', name: 'db', controller: true }] },
    spec: {
      serviceAccountName: 'api',
      priorityClassName: 'high',
      containers: [
        { name: 'web', image: 'web:1' },
        { name: 'proxy', image: 'proxy:2' },
      ],
    },
    status: { podIP: '10.0.0.5', hostIP: '192.168.1.2', qosClass: 'Burstable', startTime: '2026-07-20T10:00:00Z' },
  });
  const byHeader = (h: string) => columns.find((c) => c.header === h)!;

  it('offers kubectl’s wide columns and more of the pod’s fields', () => {
    expect(columns.map((c) => c.header)).toEqual([
      'IP',
      'Host IP',
      'Containers',
      'Images',
      'QoS class',
      'Service account',
      'Priority class',
      'Controlled by',
      'Started',
    ]);
  });

  it('reads each from the pod, and sorts by it', () => {
    const texts = [
      'IP',
      'Host IP',
      'Containers',
      'Images',
      'QoS class',
      'Service account',
      'Priority class',
      'Controlled by',
    ];
    expect(texts.map((h) => cellText(byHeader(h), pod))).toEqual([
      '10.0.0.5',
      '192.168.1.2',
      'web, proxy',
      'web:1, proxy:2',
      'Burstable',
      'api',
      'high',
      'statefulset/db',
    ]);
    expect(byHeader('IP').sortValue?.(pod)).toBe('10.0.0.5');
    expect(byHeader('Controlled by').sortValue?.(pod)).toBe('statefulset/db');
    expect(byHeader('Started').sortValue?.(pod)).toBe(-Date.parse('2026-07-20T10:00:00Z'));
  });

  it('dashes a field the pod lacks', () => {
    const empty = objectWith({});
    expect(columns.map((c) => cellText(c, empty))).toEqual(columns.map(() => '—'));
    expect(byHeader('IP').sortValue?.(empty)).toBeUndefined();
    expect(byHeader('Started').sortValue?.(empty)).toBeUndefined();
  });
});

describe('sort values', () => {
  const sortOf = (c: ObjectColumn, o: ClusterCachedDataObject) => c.sortValue?.(o);

  it('sorts a pod by its status, its share of ready containers, restarts and node', () => {
    const [status, ready, restarts, node] = columnsForKind({ apiVersion: 'v1', resource: 'pods' });
    const pod = objectWith({
      spec: { nodeName: 'pool-a-1', containers: [{}, {}] },
      status: { phase: 'Running', containerStatuses: [{ ready: true, restartCount: 4 }, { ready: false }] },
    });
    expect([status, ready, restarts, node].map((c) => sortOf(c, pod))).toEqual(['Running', 0.5, 4, 'pool-a-1']);
    expect(restarts.descFirst).toBe(true);
    expect(sortOf(ready, objectWith({}))).toBeUndefined();
  });
});

describe('toneForKind', () => {
  const tone = toneForKind({ apiVersion: 'v1', resource: 'pods' })!;
  const running = (ready: boolean) =>
    objectWith({
      spec: { containers: [{}] },
      status: {
        phase: 'Running',
        conditions: [{ type: 'Ready', status: ready ? 'True' : 'False' }],
        containerStatuses: [{ ready, state: { running: {} } }],
      },
    });

  it('reads a running pod as healthy only once it is ready', () => {
    expect(tone(running(true))).toBe('ok');
    expect(tone(running(false))).toBe('pending');
  });

  it('reads a finished pod by its status alone', () => {
    expect(tone(objectWith({ status: { phase: 'Succeeded' } }))).toBe('ok');
    expect(tone(objectWith({ status: { phase: 'Failed' } }))).toBe('failing');
  });

  it('draws an unready running pod’s pill as pending', () => {
    const status = columnsForKind({ apiVersion: 'v1', resource: 'pods' })[0];
    const { container } = render(<>{status.cell(running(false))}</>);
    expect(container.querySelector('[data-tone]')).toHaveAttribute('data-tone', 'pending');
  });
});
