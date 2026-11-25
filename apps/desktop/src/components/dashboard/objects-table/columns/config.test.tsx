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
import { cellText, objectWith } from './testutil';

const texts = (columns: ObjectColumn[], o: ClusterCachedDataObject) => columns.map((c) => cellText(c, o));

describe('config map columns', () => {
  const columns = columnsForKind({ apiVersion: 'v1', resource: 'configmaps' });

  it('counts and lists the data and binary data keys', () => {
    const o = objectWith({ data: { a: '1', b: '2' }, binaryData: { c: 'AA==' } });
    expect(texts(columns, o)).toEqual(['3', 'a, b, c']);
    expect(columns[1].hiddenByDefault).toBe(true);
    expect(texts(columns, objectWith({}))).toEqual(['0', '—']);
  });
});

describe('secret columns', () => {
  const columns = columnsForKind({ apiVersion: 'v1', resource: 'secrets' });

  it('reads the type and counts and lists the keys', () => {
    const o = objectWith({ type: 'kubernetes.io/tls', data: { 'tls.crt': '[redacted]', 'tls.key': '[redacted]' } });
    expect(texts(columns, o)).toEqual(['kubernetes.io/tls', '2', 'tls.crt, tls.key']);
    expect(texts(columns, objectWith({}))).toEqual(['—', '0', '—']);
  });

  it('has no status', () => {
    expect(toneForKind({ apiVersion: 'v1', resource: 'secrets' })).toBeUndefined();
  });
});

describe('resource quota columns', () => {
  const columns = columnsForKind({ apiVersion: 'v1', resource: 'resourcequotas' });

  it('reads each capped resource as used of hard, limits apart', () => {
    const o = objectWith({
      status: {
        hard: { pods: '10', 'requests.cpu': '4', 'limits.memory': '8Gi' },
        used: { pods: '3', 'limits.memory': '2Gi' },
      },
    });
    expect(texts(columns, o)).toEqual(['pods: 3/10, requests.cpu: 0/4', 'limits.memory: 2Gi/8Gi']);
    expect(texts(columns, objectWith({}))).toEqual(['—', '—']);
  });
});

describe('limit range columns', () => {
  const columns = columnsForKind({ apiVersion: 'v1', resource: 'limitranges' });

  it('names each type it limits once', () => {
    const o = objectWith({ spec: { limits: [{ type: 'Container' }, { type: 'Pod' }, { type: 'Container' }] } });
    expect(texts(columns, o)).toEqual(['Container, Pod']);
    expect(texts(columns, objectWith({}))).toEqual(['—']);
  });
});
