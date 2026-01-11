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

import type { Virtualizer } from '@tanstack/react-virtual';
import { render, renderHook, act, waitFor } from '@testing-library/react';
import { createRef, type RefObject } from 'react';

import { DoubleTailedArray } from './double-tailed-array';
import { createMockClient, type MockClient } from './mock';
import type { LogRecord } from './types';

import {
  useAutoScroll,
  useFollowFromEnd,
  useInit,
  useLoadMore,
  useLoadMoreAfter,
  useLoadMoreBefore,
  usePullToRefresh,
  useRecordStore,
  useLogViewerState,
  LogViewerInner,
  LogViewer,
} from './log-viewer';
import type { LogRecordInternal, Runtime, LogViewerHandle, LogViewerVirtualizer } from './log-viewer';

const mockVirtualizer = {
  range: null,
  getTotalSize: vi.fn(() => 0),
  getVirtualItems: vi.fn(() => [] as { key: number; index: number; size: number; start: number }[]),
  measureElement: vi.fn(),
  scrollToIndex: vi.fn(),
  isScrolling: false,
};

vi.mock('@tanstack/react-virtual', () => ({
  useVirtualizer: () => mockVirtualizer,
}));

function createMockRecords(count: number, startTimestamp = 0): LogRecord[] {
  return Array.from({ length: count }, (_, i) => {
    const timestamp = new Date(startTimestamp + i * 1000).toISOString();
    return {
      timestamp,
      message: `line ${i}`,
      cursor: timestamp,
      source: {
        metadata: {
          region: 'region-1',
          zone: 'az-1',
          os: 'linux',
          arch: 'amd64',
          node: 'node-1',
        },
        namespace: 'ns',
        podName: 'pod-1',
        containerName: 'container-1',
      },
    };
  });
}

function createMockRuntime(): Runtime {
  const recordsRef = { current: new DoubleTailedArray<LogRecordInternal>() };
  const setCount = vi.fn();

  const { result } = renderHook(() => useRecordStore({ recordsRef, setCount }));

  return {
    client: createMockClient(),
    config: {
      initialPosition: { type: 'head' },
      follow: false,
      overscan: 0,
      batchSizeInitial: 0,
      batchSizeRegular: 0,
      loadMoreThreshold: 0,
      pinToBottomTolerance: 0,
      hasMoreBeforeRowHeight: 0,
      hasMoreAfterRowHeight: 0,
      isRefreshingRowHeight: 0,
      estimateRowHeight: () => 24,
    },
    state: {
      count: 0,
      hasMoreBefore: false,
      hasMoreAfter: false,
      isLoading: false,
      isRefreshing: false,
      isRemeasuring: false,
    },
    refs: {
      scrollEl: { current: document.createElement('div') },
      isLoadingBefore: { current: false },
      isLoadingAfter: { current: false },
      isAutoScrollEnabled: { current: false },
      isProgrammaticScroll: { current: false },
    },
    actions: {
      setCount,
      setIsLoading: vi.fn(),
      setHasMoreBefore: vi.fn(),
      setHasMoreAfter: vi.fn(),
      setIsRefreshing: vi.fn(),
    },
    services: {
      recordStore: result.current,
      beforePaint: vi.fn(),
      virtualizer: {} as Virtualizer<HTMLDivElement, Element>,
    },
  };
}

