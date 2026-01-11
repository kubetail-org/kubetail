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

import { getContainerIDs } from './util';

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
