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

import { ownerOf } from './object-owner';

const owned = (kind: string, name: string, labels?: Record<string, string>, controller = true) => ({
  metadata: { labels, ownerReferences: [{ kind, name, controller }] },
});

describe('ownerOf', () => {
  it('reads a Deployment’s ReplicaSet as the Deployment', () => {
    expect(ownerOf(owned('ReplicaSet', 'checkout-api-7d9f', { 'pod-template-hash': '7d9f' }))).toBe(
      'deploy/checkout-api',
    );
  });

  it('keeps a ReplicaSet whose name does not end in the pod’s hash', () => {
    expect(ownerOf(owned('ReplicaSet', 'standalone', { 'pod-template-hash': '7d9f' }))).toBe('replicaset/standalone');
    expect(ownerOf(owned('ReplicaSet', 'standalone'))).toBe('replicaset/standalone');
  });

  it('names any other controller by its kind', () => {
    expect(ownerOf(owned('StatefulSet', 'checkout-db'))).toBe('statefulset/checkout-db');
    expect(ownerOf(owned('Deployment', 'checkout-api'))).toBe('deploy/checkout-api');
  });

  it('reads no owner without a controller reference, or without a body', () => {
    expect(ownerOf(owned('StatefulSet', 'checkout-db', undefined, false))).toBeUndefined();
    expect(ownerOf({ metadata: { ownerReferences: [{ controller: true, name: 'x' }] } })).toBeUndefined();
    expect(ownerOf({})).toBeUndefined();
    expect(ownerOf(null)).toBeUndefined();
  });
});
