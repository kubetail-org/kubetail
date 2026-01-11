// Copyright 2024-2026 The Kubetail Authors
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

import { describe, it, expect, beforeEach } from 'vitest';
import { k8sPagination } from './apollo-client';

describe('k8sPagination merge logic', () => {
  let mergeFunction: ReturnType<typeof k8sPagination>['merge'];

  beforeEach(() => {
    mergeFunction = k8sPagination().merge;
  });

  it('should return incoming data on first call when existing is undefined', () => {
    const incoming = {
      metadata: { continue: 'token1' },
      items: [{ id: 1 }, { id: 2 }],
    };

    const result = mergeFunction(undefined, incoming, { args: { options: { continue: '' } } });

    expect(result).toEqual(incoming);
  });

  it('should return incoming data on refetch call when continue is empty string', () => {
    const existing = {
      metadata: { continue: 'token1' },
      items: [{ id: 1 }, { id: 2 }],
    };
    const incoming = {
      metadata: { continue: 'token2' },
      items: [{ id: 3 }, { id: 4 }],
    };

    const result = mergeFunction(existing, incoming, { args: { options: { continue: '' } } });

    expect(result).toEqual(incoming);
  });

  it('should merge items when continue token matches existing continue token', () => {
    const existing = {
      metadata: { continue: 'token1' },
      items: [{ id: 1 }, { id: 2 }],
    };
    const incoming = {
      metadata: { continue: 'token2' },
      items: [{ id: 3 }, { id: 4 }],
    };

    const result = mergeFunction(existing, incoming, { args: { options: { continue: 'token1' } } });

    expect(result).toEqual({
      metadata: { continue: 'token2' },
      items: [{ id: 1 }, { id: 2 }, { id: 3 }, { id: 4 }],
    });
  });

  it('should return existing data when continue token does not match', () => {
    const existing = {
      metadata: { continue: 'token1' },
      items: [{ id: 1 }, { id: 2 }],
    };
    const incoming = {
      metadata: { continue: 'token2' },
      items: [{ id: 3 }, { id: 4 }],
    };

    const result = mergeFunction(existing, incoming, { args: { options: { continue: 'differentToken' } } });

    expect(result).toEqual(existing);
  });
});