describe('internal helpers', () => {
  describe('useRecordStore', () => {
    function createMockRecordStore() {
      const recordsRef = { current: new DoubleTailedArray<LogRecordInternal>() };
      const setCount = vi.fn();
      const { result } = renderHook(() => useRecordStore({ recordsRef, setCount }));

      return {
        recordsRef,
        setCount,
        recordStore: result.current,
      };
    }

    describe('new()', () => {
      it('initializes new array on first call', () => {
        const { recordsRef, setCount, recordStore } = createMockRecordStore();

        act(() => {
          recordStore.new(createMockRecords(2));
        });

        expect(setCount).toHaveBeenCalledTimes(1);
        expect(setCount).toHaveBeenCalledWith(2);
        expect(recordsRef.current.at(0).key).toBe(0);
        expect(recordsRef.current.at(1).key).toBe(1);
      });

      it('resets array and count when called again', () => {
        const { recordsRef, setCount, recordStore } = createMockRecordStore();
        const records = createMockRecords(2);

        // Call twice
        act(() => {
          recordStore.new(records);
          recordStore.new(records);
        });

        expect(setCount).toHaveBeenCalledTimes(2);
        expect(setCount).toHaveBeenCalledWith(2);
        expect(recordsRef.current.length).toBe(2);
        expect(recordsRef.current.at(0).key).toBe(2);
        expect(recordsRef.current.at(1).key).toBe(3);
      });

      it('handles skipSetCount option', () => {
        const { recordsRef, setCount, recordStore } = createMockRecordStore();

        act(() => {
          recordStore.new(createMockRecords(2), true);
        });

        expect(setCount).toHaveBeenCalledTimes(0);
        expect(recordsRef.current.length).toBe(2);
      });
    });

    it('append() adds keys and updates count properly', () => {
      const { recordsRef, setCount, recordStore } = createMockRecordStore();

      act(() => {
        recordStore.append(createMockRecords(2));
      });

      expect(setCount).toHaveBeenCalledTimes(1);
      expect(setCount).toHaveBeenCalledWith(2);
      expect(recordsRef.current.length).toBe(2);
      expect(recordsRef.current.at(0).key).toBe(0);
      expect(recordsRef.current.at(1).key).toBe(1);
    });

    it('prepend() adds keys and updates count properly', () => {
      const { recordsRef, setCount, recordStore } = createMockRecordStore();

      act(() => {
        recordStore.prepend(createMockRecords(2));
      });

      expect(setCount).toHaveBeenCalledTimes(1);
      expect(setCount).toHaveBeenCalledWith(2);
      expect(recordsRef.current.length).toBe(2);
      expect(recordsRef.current.at(0).key).toBe(0);
      expect(recordsRef.current.at(1).key).toBe(1);
    });
  });

  describe('useInit', () => {
    beforeEach(() => vi.useFakeTimers());

    afterEach(() => vi.useRealTimers());

    it('is idempotent', () => {
      const runtime = createMockRuntime();
      const { rerender } = renderHook(() => useInit(runtime));
      rerender();
      expect(runtime.actions.setIsLoading).toHaveBeenCalledTimes(1);
      expect(runtime.actions.setIsLoading).toHaveBeenCalledWith(true);
    });

    describe('head', () => {
      it('calls fetchSince with empty cursor', () => {
        const runtime = createMockRuntime();
        const { client, config } = runtime;

        (config as any).initialPosition = { type: 'head' };

        renderHook(() => useInit(runtime));

        expect(client.fetchSince).toHaveBeenCalledTimes(1);
        const callArgs = (client as MockClient).fetchBefore.mock.calls[0]?.[0];
        expect(callArgs?.cursor).toBeUndefined();
      });

      it('sets isLoading to true/false before/after', async () => {
        const runtime = createMockRuntime();
        const { client, config, actions } = runtime;

        (config as any).initialPosition = { type: 'head' };

        (client as MockClient).fetchSince.mockResolvedValue({
          records: createMockRecords(2),
          nextCursor: null,
        });

        renderHook(() => useInit(runtime));
        expect(actions.setIsLoading).toHaveBeenCalledTimes(1);
        expect(actions.setIsLoading).toHaveBeenCalledWith(true);
        expect(client.fetchSince).toHaveBeenCalledTimes(1);

        await vi.runAllTimersAsync();

        expect(actions.setIsLoading).toHaveBeenCalledTimes(2);
        expect(actions.setIsLoading).toHaveBeenLastCalledWith(false);
      });

      it('leaves hasMoreAfter as false when nextCursor is null', async () => {
        const runtime = createMockRuntime();
        const { client, config, actions } = runtime;

        (config as any).initialPosition = { type: 'head' };

        (client as MockClient).fetchSince.mockResolvedValue({
          records: createMockRecords(2),
          nextCursor: null,
        });

        renderHook(() => useInit(runtime));

        await vi.runAllTimersAsync();

        expect(actions.setHasMoreAfter).toHaveBeenCalledTimes(0);
      });

      it('sets hasMoreAfter to true when nextCursor is not-null', async () => {
        const runtime = createMockRuntime();
        const { client, config, actions } = runtime;

        (config as any).initialPosition = { type: 'head' };

        (client as MockClient).fetchSince.mockResolvedValue({
          records: createMockRecords(2),
          nextCursor: 'placeholder',
        });

        renderHook(() => useInit(runtime));

        await vi.runAllTimersAsync();

        expect(actions.setHasMoreAfter).toHaveBeenCalledTimes(1);
        expect(actions.setHasMoreAfter).toHaveBeenLastCalledWith(true);
      });
    });

    describe('tail', () => {
      it('calls fetchUntil with empty cursor', () => {
        const runtime = createMockRuntime();
        const { client, config } = runtime;

        (config as any).initialPosition = { type: 'tail' };

        renderHook(() => useInit(runtime));

        expect(client.fetchUntil).toHaveBeenCalledTimes(1);
        const callArgs = (client as MockClient).fetchUntil.mock.calls[0]?.[0];
        expect(callArgs?.cursor).toBeUndefined();
      });

      it('sets isLoading to true/false before/after', async () => {
        const runtime = createMockRuntime();
        const { client, config, actions } = runtime;

        (config as any).initialPosition = { type: 'tail' };

        (client as MockClient).fetchUntil.mockResolvedValue({
          records: createMockRecords(2),
          nextCursor: null,
        });

        renderHook(() => useInit(runtime));
        expect(actions.setIsLoading).toHaveBeenCalledTimes(1);
        expect(actions.setIsLoading).toHaveBeenCalledWith(true);
        expect(client.fetchUntil).toHaveBeenCalledTimes(1);

        await vi.runAllTimersAsync();

        expect(actions.setIsLoading).toHaveBeenCalledTimes(2);
        expect(actions.setIsLoading).toHaveBeenLastCalledWith(false);
      });

      it('leaves hasMoreBefore as false when nextCursor is null', async () => {
        const runtime = createMockRuntime();
        const { client, config, actions } = runtime;

        (config as any).initialPosition = { type: 'tail' };

        (client as MockClient).fetchUntil.mockResolvedValue({
          records: createMockRecords(2),
          nextCursor: null,
        });

        renderHook(() => useInit(runtime));

        await vi.runAllTimersAsync();

        expect(actions.setHasMoreBefore).toHaveBeenCalledTimes(0);
      });

      it('sets hasMoreBefore to true when nextCursor is not-null', async () => {
        const runtime = createMockRuntime();
        const { client, config, actions } = runtime;

        (config as any).initialPosition = { type: 'tail' };

        (client as MockClient).fetchUntil.mockResolvedValue({
          records: createMockRecords(2),
          nextCursor: 'placeholder',
        });

        renderHook(() => useInit(runtime));

        await vi.runAllTimersAsync();

        expect(actions.setHasMoreBefore).toHaveBeenCalledTimes(1);
        expect(actions.setHasMoreBefore).toHaveBeenLastCalledWith(true);
      });
    });

    describe('cursor', () => {
      it('calls fetchBefore/fetchSince with expected cursor', () => {
        const runtime = createMockRuntime();
        const { client, config } = runtime;

        (config as any).initialPosition = { type: 'cursor', cursor: 'c0' };

        renderHook(() => useInit(runtime));

        expect(client.fetchBefore).toHaveBeenCalledTimes(1);
        const callArgs1 = (client as MockClient).fetchBefore.mock.calls[0]?.[0];
        expect(callArgs1?.cursor).toEqual('c0');

        expect(client.fetchSince).toHaveBeenCalledTimes(1);
        const callArgs2 = (client as MockClient).fetchSince.mock.calls[0]?.[0];
        expect(callArgs2?.cursor).toEqual('c0');
      });

      it('sets isLoading to true/false before/after', async () => {
        const runtime = createMockRuntime();
        const { client, config, actions } = runtime;

        (config as any).initialPosition = { type: 'cursor', cursor: 'c0' };

        (client as MockClient).fetchBefore.mockResolvedValue({
          records: createMockRecords(2),
          nextCursor: null,
        });

        (client as MockClient).fetchSince.mockResolvedValue({
          records: createMockRecords(2),
          nextCursor: null,
        });

        renderHook(() => useInit(runtime));
        expect(actions.setIsLoading).toHaveBeenCalledTimes(1);
        expect(actions.setIsLoading).toHaveBeenCalledWith(true);
        expect(client.fetchBefore).toHaveBeenCalledTimes(1);
        expect(client.fetchSince).toHaveBeenCalledTimes(1);

        await vi.runAllTimersAsync();

        expect(actions.setIsLoading).toHaveBeenCalledTimes(2);
        expect(actions.setIsLoading).toHaveBeenLastCalledWith(false);
      });

      it('leaves hasMoreX as false when nextCursors are null', async () => {
        const runtime = createMockRuntime();
        const { client, config, actions } = runtime;

        (config as any).initialPosition = { type: 'cursor', cursor: 'c0' };

        (client as MockClient).fetchBefore.mockResolvedValue({
          records: createMockRecords(2),
          nextCursor: null,
        });

        (client as MockClient).fetchSince.mockResolvedValue({
          records: createMockRecords(2),
          nextCursor: null,
        });

        renderHook(() => useInit(runtime));

        await vi.runAllTimersAsync();

        expect(actions.setHasMoreBefore).toHaveBeenCalledTimes(0);
        expect(actions.setHasMoreAfter).toHaveBeenCalledTimes(0);
      });

      it('sets hasMoreX to true when nextCursors are not-null', async () => {
        const runtime = createMockRuntime();
        const { client, config, actions } = runtime;

        (config as any).initialPosition = { type: 'cursor', cursor: 'c0' };

        (client as MockClient).fetchBefore.mockResolvedValue({
          records: createMockRecords(2),
          nextCursor: 'placeholder',
        });

        (client as MockClient).fetchSince.mockResolvedValue({
          records: createMockRecords(2),
          nextCursor: 'placeholder',
        });

        renderHook(() => useInit(runtime));

        await vi.runAllTimersAsync();

        expect(actions.setHasMoreBefore).toHaveBeenCalledTimes(1);
        expect(actions.setHasMoreBefore).toHaveBeenLastCalledWith(true);
        expect(actions.setHasMoreAfter).toHaveBeenCalledTimes(1);
        expect(actions.setHasMoreAfter).toHaveBeenLastCalledWith(true);
      });
    });
  });

  describe('useLoadMoreBefore', () => {
    beforeEach(() => vi.useFakeTimers());

    afterEach(() => vi.useRealTimers());

    it('calls fetchBefore with cursor from first record', async () => {
      const runtime = createMockRuntime();
      const { client, services } = runtime;
      const records = createMockRecords(2);

      act(() => {
        services.recordStore.new(records);
      });

      const { result } = renderHook(() => useLoadMoreBefore(runtime));

      await result.current();
      await vi.runAllTimersAsync();

      expect(client.fetchBefore).toHaveBeenCalledTimes(1);
      const callArgs = (client as MockClient).fetchBefore.mock.calls[0]?.[0];
      expect(callArgs?.cursor).toEqual(records[0].cursor);
    });

    it('doesnt alter hasMoreBefore when nextCursor is not-null', async () => {
      const runtime = createMockRuntime();
      const { client, services, actions } = runtime;

      act(() => {
        services.recordStore.new(createMockRecords(2));
      });

      (client as MockClient).fetchBefore.mockResolvedValue({
        records: createMockRecords(2),
        nextCursor: 'placeholder',
      });

      const { result } = renderHook(() => useLoadMoreBefore(runtime));

      await result.current();
      await vi.runAllTimersAsync();

      expect(actions.setHasMoreBefore).toHaveBeenCalledTimes(0);
    });

    it('sets hasMoreBefore to false when nextCursor is null', async () => {
      const runtime = createMockRuntime();
      const { client, services, actions } = runtime;

      act(() => {
        services.recordStore.new(createMockRecords(2));
      });

      (client as MockClient).fetchBefore.mockResolvedValue({
        records: createMockRecords(2),
        nextCursor: null,
      });

      const { result } = renderHook(() => useLoadMoreBefore(runtime));

      await result.current();
      await vi.runAllTimersAsync();

      expect(actions.setHasMoreBefore).toHaveBeenCalledTimes(1);
      expect(actions.setHasMoreBefore).toHaveBeenCalledWith(false);
    });
  });

  describe('useLoadMoreAfter', () => {
    beforeEach(() => vi.useFakeTimers());

    afterEach(() => vi.useRealTimers());

    it('calls fetchAfter with cursor from last record', async () => {
      const runtime = createMockRuntime();
      const { client, services } = runtime;
      const records = createMockRecords(2);

      act(() => {
        services.recordStore.new(records);
      });

      const { result } = renderHook(() => useLoadMoreAfter(runtime));

      await result.current();
      await vi.runAllTimersAsync();

      expect(client.fetchAfter).toHaveBeenCalledTimes(1);
      const callArgs = (client as MockClient).fetchAfter.mock.calls[0]?.[0];
      expect(callArgs?.cursor).toEqual(records[1].cursor);
    });

    it('doesnt alter hasMoreAfter when nextCursor is not-null', async () => {
      const runtime = createMockRuntime();
      const { client, services, actions } = runtime;

      act(() => {
        services.recordStore.new(createMockRecords(2));
      });

      (client as MockClient).fetchAfter.mockResolvedValue({
        records: createMockRecords(2),
        nextCursor: 'placeholder',
      });

      const { result } = renderHook(() => useLoadMoreAfter(runtime));

      await result.current();
      await vi.runAllTimersAsync();

      expect(actions.setHasMoreAfter).toHaveBeenCalledTimes(0);
    });

    it('sets hasMoreAfter to false when nextCursor is null', async () => {
      const runtime = createMockRuntime();
      const { client, services, actions } = runtime;

      act(() => {
        services.recordStore.new(createMockRecords(2));
      });

      (client as MockClient).fetchBefore.mockResolvedValue({
        records: createMockRecords(2),
        nextCursor: null,
      });

      const { result } = renderHook(() => useLoadMoreAfter(runtime));

      await result.current();
      await vi.runAllTimersAsync();

      expect(actions.setHasMoreAfter).toHaveBeenCalledTimes(1);
      expect(actions.setHasMoreAfter).toHaveBeenCalledWith(false);
    });
  });

  describe('useLoadMore', () => {
    beforeEach(() => {
      vi.stubGlobal('requestAnimationFrame', (callback: FrameRequestCallback) => window.setTimeout(callback, 0));
    });

    afterEach(() => {
      vi.unstubAllGlobals();
      vi.restoreAllMocks();
    });

    it('noop when virtualizer range is outside of threshold', () => {
      const runtime = createMockRuntime();
      const { client } = runtime;

      // Prepare runtime
      (runtime as any).config = {
        ...runtime.config,
        overscan: 2,
        loadMoreThreshold: 5,
      };

      (runtime as any).state = {
        count: 100,
        hasMoreBefore: true,
        hasMoreAfter: true,
      };

      (runtime.services.virtualizer as any).range = { startIndex: 10, endIndex: 20 };

      renderHook(() => useLoadMore(runtime));

      expect(client.fetchBefore).toHaveBeenCalledTimes(0);
      expect(client.fetchAfter).toHaveBeenCalledTimes(0);
    });

    it('calls loadMoreBefore() when virtualizer range startIndex is within threshold', async () => {
      const runtime = createMockRuntime();
      const { client, services, refs } = runtime;

      act(() => {
        services.recordStore.new(createMockRecords(2));
      });

      // Prepare runtime
      (runtime as any).config = {
        ...runtime.config,
        overscan: 2,
        loadMoreThreshold: 5,
      };

      (runtime as any).state = {
        count: 100,
        hasMoreBefore: true,
        hasMoreAfter: true,
      };

      (services.virtualizer as any).range = { startIndex: 2, endIndex: 20 };

      // Call hook and perform checks
      renderHook(() => useLoadMore(runtime));
      await waitFor(() => expect(client.fetchBefore).toHaveBeenCalledTimes(1));
      expect(client.fetchAfter).toHaveBeenCalledTimes(0);
      expect(refs.isLoadingBefore.current).toEqual(true);
    });

    it('calls loadMoreAfter() when virtualizer range endIndex is within threshold', async () => {
      const runtime = createMockRuntime();
      const { client, services, refs } = runtime;

      act(() => {
        services.recordStore.new(createMockRecords(2));
      });

      // Prepare runtime
      (runtime as any).config = {
        ...runtime.config,
        overscan: 2,
        loadMoreThreshold: 5,
      };

      (runtime as any).state = {
        count: 100,
        hasMoreBefore: true,
        hasMoreAfter: true,
      };

      (runtime.services.virtualizer as any).range = { startIndex: 10, endIndex: 97 };

      // Call hook and perform checks
      renderHook(() => useLoadMore(runtime));
      await waitFor(() => expect(client.fetchAfter).toHaveBeenCalledTimes(1));
      expect(client.fetchBefore).toHaveBeenCalledTimes(0);
      expect(refs.isLoadingAfter.current).toEqual(true);
    });

    it('noop when isLoading is true', async () => {
      const runtime = createMockRuntime();
      const { client, services } = runtime;

      act(() => {
        services.recordStore.new(createMockRecords(2));
      });

      // Prepare runtime
      (runtime as any).config = {
        ...runtime.config,
        overscan: 2,
        loadMoreThreshold: 20,
      };

      (runtime as any).state = {
        count: 100,
        hasMoreBefore: true,
        hasMoreAfter: true,
        isLoading: true,
        isRemeasuring: false,
      };

      (runtime.services.virtualizer as any).range = { startIndex: 10, endIndex: 90 };

      // Call hook and perform checks
      renderHook(() => useLoadMore(runtime));
      expect(client.fetchBefore).toHaveBeenCalledTimes(0);
      expect(client.fetchAfter).toHaveBeenCalledTimes(0);
    });

    it('noop when isRemeasuring is true', async () => {
      const runtime = createMockRuntime();
      const { client, services } = runtime;

      act(() => {
        services.recordStore.new(createMockRecords(2));
      });

      // Prepare runtime
      (runtime as any).config = {
        ...runtime.config,
        ocerscan: 2,
        loadMoreThreshold: 20,
      };

      (runtime as any).state = {
        count: 100,
        hasMoreBefore: true,
        hasMoreAfter: true,
        isLoading: false,
        isRemeasuring: true,
      };

      (runtime.services.virtualizer as any).range = { startIndex: 10, endIndex: 90 };

      // Call hook and perform checks
      renderHook(() => useLoadMore(runtime));
      expect(client.fetchBefore).toHaveBeenCalledTimes(0);
      expect(client.fetchAfter).toHaveBeenCalledTimes(0);
    });
  });

  describe('useFollowFromEnd', () => {
    beforeEach(() => {
      vi.useFakeTimers();
      vi.stubGlobal('requestAnimationFrame', (callback: FrameRequestCallback) => window.setTimeout(callback, 0));
    });

    afterEach(() => {
      vi.useRealTimers();
      vi.unstubAllGlobals();
      vi.restoreAllMocks();
    });

    it('subscribes from last cursor and appends records', async () => {
      const runtime = createMockRuntime();
      const { client, refs, services } = runtime;
      const records = createMockRecords(1);

      act(() => {
        services.recordStore.new(records);
      });

      (runtime as any).config = {
        ...runtime.config,
        follow: true,
      };

      (runtime as any).state = {
        ...runtime.state,
        count: 1,
        hasMoreAfter: false,
        isLoading: false,
      };

      Object.defineProperties(refs.scrollEl.current, {
        scrollTop: { value: 0, writable: true },
        scrollHeight: { value: 100, writable: true },
      });

      refs.isAutoScrollEnabled.current = true;

      services.beforePaint = (cb) => {
        cb();
        return Promise.resolve();
      };

      renderHook(() => useFollowFromEnd(runtime));

      expect(client.subscribe).toHaveBeenCalledTimes(1);
      const callArgs = (client as MockClient).subscribe.mock.calls[0];
      expect(callArgs?.[1]).toEqual({ after: records[0].cursor });

      callArgs?.[0]?.(createMockRecords(1, 1000)[0]);
      await vi.runAllTimersAsync();

      expect(services.recordStore.length()).toBe(2);
      expect(refs.scrollEl.current?.scrollTop).toBe(100);
    });
  });

  describe('useAutoScroll', () => {
    it('disables on scroll up', () => {
      const runtime = createMockRuntime();
      const { refs } = runtime;

      (runtime as any).config = {
        ...runtime.config,
        follow: true,
        pinToBottomTolerance: 0,
      };

      (runtime as any).state = {
        ...runtime.state,
        isLoading: false,
        hasMoreAfter: false,
      };

      Object.defineProperties(refs.scrollEl.current, {
        scrollTop: { value: 90, writable: true },
        clientHeight: { value: 10, writable: true },
        scrollHeight: { value: 100, writable: true },
      });

      refs.isAutoScrollEnabled.current = true;
      renderHook(() => useAutoScroll(runtime));

      refs.scrollEl.current?.dispatchEvent(new Event('scroll'));
      if (refs.scrollEl.current) refs.scrollEl.current.scrollTop = 80;
      refs.scrollEl.current?.dispatchEvent(new Event('scroll'));

      expect(refs.isAutoScrollEnabled.current).toBe(false);
    });

    it('noop on programmatic scroll', () => {
      const runtime = createMockRuntime();
      const { refs } = runtime;
      refs.isProgrammaticScroll.current = true;

      (runtime as any).config = {
        ...runtime.config,
        follow: true,
        pinToBottomTolerance: 0,
      };

      (runtime as any).state = {
        ...runtime.state,
        isLoading: false,
        hasMoreAfter: false,
      };

      Object.defineProperties(refs.scrollEl.current, {
        scrollTop: { value: 90, writable: true },
        clientHeight: { value: 10, writable: true },
        scrollHeight: { value: 100, writable: true },
      });

      refs.isAutoScrollEnabled.current = true;
      renderHook(() => useAutoScroll(runtime));

      refs.scrollEl.current?.dispatchEvent(new Event('scroll'));
      if (refs.scrollEl.current) refs.scrollEl.current.scrollTop = 80;
      refs.scrollEl.current?.dispatchEvent(new Event('scroll'));

      expect(refs.isAutoScrollEnabled.current).toBe(true);
    });
  });

  describe('usePullToRefresh', () => {
    beforeEach(() => {
      vi.useFakeTimers();
      vi.stubGlobal('requestAnimationFrame', (callback: FrameRequestCallback) => window.setTimeout(callback, 0));
    });

    afterEach(() => {
      vi.useRealTimers();
      vi.unstubAllGlobals();
      vi.restoreAllMocks();
    });

    it('triggers refresh when wheel down at bottom', async () => {
      const runtime = createMockRuntime();
      const { client, refs, services, actions } = runtime;

      act(() => {
        services.recordStore.new(createMockRecords(1));
      });

      (runtime as any).config = {
        ...runtime.config,
        follow: false,
        pinToBottomTolerance: 0,
      };

      (runtime as any).state = {
        ...runtime.state,
        count: 1,
        hasMoreAfter: false,
        isLoading: false,
      };

      Object.defineProperties(refs.scrollEl.current, {
        scrollTop: { value: 90, writable: true },
        clientHeight: { value: 10, writable: true },
        scrollHeight: { value: 100, writable: true },
      });

      renderHook(() => usePullToRefresh(runtime));

      refs.scrollEl.current?.dispatchEvent(new WheelEvent('wheel', { deltaY: 20 }));
      await vi.runAllTimersAsync();

      expect(actions.setIsRefreshing).toHaveBeenCalledWith(true);
      expect(client.fetchAfter).toHaveBeenCalledTimes(1);
    });
  });
});

