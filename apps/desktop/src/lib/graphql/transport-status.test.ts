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

import { describe, expect, it, vi } from 'vitest';

import { clearStatus, getStatus, markConnected, markDisconnected, subscribeStatus } from './transport-status';

// A unique key per test keeps the module-level registry from bleeding between
// cases (the store is a process-wide singleton, like the real exchange's).
let nextKey = 1;
const freshKey = () => {
  nextKey += 1;
  return nextKey;
};

describe('transport-status', () => {
  it('reports a stable disconnected default for an unknown key', () => {
    const key = freshKey();
    expect(getStatus(key)).toEqual({ connected: false, generation: 0 });
    // useSyncExternalStore requires getSnapshot to be referentially stable
    // across unchanged reads, else it loops forever.
    expect(getStatus(key)).toBe(getStatus(key));
  });

  it('markConnected flips connected true, bumps the generation, and notifies', () => {
    const key = freshKey();
    const cb = vi.fn();
    subscribeStatus(key, cb);

    markConnected(key);
    expect(getStatus(key).connected).toBe(true);
    const gen1 = getStatus(key).generation;
    expect(cb).toHaveBeenCalledTimes(1);

    // Each new connection is a new, strictly-greater generation (the serial is
    // process-wide monotonic, so absolute values depend on prior tests).
    markConnected(key);
    expect(getStatus(key).generation).toBeGreaterThan(gen1);
    expect(cb).toHaveBeenCalledTimes(2);
  });

  it('is globally monotonic — a fresh key never reuses a generation', () => {
    const a = freshKey();
    const b = freshKey();
    markConnected(a);
    markConnected(b);
    // Even two just-opened keys differ: a carried-over accumulator tagged with
    // one key's generation can't alias another's.
    expect(getStatus(b).generation).toBeGreaterThan(getStatus(a).generation);
  });

  it('markDisconnected flips connected false without touching the generation, and notifies', () => {
    const key = freshKey();
    const cb = vi.fn();
    subscribeStatus(key, cb);

    markConnected(key);
    const gen = getStatus(key).generation;
    cb.mockClear();

    markDisconnected(key);
    // Generation is unchanged: a drop is not a new connection, so the hook must
    // keep its last-known data (only an `open` resets).
    expect(getStatus(key)).toEqual({ connected: false, generation: gen });
    expect(cb).toHaveBeenCalledTimes(1);
  });

  it('returns a new snapshot reference only when the status actually changes', () => {
    const key = freshKey();
    markConnected(key);
    const a = getStatus(key);
    const b = getStatus(key);
    expect(a).toBe(b); // no change between reads

    markConnected(key);
    expect(getStatus(key)).not.toBe(a); // changed → new reference
  });

  it('stops notifying after unsubscribe', () => {
    const key = freshKey();
    const cb = vi.fn();
    const unsub = subscribeStatus(key, cb);
    markConnected(key);
    expect(cb).toHaveBeenCalledTimes(1);

    unsub();
    markConnected(key);
    expect(cb).toHaveBeenCalledTimes(1);
  });

  it('clearStatus resets a key back to the disconnected default', () => {
    const key = freshKey();
    markConnected(key);
    expect(getStatus(key).connected).toBe(true);

    clearStatus(key);
    expect(getStatus(key)).toEqual({ connected: false, generation: 0 });
  });
});
