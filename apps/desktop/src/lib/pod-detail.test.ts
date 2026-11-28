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

import { containerState, podContainers, podLastTermination, podReady, podResources, podRestarts } from './pod-detail';

describe('podReady', () => {
  it('counts the ready containers against the spec', () => {
    const b = { spec: { containers: [{}, {}] }, status: { containerStatuses: [{ ready: true }, { ready: false }] } };
    expect(podReady(b)).toEqual({ ready: 1, total: 2 });
  });

  it('reads the statuses when the spec lists none, and nothing with neither', () => {
    expect(podReady({ status: { containerStatuses: [{ ready: true }] } })).toEqual({ ready: 1, total: 1 });
    expect(podReady({})).toBeUndefined();
  });
});

describe('podRestarts', () => {
  it('sums the init and regular containers’ restarts', () => {
    const b = {
      status: { initContainerStatuses: [{ restartCount: 2 }], containerStatuses: [{ restartCount: 3 }, {}] },
    };
    expect(podRestarts(b)).toBe(5);
  });

  it('reads nothing before any status lands', () => {
    expect(podRestarts({})).toBeUndefined();
  });
});

describe('podLastTermination', () => {
  const ended = (finishedAt: string, reason?: string, exitCode?: number) => ({
    lastState: { terminated: { finishedAt, reason, exitCode } },
  });

  it('picks the newest, an init container’s included', () => {
    const b = {
      status: {
        initContainerStatuses: [ended('2026-11-22T09:20:00Z', 'Error', 1)],
        containerStatuses: [ended('2026-11-22T09:10:00Z', 'OOMKilled', 137), ended('2026-11-22T09:00:00Z')],
      },
    };
    expect(podLastTermination(b)).toEqual({ reason: 'Error', exitCode: 1, finishedAt: '2026-11-22T09:20:00Z' });
  });

  it('skips a termination with no time or one that does not parse', () => {
    const b = {
      status: {
        containerStatuses: [
          { lastState: { terminated: { reason: 'Error' } } },
          ended('soon'),
          ended('2026-11-22T09:00:00Z', 'Completed', 0),
        ],
      },
    };
    expect(podLastTermination(b)).toEqual({ reason: 'Completed', exitCode: 0, finishedAt: '2026-11-22T09:00:00Z' });
  });

  it('reads none for a pod whose containers never ended', () => {
    expect(podLastTermination({ status: { containerStatuses: [{ lastState: {} }] } })).toBeUndefined();
    expect(podLastTermination({})).toBeUndefined();
  });
});

describe('containerState', () => {
  it.each([
    [
      'a waiting reason as failing',
      { state: { waiting: { reason: 'CrashLoopBackOff' } } },
      'CrashLoopBackOff',
      'failing',
    ],
    [
      'ContainerCreating as pending',
      { state: { waiting: { reason: 'ContainerCreating' } } },
      'ContainerCreating',
      'pending',
    ],
    ['PodInitializing as pending', { state: { waiting: { reason: 'PodInitializing' } } }, 'PodInitializing', 'pending'],
    ['a wait with no reason as pending', { state: { waiting: {} } }, 'Waiting', 'pending'],
    ['a ready running container as ok', { ready: true, state: { running: {} } }, 'Running', 'ok'],
    ['a running container not yet ready as pending', { state: { running: {} } }, 'Running', 'pending'],
    ['Completed as ok', { state: { terminated: { reason: 'Completed' } } }, 'Completed', 'ok'],
    ['any other termination as failing', { state: { terminated: { reason: 'Error' } } }, 'Error', 'failing'],
    ['a termination with no reason as failing', { state: { terminated: {} } }, 'Terminated', 'failing'],
  ])('reads %s', (_, status, label, tone) => {
    expect(containerState(status)).toEqual({ label, tone });
  });

  it('reads nothing before the container has a state', () => {
    expect(containerState({})).toBeUndefined();
    expect(containerState(undefined)).toBeUndefined();
  });
});

describe('podContainers', () => {
  it('lists the containers, then the init containers, each joined to its status by name', () => {
    const b = {
      spec: {
        containers: [
          { name: 'app', image: 'app:1' },
          { name: 'proxy', image: 'envoy:1' },
        ],
        initContainers: [{ name: 'migrate', image: 'migrate:1' }],
      },
      status: {
        containerStatuses: [{ name: 'proxy', ready: true, restartCount: 0, state: { running: {} } }],
        initContainerStatuses: [{ name: 'migrate', restartCount: 1, state: { terminated: { reason: 'Completed' } } }],
      },
    };
    expect(podContainers(b)).toEqual([
      { name: 'app', image: 'app:1', init: false, state: undefined, restarts: undefined },
      { name: 'proxy', image: 'envoy:1', init: false, state: { label: 'Running', tone: 'ok' }, restarts: 0 },
      { name: 'migrate', image: 'migrate:1', init: true, state: { label: 'Completed', tone: 'ok' }, restarts: 1 },
    ]);
  });

  it('lists nothing for a pod with no spec, and names a nameless container empty', () => {
    expect(podContainers({})).toEqual([]);
    expect(podContainers({ spec: { containers: [{}] } })[0].name).toBe('');
  });
});

describe('podResources', () => {
  it('reads each side of each container, a dash for one unset', () => {
    const b = {
      spec: {
        containers: [
          {
            name: 'app',
            resources: { requests: { cpu: '100m', memory: '128Mi' }, limits: { cpu: '500m', memory: '256Mi' } },
          },
          { name: 'proxy', resources: { requests: { cpu: '50m' } } },
          {},
        ],
      },
    };
    expect(podResources(b)).toEqual([
      { name: 'app', cpu: '100m / 500m', memory: '128Mi / 256Mi' },
      { name: 'proxy', cpu: '50m / —', memory: '— / —' },
      { name: '', cpu: '— / —', memory: '— / —' },
    ]);
  });
});
