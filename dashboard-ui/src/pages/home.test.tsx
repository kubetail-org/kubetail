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

import { createMemoryHistory } from 'history';
import { Router } from 'react-router-dom';
import { render } from '@testing-library/react';

import { describe, it, expect } from 'vitest';

import Home, { applySearchAndFilter, noSearchResults } from '@/pages/home';
import { ownerShipMapMock, workloadItemMock } from '@/mocks/home';
import { getContainerIDs } from './home';

describe('home page', () => {
  it('blocks access if user is unauthenticated', () => {
    const history = createMemoryHistory();

    render(
      <Router location={history.location} navigator={history}>
        <Home />
      </Router>,
    );

    // assertions
    expect(history.location.pathname).toBe('/auth/login');
  });
});

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
    const nonDeletedWorkload = workloadItemMock.filter((item) => item.metadata.deletionTimestamp === null);

    expect(result).toEqual(nonDeletedWorkload);
  });

  it('returns workload items within selected namespace and search string', () => {
    const namespace = 'kube-system';
    const search = 'kind';

    const result = applySearchAndFilter(false, workloadItemMock, search, namespace);
    const output = workloadItemMock.filter((item) => item.metadata.namespace === namespace && item.metadata.name.includes(search));

    expect(result).toEqual(output);
  });

  it('returns all workloads from selected namespace', () => {
    const firstResult = applySearchAndFilter(false, workloadItemMock, '', 'kube-system');
    const output = workloadItemMock.filter((item) => item.metadata.namespace === 'kube-system');
    expect(firstResult).toEqual(output);

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
  it('return an empty array if parentId is not present in the ownership map', () => {
    const result = getContainerIDs('dc8fbace-67c0-aaff6dbe2d7a-id-not-present', ownerShipMapMock);

    expect(result).toEqual([]);
  });

  it('returns all the values of a parentId when the childId is not a key in the ownership map', () => {
    const parentId = 'dc8fbace-67c0-43d5-a59d-aaff6dbe2d7a';
    const result = getContainerIDs(parentId, ownerShipMapMock);
    const output = ownerShipMapMock.get(parentId);

    expect(result).toEqual(output);
  });

  it('returns parentId values merged with childId values, excluding childId itself that is a key in ownership map', () => {
    const parentId = 'db03b586-95df-48f3-aaeb-9e0de42d3926';

    // this is the childId of parentId that is also a key in ownership map
    const childId = '603414f8-cdec-40dd-bbbe-7ada2473d77c';
    const result = getContainerIDs(parentId, ownerShipMapMock);

    const mainIds = ownerShipMapMock.get(parentId)?.filter((id) => id !== childId) ?? [];
    const childIds = ownerShipMapMock.get(childId) ?? [];
    const output = [...mainIds, ...childIds];

    expect(new Set(result)).toEqual(new Set(output));
  });
});
