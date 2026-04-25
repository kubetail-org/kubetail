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

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { k8sPagination, waitUntilOnline, waitUntilVisible } from './apollo-client';

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

describe('waitUntilVisible', () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('resolves immediately when the document is already visible', async () => {
    vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('visible');

    await expect(waitUntilVisible()).resolves.toBeUndefined();
  });

  it('resolves when visibility transitions to visible', async () => {
    const stateSpy = vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden');

    const pending = waitUntilVisible();

    stateSpy.mockReturnValue('visible');
    document.dispatchEvent(new Event('visibilitychange'));

    await expect(pending).resolves.toBeUndefined();
  });

  it('does not resolve while the document remains hidden', async () => {
    vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden');

    let resolved = false;
    waitUntilVisible().then(() => {
      resolved = true;
    });

    document.dispatchEvent(new Event('visibilitychange'));
    await Promise.resolve();

    expect(resolved).toBe(false);
  });
});

describe('waitUntilOnline', () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('resolves immediately when navigator is online', async () => {
    vi.spyOn(navigator, 'onLine', 'get').mockReturnValue(true);

    await expect(waitUntilOnline()).resolves.toBeUndefined();
  });

  it('resolves when an online event fires', async () => {
    vi.spyOn(navigator, 'onLine', 'get').mockReturnValue(false);

    const pending = waitUntilOnline();

    window.dispatchEvent(new Event('online'));

    await expect(pending).resolves.toBeUndefined();
  });
});
