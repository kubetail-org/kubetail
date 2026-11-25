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

import type { ObjectColumn } from './common';
import { columnsForKind, toneForKind } from './registry';
import { cellText, objectWith, pillTone } from './testutil';

const resource = (r: string) => ({ apiVersion: 'resource.k8s.io/v1', resource: r });
const headers = (columns: ObjectColumn[], hidden: boolean) =>
  columns.filter((c) => Boolean(c.hiddenByDefault) === hidden).map((c) => c.header);
const texts = (columns: ObjectColumn[], rawJSON: unknown) => columns.map((c) => cellText(c, objectWith(rawJSON)));

const requests = [
  { exactly: { deviceClassName: 'gpu' } },
  { firstAvailable: [{ deviceClassName: 'big-gpu' }, { deviceClassName: 'small-gpu' }] },
  {},
];

describe('resource claims', () => {
  const columns = columnsForKind(resource('resourceclaims'));
  const tone = toneForKind(resource('resourceclaims'))!;
  const state = columns[0];

  it('draws the state, its classes, devices and reservations hidden', () => {
    expect(headers(columns, false)).toEqual(['State']);
    expect(headers(columns, true)).toEqual(['Device classes', 'Devices', 'Consumers']);
    const claim = {
      spec: { devices: { requests } },
      status: { allocation: { devices: { results: [{ device: 'gpu-0' }] } }, reservedFor: [{}] },
    };
    expect(texts(columns, claim)).toEqual(['allocated,reserved', 'gpu, big-gpu, small-gpu', 'gpu-0', '1']);
    expect(texts(columns, {})).toEqual(['pending', '—', '—', '0']);
  });

  it('reads an allocated claim as healthy, one waiting or deleted as pending', () => {
    const allocated = objectWith({ status: { allocation: {} } });
    const deleted = objectWith({ metadata: { deletionTimestamp: 'now' }, status: { allocation: {} } });
    expect([pillTone(state, allocated), tone(allocated)]).toEqual(['ok', 'ok']);
    expect(cellText(state, deleted)).toBe('allocated,deleted');
    expect([tone(deleted), tone(objectWith({}))]).toEqual(['pending', 'pending']);
  });
});

describe('resource claim templates', () => {
  it('draws the classes and count of its requests', () => {
    const columns = columnsForKind(resource('resourceclaimtemplates'));
    expect(texts(columns, { spec: { spec: { devices: { requests } } } })).toEqual(['gpu, big-gpu, small-gpu', '3']);
    const unnamed = [{ exactly: {} }, { firstAvailable: [{}] }];
    expect(texts(columns, { spec: { spec: { devices: { requests: unnamed } } } })[1]).toBe('2');
    expect(texts(columns, {})).toEqual(['—', '0']);
  });
});

describe('resource slices', () => {
  const columns = columnsForKind(resource('resourceslices'));

  it('draws kubectl’s node, driver and pool, and its devices', () => {
    expect(headers(columns, false)).toEqual(['Node', 'Driver', 'Pool', 'Devices']);
    expect(headers(columns, true)).toEqual(['Generation']);
    const slice = {
      spec: { nodeName: 'n1', driver: 'gpu.example.com', pool: { name: 'n1', generation: 2 }, devices: [{}, {}] },
    };
    expect(texts(columns, slice)).toEqual(['n1', 'gpu.example.com', 'n1', '2', '2']);
    expect(texts(columns, {})).toEqual(['—', '—', '—', '0', '—']);
  });

  it('names a slice for every node', () => {
    expect(cellText(columns[0], objectWith({ spec: { allNodes: true } }))).toBe('<all>');
  });
});

describe('device classes', () => {
  it('counts selectors, and config hidden', () => {
    const columns = columnsForKind(resource('deviceclasses'));
    expect(headers(columns, true)).toEqual(['Config']);
    expect(texts(columns, { spec: { selectors: [{}], config: [{}, {}] } })).toEqual(['1', '2']);
    expect(texts(columns, {})).toEqual(['0', '0']);
  });
});
