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

import { mockTauriCore } from '@/test-utils';

// Mocks ---------------------------------------------------------------

const { invokeMock, factory } = mockTauriCore();
vi.mock('@tauri-apps/api/core', () => factory());

// `listen` resolves to its own unlisten, and the timing of that resolution is the
// thing under test, so drive it by hand rather than with the shared fake.
const { listenMock } = vi.hoisted(() => ({ listenMock: vi.fn() }));
vi.mock('@tauri-apps/api/event', () => ({ listen: listenMock }));

const { readInjectedHostFile, subscribeHostFile, updateHostFile } = await import('./host-file');

// Helpers -------------------------------------------------------------

// A `listen` whose promise settles when the test says so.
function deferredListen() {
  const unlisten = vi.fn();
  let resolveListen!: () => void;
  let handler: ((event: { payload: unknown }) => void) | undefined;
  listenMock.mockImplementation(
    (_name: string, cb: (event: { payload: unknown }) => void) =>
      new Promise((resolve) => {
        handler = cb;
        resolveListen = () => resolve(unlisten);
      }),
  );
  return {
    unlisten,
    settle: async () => {
      resolveListen();
      await Promise.resolve();
    },
    emit: (payload: unknown) => handler?.({ payload }),
  };
}

// Tests ---------------------------------------------------------------

describe('readInjectedHostFile', () => {
  it('reads the snapshot the host injected before first paint', () => {
    window.__KUBETAIL_HOST__ = { schemaVersion: 1, colorSchemePreference: 'dark' };
    expect(readInjectedHostFile()).toEqual({ schemaVersion: 1, colorSchemePreference: 'dark' });
  });

  it('reads an empty file outside Tauri, where nothing was injected', () => {
    delete window.__KUBETAIL_HOST__;
    expect(readInjectedHostFile()).toEqual({});
  });
});

describe('updateHostFile', () => {
  it('sends the patch for the host to merge', () => {
    invokeMock.mockReset();
    invokeMock.mockResolvedValue(undefined);
    updateHostFile({ colorSchemePreference: 'light' });
    expect(invokeMock).toHaveBeenCalledWith('update_host_file', { patch: { colorSchemePreference: 'light' } });
  });

  it('swallows a failed write, which costs durability and not the session', async () => {
    invokeMock.mockReset();
    invokeMock.mockRejectedValue(new Error('no host'));
    expect(() => updateHostFile({ colorSchemePreference: 'light' })).not.toThrow();
    // An unhandled rejection would fail the suite.
    await Promise.resolve();
  });
});

describe('subscribeHostFile', () => {
  beforeEach(() => {
    invokeMock.mockReset();
    invokeMock.mockResolvedValue(undefined);
  });

  it('reads the file once listening, for a write broadcast before the listener mounted', async () => {
    invokeMock.mockResolvedValue({ colorSchemePreference: 'dark' });
    const listen = deferredListen();
    const onUpdate = vi.fn();
    subscribeHostFile(onUpdate);
    expect(invokeMock).not.toHaveBeenCalled();
    await listen.settle();

    expect(invokeMock).toHaveBeenCalledWith('read_host_file');
    await vi.waitFor(() => expect(onUpdate).toHaveBeenCalledWith({ colorSchemePreference: 'dark' }));
  });

  it('drops the read when a broadcast was heard first', async () => {
    let resolveRead!: (file: unknown) => void;
    invokeMock.mockReturnValue(
      new Promise((resolve) => {
        resolveRead = resolve;
      }),
    );
    const listen = deferredListen();
    const onUpdate = vi.fn();
    subscribeHostFile(onUpdate);
    await listen.settle();

    listen.emit({ colorSchemePreference: 'light' });
    resolveRead({ colorSchemePreference: 'dark' });
    await Promise.resolve();
    await Promise.resolve();
    expect(onUpdate).toHaveBeenCalledTimes(1);
    expect(onUpdate).toHaveBeenCalledWith({ colorSchemePreference: 'light' });
  });

  it('drops a read that lands after the unsubscribe', async () => {
    let resolveRead!: (file: unknown) => void;
    invokeMock.mockReturnValue(
      new Promise((resolve) => {
        resolveRead = resolve;
      }),
    );
    const listen = deferredListen();
    const onUpdate = vi.fn();
    const unsubscribe = subscribeHostFile(onUpdate);
    await listen.settle();

    unsubscribe();
    resolveRead({ colorSchemePreference: 'dark' });
    await Promise.resolve();
    await Promise.resolve();
    expect(onUpdate).not.toHaveBeenCalled();
  });

  it('survives a failed read', async () => {
    invokeMock.mockRejectedValue(new Error('no host'));
    const listen = deferredListen();
    const onUpdate = vi.fn();
    subscribeHostFile(onUpdate);
    await listen.settle();

    // An unhandled rejection would fail the suite.
    await Promise.resolve();
    listen.emit({ colorSchemePreference: 'dark' });
    expect(onUpdate).toHaveBeenCalledWith({ colorSchemePreference: 'dark' });
  });

  it('delivers the merged file the host broadcasts', async () => {
    const listen = deferredListen();
    const onUpdate = vi.fn();
    subscribeHostFile(onUpdate);
    await listen.settle();

    listen.emit({ colorSchemePreference: 'dark' });
    expect(onUpdate).toHaveBeenLastCalledWith({ colorSchemePreference: 'dark' });
  });

  it('reads a payload-less event as an empty file', async () => {
    const listen = deferredListen();
    const onUpdate = vi.fn();
    subscribeHostFile(onUpdate);
    await listen.settle();

    listen.emit(null);
    expect(onUpdate).toHaveBeenCalledWith({});
  });

  it('unsubscribes once registration has landed', async () => {
    const listen = deferredListen();
    const unsubscribe = subscribeHostFile(vi.fn());
    await listen.settle();

    unsubscribe();
    expect(listen.unlisten).toHaveBeenCalledTimes(1);
  });

  it('unsubscribes a registration that was still in flight', async () => {
    // A `useEffect` cleanup never awaits `listen`, so the late resolution has to
    // undo itself or the listener outlives the component.
    const listen = deferredListen();
    const unsubscribe = subscribeHostFile(vi.fn());

    unsubscribe();
    await listen.settle();
    expect(listen.unlisten).toHaveBeenCalledTimes(1);
  });

  it('survives an absent bridge, where there is no broadcast to track', async () => {
    listenMock.mockRejectedValue(new Error('no bridge'));
    const unsubscribe = subscribeHostFile(vi.fn());
    await Promise.resolve();
    expect(() => unsubscribe()).not.toThrow();
  });
});
