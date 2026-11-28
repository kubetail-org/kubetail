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

import { conditionsOf, controllerOf, labelsOf } from './object-detail';

describe('controllerOf', () => {
  it('names the direct controller by its kind and name, never the workload above it', () => {
    const pod = {
      metadata: {
        labels: { 'pod-template-hash': '6b7c9d8f5' },
        ownerReferences: [
          { kind: 'Node', name: 'pool-a', controller: false },
          { kind: 'ReplicaSet', name: 'worker-6b7c9d8f5', controller: true },
        ],
      },
    };
    expect(controllerOf(pod)).toBe('ReplicaSet worker-6b7c9d8f5');
  });

  it('reads none without a controller reference, a kind, a name or a body', () => {
    expect(controllerOf({ metadata: { ownerReferences: [{ kind: 'Job', name: 'x' }] } })).toBeUndefined();
    expect(controllerOf({ metadata: { ownerReferences: [{ name: 'x', controller: true }] } })).toBeUndefined();
    expect(controllerOf({ metadata: { ownerReferences: [{ kind: 'Job', controller: true }] } })).toBeUndefined();
    expect(controllerOf(undefined)).toBeUndefined();
    expect(controllerOf(null)).toBeUndefined();
  });
});

describe('conditionsOf', () => {
  it('reads `status.conditions` as they are', () => {
    const conditions = [
      { type: 'Ready', status: 'True', reason: 'PodReady', lastTransitionTime: '2026-11-22T09:14:00Z' },
    ];
    expect(conditionsOf({ status: { conditions } })).toEqual(conditions);
  });

  it('drops an entry that is not an object, and reads a field that is not a string as missing', () => {
    const conditions = [null, 'Ready', ['x'], { type: 'Synced', status: true, reason: 3, lastTransitionTime: {} }];
    expect(conditionsOf({ status: { conditions } })).toEqual([
      { type: 'Synced', status: undefined, reason: undefined, lastTransitionTime: undefined },
    ]);
  });

  it('reads none for a kind that keeps no array there', () => {
    expect(conditionsOf({ status: { conditions: { Ready: true } } })).toBeUndefined();
    expect(conditionsOf({ status: {} })).toBeUndefined();
    expect(conditionsOf(null)).toBeUndefined();
  });
});

describe('labelsOf', () => {
  it('reads each label as a key and value', () => {
    expect(labelsOf({ metadata: { labels: { app: 'worker', tier: 'jobs' } } })).toEqual([
      ['app', 'worker'],
      ['tier', 'jobs'],
    ]);
  });

  it('reads none without labels', () => {
    expect(labelsOf({ metadata: {} })).toEqual([]);
    expect(labelsOf(undefined)).toEqual([]);
  });
});