describe('LogViewerInner', () => {
  beforeEach(() => {
    vi.stubGlobal('requestAnimationFrame', (callback: FrameRequestCallback) => window.setTimeout(callback, 0));
    vi.stubGlobal('cancelAnimationFrame', (id: number) => window.clearTimeout(id));
    mockVirtualizer.getTotalSize.mockReturnValue(0);
    mockVirtualizer.getVirtualItems.mockReturnValue([]);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.clearAllMocks();
  });

  it('exposes virtual rows with records', async () => {
    const runtime = createMockRuntime();

    (runtime.client as MockClient).fetchSince.mockResolvedValue({
      records: createMockRecords(1),
      nextCursor: null,
    });

    mockVirtualizer.getVirtualItems.mockReturnValue([{ key: 0, index: 0, size: 24, start: 0 }]);

    const virtualizerRef: { current: LogViewerVirtualizer | null } = { current: null };
    const internalVirtualizerRef = { current: null };

    render(
      <LogViewerInner
        client={runtime.client}
        config={runtime.config}
        isLoading={runtime.state.isLoading}
        setIsLoading={runtime.actions.setIsLoading}
        isRemeasuring={runtime.state.isRemeasuring}
        virtualizerRef={internalVirtualizerRef}
      >
        {(virtualizer) => {
          virtualizerRef.current = virtualizer;
          return null;
        }}
      </LogViewerInner>,
    );

    await waitFor(() => {
      expect(virtualizerRef.current?.getVirtualRows().length).toBe(1);
    });

    const row = virtualizerRef.current?.getVirtualRows()[0];
    expect(row?.record.message).toBe('line 0');
    expect(row?.key).toBe(0);
  });

  it('adds hasMoreAfterRowHeight to total size when more after', async () => {
    const client = createMockClient();

    client.fetchSince.mockResolvedValue({
      records: createMockRecords(1),
      nextCursor: 'c1',
    });

    mockVirtualizer.getTotalSize.mockReturnValue(100);

    const virtualizerRef: { current: LogViewerVirtualizer | null } = { current: null };
    const internalVirtualizerRef = { current: null };

    render(
      <LogViewerInner
        client={client}
        config={{
          initialPosition: { type: 'head' },
          follow: false,
          overscan: 0,
          batchSizeInitial: 50,
          batchSizeRegular: 50,
          loadMoreThreshold: 5,
          pinToBottomTolerance: 0,
          hasMoreBeforeRowHeight: 10,
          hasMoreAfterRowHeight: 25,
          isRefreshingRowHeight: 0,
          estimateRowHeight: () => 24,
        }}
        isLoading={false}
        setIsLoading={vi.fn()}
        isRemeasuring={false}
        virtualizerRef={internalVirtualizerRef}
      >
        {(virtualizer) => {
          virtualizerRef.current = virtualizer;
          return null;
        }}
      </LogViewerInner>,
    );

    await waitFor(() => {
      expect(virtualizerRef.current?.hasMoreAfter).toBe(true);
    });

    expect(virtualizerRef.current?.getTotalSize()).toBe(125);
  });
});

