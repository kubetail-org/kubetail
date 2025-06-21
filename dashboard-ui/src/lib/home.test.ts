// Copyright 2024 Andres Morey
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

import { applySearchAndFilter, noSearchResults, getContainerIDs } from './home';
import type { WorkloadItem } from './home';

export const workloadItemMock = [
  {
    __typename: 'AppsV1DaemonSet',
    id: 'b19915f5-cfcf-442e-b5a9-7eeea9c6de08',
    metadata: {
      __typename: 'MetaV1ObjectMeta',
      namespace: 'kube-system',
      name: 'kindnet',
      uid: 'b19915f5-cfcf-442e-b5a9-7eeea9c6de08',
      creationTimestamp: '2025-05-03T11:01:32.000Z',
      deletionTimestamp: null,
      resourceVersion: '621',
      ownerReferences: [],
    },
  },
  {
    __typename: 'AppsV1DaemonSet',
    id: 'f816bf39-fd58-44c0-918c-c56e3be62f59',
    metadata: {
      __typename: 'MetaV1ObjectMeta',
      namespace: 'kube-system',
      name: 'kube-proxy',
      uid: 'f816bf39-fd58-44c0-918c-c56e3be62f59',
      creationTimestamp: '2025-05-03T11:01:31.000Z',
      deletionTimestamp: null,
      resourceVersion: '564',
      ownerReferences: [],
    },
  },
  {
    __typename: 'AppsV1DaemonSet',
    id: 'b7577341-a44c-494e-bb1a-a45200fe57e3',
    metadata: {
      __typename: 'MetaV1ObjectMeta',
      namespace: 'kubetail-system',
      name: 'kubetail-cluster-agent',
      uid: 'b7577341-a44c-494e-bb1a-a45200fe57e3',
      creationTimestamp: '2025-05-03T11:23:29.000Z',
      deletionTimestamp: '2025-05-03T13:01:31.000Z',
      resourceVersion: '12542',
      ownerReferences: [],
    },
  },
] satisfies WorkloadItem[];

const ownershipMapMock = new Map([
  [
    'dc8fbace-67c0-43d5-a59d-aaff6dbe2d7a',
    ['60c83096-174e-4191-a705-1245b52a0e33', '5955f63b-b69b-45de-b2e1-2eb60e4cd15e'],
  ],
  [
    '60c83096-174e-4191-a705-1245b52a0e33',
    ['93edde53-0bb8-44e6-b271-0022abe42100', 'edc816e9-dea5-4133-b499-89984b9ebb14'],
  ],
  [
    'db03b586-95df-48f3-aaeb-9e0de42d3926',
    ['3596ec70-0de7-40a9-90a8-d57f8931ae15', '603414f8-cdec-40dd-bbbe-7ada2473d77c'],
  ],
]);

describe('applySearchAndFilter', () => {
  it('returns undefined when fetching is true', () => {
    const result = applySearchAndFilter(true, workloadItemMock, '', '');

    expect(result).toBeUndefined();
  });

  it('returns undefined if the workload item is undefined or null', () => {
    const firstResult = applySearchAndFilter(false, undefined, '', '');
    const secondResult = applySearchAndFilter(false, null, '', '');

    expect(firstResult).toBeUndefined();
    expect(secondResult).toBeUndefined();
  });

  it('returns only the non deleted workload items', () => {
    const result = applySearchAndFilter(false, workloadItemMock, '', '');
    const expected = workloadItemMock.filter((item) => item.metadata.deletionTimestamp === null);

    expect(result).toEqual(expected);
  });

  it('returns workload items within selected namespace and search string', () => {
    const namespace = 'kube-system';
    const search = 'kind';

    const result = applySearchAndFilter(false, workloadItemMock, search, namespace);
    const expected = workloadItemMock.filter(
      (item) => item.metadata.namespace === namespace && item.metadata.name.includes(search),
    );

    expect(result).toEqual(expected);
  });

  it('returns all workloads from selected namespace', () => {
    const firstResult = applySearchAndFilter(false, workloadItemMock, '', 'kube-system');
    const expected = workloadItemMock.filter((item) => item.metadata.namespace === 'kube-system');
    expect(firstResult).toEqual(expected);

    // returns empty array as the only workload item in namespace kubetail-system has a deletion timestamp
    const secondResult = applySearchAndFilter(false, workloadItemMock, '', 'kubetail-system');
    expect(secondResult).toEqual([]);
  });
});

describe('noSearchResults', () => {
  it('returns true if all workload items are undefined', () => {
    const result = noSearchResults(undefined, undefined, undefined);
    expect(result).toEqual(true);
  });

  it('returns true if all workload items are either undefined or empty arrays', () => {
    const result = noSearchResults(undefined, undefined, undefined, [], []);
    expect(result).toEqual(true);
  });

  it('returns false if any or every workload item has alteast one item in it', () => {
    const firstResult = noSearchResults(workloadItemMock, workloadItemMock, workloadItemMock, workloadItemMock);
    const secondResult = noSearchResults(workloadItemMock, workloadItemMock, undefined, []);

    expect(firstResult).toEqual(false);
    expect(secondResult).toEqual(false);
  });
});

describe('getContainerIDs', () => {
  it('returns an empty array if parentId is not present in the ownership map', () => {
    const result = getContainerIDs('dc8fbace-67c0-aaff6dbe2d7a-id-not-present', ownershipMapMock);

    expect(result).toEqual([]);
  });

  it('returns all the immediate child IDs when none of them have children of their own ', () => {
    const parentId = 'db03b586-95df-48f3-aaeb-9e0de42d3926';
    const result = getContainerIDs(parentId, ownershipMapMock);
    const expected = ownershipMapMock.get(parentId);

    expect(result).toEqual(expected);
  });

  it('returns only the leaf node IDs while omitting the parent nodes from results', () => {
    const parentId = 'dc8fbace-67c0-43d5-a59d-aaff6dbe2d7a';
    const result = getContainerIDs(parentId, ownershipMapMock);

    const expected = [
      '5955f63b-b69b-45de-b2e1-2eb60e4cd15e',
      '93edde53-0bb8-44e6-b271-0022abe42100',
      'edc816e9-dea5-4133-b499-89984b9ebb14',
    ];

    expect(new Set(result)).toEqual(new Set(expected));
  });
});
