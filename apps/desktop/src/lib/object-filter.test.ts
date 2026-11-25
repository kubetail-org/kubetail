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

import { matchesFilter } from './object-filter';

const pod = {
  name: 'checkout-api-7d9f',
  namespace: 'shop',
  rawJSON: {
    metadata: {
      labels: { app: 'checkout', tier: 'web', 'pod-template-hash': '7d9f' },
      ownerReferences: [{ kind: 'ReplicaSet', name: 'storefront-7d9f', controller: true }],
    },
    spec: { nodeName: 'pool-a-2' },
  },
};

describe('matchesFilter', () => {
  it('matches everything with no query', () => {
    expect(matchesFilter(pod, '  ')).toBe(true);
  });

  it('skips a custom resource’s nodeName that is not text', () => {
    const widget = { name: 'w', namespace: 'shop', rawJSON: { spec: { nodeName: { pool: 'a' } } } };
    expect(matchesFilter(widget, 'pool')).toBe(false);
    expect(matchesFilter({ ...widget, rawJSON: { spec: { nodeName: 7 } } }, '7')).toBe(false);
  });

  it('matches a substring of the name, in any case', () => {
    expect(matchesFilter(pod, 'API-7')).toBe(true);
    expect(matchesFilter(pod, 'cart')).toBe(false);
  });

  it('matches a label as key=value', () => {
    expect(matchesFilter(pod, 'tier=web')).toBe(true);
    expect(matchesFilter(pod, 'app=check')).toBe(true);
    expect(matchesFilter(pod, 'tier=db')).toBe(false);
  });

  it('matches the node', () => {
    expect(matchesFilter(pod, ' pool-a ')).toBe(true);
  });

  it('reads an object without a body by its name alone', () => {
    expect(matchesFilter({ name: 'pv-1', namespace: '', rawJSON: null }, 'pv')).toBe(true);
    expect(matchesFilter({ name: 'pv-1', namespace: '', rawJSON: null }, 'pool')).toBe(false);
  });

  it('matches the namespace only when asked to', () => {
    expect(matchesFilter(pod, 'SHOP')).toBe(false);
    expect(matchesFilter(pod, 'SHOP', { namespace: true })).toBe(true);
  });

  it('matches the owner as it is drawn', () => {
    expect(matchesFilter(pod, 'deploy/store')).toBe(true);
    expect(matchesFilter(pod, 'replicaset/')).toBe(false);
  });
});