describe('LogViewer', () => {
  beforeEach(() => {
    vi.stubGlobal('requestAnimationFrame', (callback: FrameRequestCallback) => window.setTimeout(callback, 0));
    vi.stubGlobal('cancelAnimationFrame', (id: number) => window.clearTimeout(id));
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.clearAllMocks();
  });

  it('triggers fetchSince after jumpToBeginning', async () => {
    const client = createMockClient();
    const logViewerRef = createRef<LogViewerHandle>();

    render(
      <LogViewer ref={logViewerRef} client={client} estimateRowHeight={() => 24} initialPosition={{ type: 'head' }}>
        {() => null}
      </LogViewer>,
    );

    await waitFor(() => {
      expect(client.fetchSince).toHaveBeenCalledTimes(1);
    });

    act(() => {
      logViewerRef.current?.jumpToBeginning();
    });

    await waitFor(() => {
      expect(client.fetchSince).toHaveBeenCalledTimes(2);
    });
  });

  it('triggers fetchUntil after jumpToEnd', async () => {
    const client = createMockClient();
    const logViewerRef = createRef<LogViewerHandle>();

    render(
      <LogViewer ref={logViewerRef} client={client} estimateRowHeight={() => 24} initialPosition={{ type: 'head' }}>
        {() => null}
      </LogViewer>,
    );

    await waitFor(() => {
      expect(client.fetchSince).toHaveBeenCalledTimes(1);
    });

    act(() => {
      logViewerRef.current?.jumpToEnd();
    });

    await waitFor(() => {
      expect(client.fetchUntil).toHaveBeenCalledTimes(1);
    });
  });

  it('triggers fetchBefore and fetchSince after jumpToCursor', async () => {
    const client = createMockClient();
    const logViewerRef = createRef<LogViewerHandle>();

    render(
      <LogViewer ref={logViewerRef} client={client} estimateRowHeight={() => 24} initialPosition={{ type: 'head' }}>
        {() => null}
      </LogViewer>,
    );

    await waitFor(() => {
      expect(client.fetchSince).toHaveBeenCalledTimes(1);
    });

    act(() => {
      logViewerRef.current?.jumpToCursor('c0');
    });

    await waitFor(() => {
      expect(client.fetchSince).toHaveBeenCalledTimes(2);
      expect(client.fetchBefore).toHaveBeenCalledTimes(1);
    });

    const callArgs1 = client.fetchSince.mock.calls[1]?.[0];
    expect(callArgs1?.cursor).toEqual('c0');

    const callArgs2 = client.fetchBefore.mock.calls[0]?.[0];
    expect(callArgs2?.cursor).toEqual('c0');
  });

  it('re-initializes when the client changes', async () => {
    const clientA = createMockClient();
    const clientB = createMockClient();

    const { rerender } = render(
      <LogViewer client={clientA} estimateRowHeight={() => 24} initialPosition={{ type: 'head' }}>
        {() => null}
      </LogViewer>,
    );

    await waitFor(() => {
      expect(clientA.fetchSince).toHaveBeenCalledTimes(1);
    });

    rerender(
      <LogViewer client={clientB} estimateRowHeight={() => 24} initialPosition={{ type: 'head' }}>
        {() => null}
      </LogViewer>,
    );

    await waitFor(() => {
      expect(clientB.fetchSince).toHaveBeenCalledTimes(1);
    });
  });
});

describe('useLogViewerState', () => {
  it('returns initial state when ref is null', () => {
    const logViewerRef = { current: null } as RefObject<LogViewerHandle | null>;
    const { result } = renderHook(() => useLogViewerState(logViewerRef, []));
    expect(result.current.isLoading).toBe(true);
  });

  it('subscribes to external store updates', () => {
    let state = { isLoading: false };

    const listeners = new Set<() => void>();

    const handle = {
      subscribe: (callback: () => void) => {
        listeners.add(callback);
        return () => listeners.delete(callback);
      },
      getSnapshot: () => state,
    } as unknown as LogViewerHandle;

    const logViewerRef = { current: handle } as RefObject<LogViewerHandle | null>;

    const { result } = renderHook(() => useLogViewerState(logViewerRef, []));
    expect(result.current.isLoading).toBe(false);

    act(() => {
      state = { isLoading: true };
      listeners.forEach((callback) => callback());
    });

    expect(result.current.isLoading).toBe(true);
  });

  it('refreshes store when isLoading changes', async () => {
    const listenersA = new Set<() => void>();
    const listenersB = new Set<() => void>();

    const snapshotA = { isLoading: false };
    const snapshotB = { isLoading: true };

    const handleA = {
      subscribe: (callback: () => void) => {
        listenersA.add(callback);
        return () => listenersA.delete(callback);
      },
      getSnapshot: () => snapshotA,
    } as unknown as LogViewerHandle;

    const handleB = {
      subscribe: (callback: () => void) => {
        listenersB.add(callback);
        return () => listenersB.delete(callback);
      },
      getSnapshot: () => snapshotB,
    } as unknown as LogViewerHandle;

    const logViewerRef = { current: handleA } as RefObject<LogViewerHandle | null>;

    const { result, rerender } = renderHook(({ deps }) => useLogViewerState(logViewerRef, deps), {
      initialProps: { deps: [0] },
    });

    expect(result.current.isLoading).toBe(false);

    act(() => {
      logViewerRef.current = handleB;
    });
    rerender({ deps: [1] });

    await waitFor(() => {
      expect(result.current.isLoading).toBe(true);
    });
  });
});
