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

import type { Virtualizer } from '@tanstack/react-virtual';
import { render, renderHook, act, waitFor } from '@testing-library/react';
import { createRef, type RefObject } from 'react';

import { DoubleTailedArray } from './double-tailed-array';
import {
  countToEvict,
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
import type { Runtime, LogViewerHandle, LogViewerVirtualizer } from './log-viewer';
import { createMockClient, type MockClient } from './mock';
import type { LogRecord } from './types';

const mockVirtualizer = {
  range: null,
  measurementsCache: [],
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

type MockMeasurements = { start: number; end: number }[];

function rowsOf(count: number, height: number, offset = 0): MockMeasurements {
  return Array.from({ length: count }, (_, i) => ({ start: offset + i * height, end: offset + (i + 1) * height }));
}

// Swaps in `after` when the code under test refreshes the measurements with `getTotalSize()`.
function createMockMeasuredVirtualizer(before: MockMeasurements, after: MockMeasurements) {
  const virtualizer = {
    range: null,
    measurementsCache: before,
    getTotalSize: () => {
      virtualizer.measurementsCache = after;
      return 0;
    },
  };
  return virtualizer as unknown as Virtualizer<HTMLDivElement, Element>;
}

function createDeferred<T>() {
  let resolve: (value: T) => void = () => {};
  const promise = new Promise<T>((r) => {
    resolve = r;
  });
  return { promise, resolve };
}

function createMockRuntime(): Runtime & { recordsRef: { current: DoubleTailedArray<LogRecord> } } {
  const recordsRef = { current: new DoubleTailedArray<LogRecord>() };
  const setCount = vi.fn();

  const { result } = renderHook(() => useRecordStore({ recordsRef, setCount }));

  return {
    recordsRef,
    client: createMockClient(),
    config: {
      initialPosition: { type: 'head' },
      follow: false,
      overscan: 0,
      batchSizeInitial: 0,
      batchSizeRegular: 0,
      loadMoreThreshold: 0,
      maxRecords: 1000,
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
      onLoaded: vi.fn(),
    },
    services: {
      recordStore: result.current,
      beforePaint: vi.fn(),
      virtualizer: {
        range: null,
        measurementsCache: [],
        getTotalSize: () => 0,
      } as unknown as Virtualizer<HTMLDivElement, Element>,
    },
  };
}

describe('internal helpers', () => {
  describe('useRecordStore', () => {
    function createMockRecordStore() {
      const recordsRef = { current: new DoubleTailedArray<LogRecord>() };
      const setCount = vi.fn();
      const { result } = renderHook(() => useRecordStore({ recordsRef, setCount }));

      return {
        recordsRef,
        setCount,
        recordStore: result.current,
      };
    }

    describe('new()', () => {
      it('initializes new array with last record at key 0 by default', () => {
        const { recordsRef, setCount, recordStore } = createMockRecordStore();

        act(() => {
          recordStore.new(createMockRecords(3));
        });

        expect(setCount).toHaveBeenCalledTimes(1);
        expect(setCount).toHaveBeenCalledWith(3);
        expect(recordsRef.current.keyAt(0)).toBe(-2);
        expect(recordsRef.current.keyAt(1)).toBe(-1);
        expect(recordsRef.current.keyAt(2)).toBe(0);
      });

      it('initializes new array with first record at key 0 when zeroAt is first', () => {
        const { recordsRef, setCount, recordStore } = createMockRecordStore();

        act(() => {
          recordStore.new(createMockRecords(3), { zeroAt: 'first' });
        });

        expect(setCount).toHaveBeenCalledTimes(1);
        expect(setCount).toHaveBeenCalledWith(3);
        expect(recordsRef.current.keyAt(0)).toBe(0);
        expect(recordsRef.current.keyAt(1)).toBe(1);
        expect(recordsRef.current.keyAt(2)).toBe(2);
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
        expect(recordsRef.current.keyAt(0)).toBe(-1);
        expect(recordsRef.current.keyAt(1)).toBe(0);
      });

      it('handles skipSetCount option', () => {
        const { recordsRef, setCount, recordStore } = createMockRecordStore();

        act(() => {
          recordStore.new(createMockRecords(2), { skipSetCount: true });
        });

        expect(setCount).toHaveBeenCalledTimes(0);
        expect(recordsRef.current.length).toBe(2);
      });
    });

    it('append() adds records and updates count properly', () => {
      const { recordsRef, setCount, recordStore } = createMockRecordStore();

      act(() => {
        recordStore.append(createMockRecords(2));
      });

      expect(setCount).toHaveBeenCalledTimes(1);
      expect(setCount).toHaveBeenCalledWith(2);
      expect(recordsRef.current.length).toBe(2);
    });

    it('prepend() adjusts keys and updates count properly', () => {
      const { recordsRef, setCount, recordStore } = createMockRecordStore();

      act(() => {
        recordStore.prepend(createMockRecords(2));
      });

      expect(setCount).toHaveBeenCalledTimes(1);
      expect(setCount).toHaveBeenCalledWith(2);
      expect(recordsRef.current.length).toBe(2);
      expect(recordsRef.current.keyAt(0)).toBe(-2);
      expect(recordsRef.current.keyAt(1)).toBe(-1);
    });

    it('dropFirst() removes records, keeps keys and updates count', () => {
      const { recordsRef, setCount, recordStore } = createMockRecordStore();
      const records = createMockRecords(3);

      act(() => {
        recordStore.new(records, { zeroAt: 'first' });
        recordStore.dropFirst(2);
      });

      expect(setCount).toHaveBeenLastCalledWith(1);
      expect(recordStore.first()).toBe(records[2]);
      expect(recordsRef.current.keyAt(0)).toBe(2);
    });

    it('dropLast() removes records, keeps keys and updates count', () => {
      const { recordsRef, setCount, recordStore } = createMockRecordStore();
      const records = createMockRecords(3);

      act(() => {
        recordStore.new(records, { zeroAt: 'first' });
        recordStore.dropLast(2);
      });

      expect(setCount).toHaveBeenLastCalledWith(1);
      expect(recordStore.last()).toBe(records[0]);
      expect(recordsRef.current.keyAt(0)).toBe(0);
    });
  });

  describe('countToEvict', () => {
    const base = { length: 100, added: 10, maxRecords: 100, loadMoreThreshold: 5 };

    it('evicts nothing under the cap', () => {
      expect(countToEvict({ ...base, end: 'first', added: 0, range: null })).toBe(0);
    });

    it('evicts the excess when nothing is on screen', () => {
      expect(countToEvict({ ...base, end: 'first', range: null })).toBe(10);
      expect(countToEvict({ ...base, end: 'last', added: 500, range: null })).toBe(100);
    });

    it('evicts the excess outside the visible range', () => {
      const range = { startIndex: 40, endIndex: 50 };
      expect(countToEvict({ ...base, end: 'first', range })).toBe(10);
      expect(countToEvict({ ...base, end: 'last', range })).toBe(10);
    });

    it('stops loadMoreThreshold rows short of the visible range', () => {
      expect(countToEvict({ ...base, end: 'first', range: { startIndex: 10, endIndex: 20 } })).toBe(4);
      expect(countToEvict({ ...base, end: 'last', range: { startIndex: 80, endIndex: 90 } })).toBe(3);
      expect(countToEvict({ ...base, end: 'first', range: { startIndex: 2, endIndex: 20 } })).toBe(0);
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

        runtime.config = { ...config, initialPosition: { type: 'head' } };

        renderHook(() => useInit(runtime));

        expect(client.fetchSince).toHaveBeenCalledTimes(1);
        const callArgs = (client as MockClient).fetchBefore.mock.calls[0]?.[0];
        expect(callArgs?.cursor).toBeUndefined();
      });

      it('sets isLoading to true/false before/after', async () => {
        const runtime = createMockRuntime();
        const { client, config, actions } = runtime;

        runtime.config = { ...config, initialPosition: { type: 'head' } };

        (client as MockClient).fetchSince.mockResolvedValue({
          records: createMockRecords(2),
          nextCursor: null,
        });

        renderHook(() => useInit(runtime));
        expect(actions.setIsLoading).toHaveBeenCalledTimes(1);
        expect(actions.setIsLoading).toHaveBeenCalledWith(true);
        expect(client.fetchSince).toHaveBeenCalledTimes(1);

        await act(() => vi.runAllTimersAsync());

        expect(actions.setIsLoading).toHaveBeenCalledTimes(2);
        expect(actions.setIsLoading).toHaveBeenLastCalledWith(false);
      });

      it('leaves hasMoreAfter as false when nextCursor is null', async () => {
        const runtime = createMockRuntime();
        const { client, config, actions } = runtime;

        runtime.config = { ...config, initialPosition: { type: 'head' } };

        (client as MockClient).fetchSince.mockResolvedValue({
          records: createMockRecords(2),
          nextCursor: null,
        });

        renderHook(() => useInit(runtime));

        await act(() => vi.runAllTimersAsync());

        expect(actions.setHasMoreAfter).toHaveBeenCalledTimes(0);
      });

      it('sets hasMoreAfter to true when nextCursor is not-null', async () => {
        const runtime = createMockRuntime();
        const { client, config, actions } = runtime;

        runtime.config = { ...config, initialPosition: { type: 'head' } };

        (client as MockClient).fetchSince.mockResolvedValue({
          records: createMockRecords(2),
          nextCursor: 'placeholder',
        });

        renderHook(() => useInit(runtime));

        await act(() => vi.runAllTimersAsync());

        expect(actions.setHasMoreAfter).toHaveBeenCalledTimes(1);
        expect(actions.setHasMoreAfter).toHaveBeenLastCalledWith(true);
      });
    });

    describe('tail', () => {
      it('calls fetchUntil with empty cursor', () => {
        const runtime = createMockRuntime();
        const { client, config } = runtime;

        runtime.config = { ...config, initialPosition: { type: 'tail' } };

        renderHook(() => useInit(runtime));

        expect(client.fetchUntil).toHaveBeenCalledTimes(1);
        const callArgs = (client as MockClient).fetchUntil.mock.calls[0]?.[0];
        expect(callArgs?.cursor).toBeUndefined();
      });

      it('sets isLoading to true/false before/after', async () => {
        const runtime = createMockRuntime();
        const { client, config, actions } = runtime;

        runtime.config = { ...config, initialPosition: { type: 'tail' } };

        (client as MockClient).fetchUntil.mockResolvedValue({
          records: createMockRecords(2),
          nextCursor: null,
        });

        renderHook(() => useInit(runtime));
        expect(actions.setIsLoading).toHaveBeenCalledTimes(1);
        expect(actions.setIsLoading).toHaveBeenCalledWith(true);
        expect(client.fetchUntil).toHaveBeenCalledTimes(1);

        await act(() => vi.runAllTimersAsync());

        expect(actions.setIsLoading).toHaveBeenCalledTimes(2);
        expect(actions.setIsLoading).toHaveBeenLastCalledWith(false);
      });

      it('leaves hasMoreBefore as false when nextCursor is null', async () => {
        const runtime = createMockRuntime();
        const { client, config, actions } = runtime;

        runtime.config = { ...config, initialPosition: { type: 'tail' } };

        (client as MockClient).fetchUntil.mockResolvedValue({
          records: createMockRecords(2),
          nextCursor: null,
        });

        renderHook(() => useInit(runtime));

        await act(() => vi.runAllTimersAsync());

        expect(actions.setHasMoreBefore).toHaveBeenCalledTimes(0);
      });

      it('sets hasMoreBefore to true when nextCursor is not-null', async () => {
        const runtime = createMockRuntime();
        const { client, config, actions } = runtime;

        runtime.config = { ...config, initialPosition: { type: 'tail' } };

        (client as MockClient).fetchUntil.mockResolvedValue({
          records: createMockRecords(2),
          nextCursor: 'placeholder',
        });

        renderHook(() => useInit(runtime));

        await act(() => vi.runAllTimersAsync());

        expect(actions.setHasMoreBefore).toHaveBeenCalledTimes(1);
        expect(actions.setHasMoreBefore).toHaveBeenLastCalledWith(true);
      });
    });

    describe('cursor', () => {
      it('calls fetchBefore/fetchSince with expected cursor', () => {
        const runtime = createMockRuntime();
        const { client, config } = runtime;

        runtime.config = { ...config, initialPosition: { type: 'cursor', cursor: 'c0' } };

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

        runtime.config = { ...config, initialPosition: { type: 'cursor', cursor: 'c0' } };

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

        await act(() => vi.runAllTimersAsync());

        expect(actions.setIsLoading).toHaveBeenCalledTimes(2);
        expect(actions.setIsLoading).toHaveBeenLastCalledWith(false);
      });

      it('leaves hasMoreX as false when nextCursors are null', async () => {
        const runtime = createMockRuntime();
        const { client, config, actions } = runtime;

        runtime.config = { ...config, initialPosition: { type: 'cursor', cursor: 'c0' } };

        (client as MockClient).fetchBefore.mockResolvedValue({
          records: createMockRecords(2),
          nextCursor: null,
        });

        (client as MockClient).fetchSince.mockResolvedValue({
          records: createMockRecords(2),
          nextCursor: null,
        });

        renderHook(() => useInit(runtime));

        await act(() => vi.runAllTimersAsync());

        expect(actions.setHasMoreBefore).toHaveBeenCalledTimes(0);
        expect(actions.setHasMoreAfter).toHaveBeenCalledTimes(0);
      });

      it('sets hasMoreX to true when nextCursors are not-null', async () => {
        const runtime = createMockRuntime();
        const { client, config, actions } = runtime;

        runtime.config = { ...config, initialPosition: { type: 'cursor', cursor: 'c0' } };

        (client as MockClient).fetchBefore.mockResolvedValue({
          records: createMockRecords(2),
          nextCursor: 'placeholder',
        });

        (client as MockClient).fetchSince.mockResolvedValue({
          records: createMockRecords(2),
          nextCursor: 'placeholder',
        });

        renderHook(() => useInit(runtime));

        await act(() => vi.runAllTimersAsync());

        expect(actions.setHasMoreBefore).toHaveBeenCalledTimes(1);
        expect(actions.setHasMoreBefore).toHaveBeenLastCalledWith(true);
        expect(actions.setHasMoreAfter).toHaveBeenCalledTimes(1);
        expect(actions.setHasMoreAfter).toHaveBeenLastCalledWith(true);
      });

      it('assigns key 0 to the first record after the cursor', async () => {
        const runtime = createMockRuntime();
        const { client, config, recordsRef } = runtime;

        runtime.config = { ...config, initialPosition: { type: 'cursor', cursor: 'c0' } };

        const beforeRecords = createMockRecords(3);
        const afterRecords = createMockRecords(2);

        (client as MockClient).fetchBefore.mockResolvedValue({
          records: beforeRecords,
          nextCursor: null,
        });

        (client as MockClient).fetchSince.mockResolvedValue({
          records: afterRecords,
          nextCursor: null,
        });

        renderHook(() => useInit(runtime));

        await act(() => vi.runAllTimersAsync());

        // First "since" record should be the anchor (key 0)
        const firstSinceIndex = beforeRecords.length;
        expect(recordsRef.current.keyAt(firstSinceIndex)).toBe(0);

        // "Before" records should have negative keys
        expect(recordsRef.current.keyAt(0)).toBe(-beforeRecords.length);

        // Second "since" record should be key 1
        expect(recordsRef.current.keyAt(firstSinceIndex + 1)).toBe(1);
      });
    });
  });

  describe('useInit onLoaded', () => {
    beforeEach(() => vi.useFakeTimers());

    afterEach(() => vi.useRealTimers());

    it('calls onLoaded with isLoading turned off once loading finishes', async () => {
      const runtime = createMockRuntime();
      const { client, actions } = runtime;
      const deferred = createDeferred<{ records: LogRecord[]; nextCursor: null }>();

      (client as MockClient).fetchSince.mockReturnValue(deferred.promise);

      renderHook(() => useInit(runtime));

      await act(() => vi.runAllTimersAsync());
      expect(actions.onLoaded).not.toHaveBeenCalled();

      deferred.resolve({ records: createMockRecords(2), nextCursor: null });
      await act(() => vi.runAllTimersAsync());

      expect(actions.onLoaded).toHaveBeenCalledTimes(1);
      expect(actions.setIsLoading).toHaveBeenLastCalledWith(false);
    });
  });

  describe('useLoadMoreBefore', () => {
    beforeEach(() => vi.useFakeTimers());

    afterEach(() => vi.useRealTimers());

    it('evicts records from the end over the cap and holds the scroll position', async () => {
      const runtime = createMockRuntime();
      const { client, services, actions, refs } = runtime;
      const records = createMockRecords(10, 10_000);

      act(() => {
        services.recordStore.new(records);
      });

      runtime.config = { ...runtime.config, maxRecords: 10 };
      services.beforePaint = (cb) => {
        void cb();
        return Promise.resolve();
      };
      // Three 10px rows land above the first record, and the "more after" row goes away below
      services.virtualizer = createMockMeasuredVirtualizer(rowsOf(10, 10), rowsOf(10, 10));

      Object.defineProperty(refs.scrollEl.current, 'scrollTop', { value: 50, writable: true });

      (client as MockClient).fetchBefore.mockResolvedValue({
        records: createMockRecords(3),
        nextCursor: 'more',
      });

      const { result } = renderHook(() => useLoadMoreBefore(runtime));

      await act(async () => {
        await result.current();
        await vi.runAllTimersAsync();
      });

      expect(services.recordStore.length()).toBe(10);
      expect(services.recordStore.last()).toBe(records[6]);
      expect(actions.setHasMoreAfter).toHaveBeenCalledWith(true);
      expect(refs.scrollEl.current?.scrollTop).toBe(80);
    });

    it('calls fetchBefore with cursor from first record', async () => {
      const runtime = createMockRuntime();
      const { client, services } = runtime;
      const records = createMockRecords(2);

      act(() => {
        services.recordStore.new(records);
      });

      const { result } = renderHook(() => useLoadMoreBefore(runtime));

      await act(async () => {
        await result.current();
        await vi.runAllTimersAsync();
      });

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

      await act(async () => {
        await result.current();
        await vi.runAllTimersAsync();
      });

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

      await act(async () => {
        await result.current();
        await vi.runAllTimersAsync();
      });

      expect(actions.setHasMoreBefore).toHaveBeenCalledTimes(1);
      expect(actions.setHasMoreBefore).toHaveBeenCalledWith(false);
    });
  });

  describe('useLoadMoreAfter', () => {
    beforeEach(() => vi.useFakeTimers());

    afterEach(() => vi.useRealTimers());

    it('evicts records from the beginning over the cap and holds the scroll position', async () => {
      const runtime = createMockRuntime();
      const { client, services, actions, refs } = runtime;
      const records = createMockRecords(10);

      act(() => {
        services.recordStore.new(records);
      });

      runtime.config = { ...runtime.config, maxRecords: 10 };
      services.beforePaint = (cb) => {
        void cb();
        return Promise.resolve();
      };
      // After eviction the old fourth row is first, below a 20px "more before" row
      services.virtualizer = createMockMeasuredVirtualizer(rowsOf(10, 10), rowsOf(10, 10, 20));

      Object.defineProperty(refs.scrollEl.current, 'scrollTop', { value: 50, writable: true });

      (client as MockClient).fetchAfter.mockResolvedValue({
        records: createMockRecords(3, 10_000),
        nextCursor: 'more',
      });

      const { result } = renderHook(() => useLoadMoreAfter(runtime));

      await act(async () => {
        await result.current();
        await vi.runAllTimersAsync();
      });

      expect(services.recordStore.length()).toBe(10);
      expect(services.recordStore.first()).toBe(records[3]);
      expect(actions.setHasMoreBefore).toHaveBeenCalledWith(true);
      expect(refs.scrollEl.current?.scrollTop).toBe(40);
    });

    it('keeps every record under the cap', async () => {
      const runtime = createMockRuntime();
      const { client, services, actions } = runtime;

      act(() => {
        services.recordStore.new(createMockRecords(2));
      });

      (client as MockClient).fetchAfter.mockResolvedValue({
        records: createMockRecords(3, 10_000),
        nextCursor: 'more',
      });

      const { result } = renderHook(() => useLoadMoreAfter(runtime));

      await act(async () => {
        await result.current();
        await vi.runAllTimersAsync();
      });

      expect(services.recordStore.length()).toBe(5);
      expect(actions.setHasMoreBefore).not.toHaveBeenCalled();
    });

    it('calls fetchAfter with cursor from last record', async () => {
      const runtime = createMockRuntime();
      const { client, services } = runtime;
      const records = createMockRecords(2);

      act(() => {
        services.recordStore.new(records);
      });

      const { result } = renderHook(() => useLoadMoreAfter(runtime));

      await act(async () => {
        await result.current();
        await vi.runAllTimersAsync();
      });

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

      await act(async () => {
        await result.current();
        await vi.runAllTimersAsync();
      });

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

      await act(async () => {
        await result.current();
        await vi.runAllTimersAsync();
      });

      expect(actions.setHasMoreAfter).toHaveBeenCalledTimes(1);
      expect(actions.setHasMoreAfter).toHaveBeenCalledWith(false);
    });
  });

  describe('useLoadMore', () => {
    afterEach(() => {
      vi.restoreAllMocks();
    });

    it('noop when virtualizer range is outside of threshold', () => {
      const runtime = createMockRuntime();
      const { client } = runtime;

      // Prepare runtime
      runtime.config = {
        ...runtime.config,
        overscan: 2,
        loadMoreThreshold: 5,
      };

      runtime.state = {
        ...runtime.state,
        count: 100,
        hasMoreBefore: true,
        hasMoreAfter: true,
      };

      runtime.services.virtualizer.range = { startIndex: 10, endIndex: 20 };

      renderHook(() => useLoadMore(runtime));

      expect(client.fetchBefore).toHaveBeenCalledTimes(0);
      expect(client.fetchAfter).toHaveBeenCalledTimes(0);
    });

    it('calls loadMoreBefore() when virtualizer range startIndex is within threshold', async () => {
      const runtime = createMockRuntime();
      const { client, services } = runtime;

      act(() => {
        services.recordStore.new(createMockRecords(2));
      });

      // Prepare runtime
      runtime.config = {
        ...runtime.config,
        overscan: 2,
        loadMoreThreshold: 5,
      };

      runtime.state = {
        ...runtime.state,
        count: 100,
        hasMoreBefore: true,
        hasMoreAfter: true,
      };

      services.virtualizer.range = { startIndex: 2, endIndex: 20 };

      // Call hook and perform checks
      renderHook(() => useLoadMore(runtime));
      await waitFor(() => expect(client.fetchBefore).toHaveBeenCalledTimes(1));
      expect(client.fetchAfter).toHaveBeenCalledTimes(0);
    });

    it('calls loadMoreAfter() when virtualizer range endIndex is within threshold', async () => {
      const runtime = createMockRuntime();
      const { client, services } = runtime;

      act(() => {
        services.recordStore.new(createMockRecords(2));
      });

      // Prepare runtime
      runtime.config = {
        ...runtime.config,
        overscan: 2,
        loadMoreThreshold: 5,
      };

      runtime.state = {
        ...runtime.state,
        count: 100,
        hasMoreBefore: true,
        hasMoreAfter: true,
      };

      runtime.services.virtualizer.range = { startIndex: 10, endIndex: 97 };

      // Call hook and perform checks
      renderHook(() => useLoadMore(runtime));
      await waitFor(() => expect(client.fetchAfter).toHaveBeenCalledTimes(1));
      expect(client.fetchBefore).toHaveBeenCalledTimes(0);
    });

    it('noop when isLoading is true', async () => {
      const runtime = createMockRuntime();
      const { client, services } = runtime;

      act(() => {
        services.recordStore.new(createMockRecords(2));
      });

      // Prepare runtime
      runtime.config = {
        ...runtime.config,
        overscan: 2,
        loadMoreThreshold: 20,
      };

      runtime.state = {
        ...runtime.state,
        count: 100,
        hasMoreBefore: true,
        hasMoreAfter: true,
        isLoading: true,
        isRemeasuring: false,
      };

      runtime.services.virtualizer.range = { startIndex: 10, endIndex: 90 };

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
      runtime.config = {
        ...runtime.config,
        overscan: 2,
        loadMoreThreshold: 20,
      };

      runtime.state = {
        ...runtime.state,
        count: 100,
        hasMoreBefore: true,
        hasMoreAfter: true,
        isLoading: false,
        isRemeasuring: true,
      };

      runtime.services.virtualizer.range = { startIndex: 10, endIndex: 90 };

      // Call hook and perform checks
      renderHook(() => useLoadMore(runtime));
      expect(client.fetchBefore).toHaveBeenCalledTimes(0);
      expect(client.fetchAfter).toHaveBeenCalledTimes(0);
    });
  });

  describe('useFollowFromEnd', () => {
    beforeEach(() => {
      vi.useFakeTimers();
    });

    afterEach(() => {
      vi.useRealTimers();
      vi.restoreAllMocks();
    });

    it('subscribes from last cursor and appends records', async () => {
      const runtime = createMockRuntime();
      const { client, refs, services } = runtime;
      const records = createMockRecords(1);

      act(() => {
        services.recordStore.new(records);
      });

      runtime.config = {
        ...runtime.config,
        follow: true,
      };

      runtime.state = {
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
        void cb();
        return Promise.resolve();
      };

      renderHook(() => useFollowFromEnd(runtime));

      expect(client.subscribe).toHaveBeenCalledTimes(1);
      const callArgs = (client as MockClient).subscribe.mock.calls[0];
      expect(callArgs?.[1]).toEqual({ after: records[0].cursor });

      callArgs?.[0]?.(createMockRecords(1, 1000)[0]);
      await act(() => vi.runAllTimersAsync());

      expect(services.recordStore.length()).toBe(2);
      expect(refs.scrollEl.current?.scrollTop).toBe(100);
    });

    it('evicts records from the beginning over the cap', async () => {
      const runtime = createMockRuntime();
      const { client, refs, services, actions } = runtime;
      const records = createMockRecords(5);

      act(() => {
        services.recordStore.new(records);
      });

      runtime.config = { ...runtime.config, follow: true, maxRecords: 5 };
      runtime.state = { ...runtime.state, count: 5 };

      services.beforePaint = (cb) => {
        void cb();
        return Promise.resolve();
      };
      services.virtualizer = createMockMeasuredVirtualizer(rowsOf(5, 10), rowsOf(5, 10, 20));

      Object.defineProperty(refs.scrollEl.current, 'scrollTop', { value: 30, writable: true });
      refs.isAutoScrollEnabled.current = false;

      renderHook(() => useFollowFromEnd(runtime));

      const callArgs = (client as MockClient).subscribe.mock.calls[0];
      callArgs?.[0]?.(createMockRecords(1, 10_000)[0]);
      callArgs?.[0]?.(createMockRecords(1, 11_000)[0]);
      await act(() => vi.runAllTimersAsync());

      expect(services.recordStore.length()).toBe(5);
      expect(services.recordStore.first()).toBe(records[2]);
      expect(actions.setHasMoreBefore).toHaveBeenCalledWith(true);
      expect(refs.scrollEl.current?.scrollTop).toBe(30);
    });
  });

  describe('useAutoScroll', () => {
    it('disables on scroll up', () => {
      const runtime = createMockRuntime();
      const { refs } = runtime;

      runtime.config = {
        ...runtime.config,
        follow: true,
        pinToBottomTolerance: 0,
      };

      runtime.state = {
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

      runtime.config = {
        ...runtime.config,
        follow: true,
        pinToBottomTolerance: 0,
      };

      runtime.state = {
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

    it('disables on mousedown', () => {
      const runtime = createMockRuntime();
      const { refs } = runtime;

      runtime.state = {
        ...runtime.state,
        isLoading: false,
        hasMoreAfter: false,
      };

      refs.isAutoScrollEnabled.current = true;
      renderHook(() => useAutoScroll(runtime));

      refs.scrollEl.current?.dispatchEvent(new Event('mousedown'));

      expect(refs.isAutoScrollEnabled.current).toBe(false);
    });

    it('mousedown is noop when autoscroll already disabled', () => {
      const runtime = createMockRuntime();
      const { refs } = runtime;

      runtime.state = {
        ...runtime.state,
        isLoading: false,
        hasMoreAfter: false,
      };

      refs.isAutoScrollEnabled.current = false;
      renderHook(() => useAutoScroll(runtime));

      refs.scrollEl.current?.dispatchEvent(new Event('mousedown'));

      expect(refs.isAutoScrollEnabled.current).toBe(false);
    });
  });

  describe('usePullToRefresh', () => {
    beforeEach(() => {
      vi.useFakeTimers();
    });

    afterEach(() => {
      vi.useRealTimers();
      vi.restoreAllMocks();
    });

    it('triggers refresh when wheel down at bottom', async () => {
      const runtime = createMockRuntime();
      const { client, refs, services, actions } = runtime;

      act(() => {
        services.recordStore.new(createMockRecords(1));
      });

      runtime.config = {
        ...runtime.config,
        follow: false,
        pinToBottomTolerance: 0,
      };

      runtime.state = {
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
      await act(() => vi.runAllTimersAsync());

      expect(actions.setIsRefreshing).toHaveBeenCalledWith(true);
      expect(client.fetchAfter).toHaveBeenCalledTimes(1);
    });
  });
});

describe('LogViewerInner', () => {
  beforeEach(() => {
    mockVirtualizer.getTotalSize.mockReturnValue(0);
    mockVirtualizer.getVirtualItems.mockReturnValue([]);
  });

  afterEach(() => {
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
        onLoaded={runtime.actions.onLoaded}
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
          maxRecords: 1000,
          pinToBottomTolerance: 0,
          hasMoreBeforeRowHeight: 10,
          hasMoreAfterRowHeight: 25,
          isRefreshingRowHeight: 0,
          estimateRowHeight: () => 24,
        }}
        isLoading={false}
        setIsLoading={vi.fn()}
        onLoaded={vi.fn()}
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
  afterEach(() => {
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
      void logViewerRef.current?.jumpToBeginning();
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
      void logViewerRef.current?.jumpToEnd();
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
      void logViewerRef.current?.jumpToCursor('c0');
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

  it.each([
    ['jumpToBeginning', (h: LogViewerHandle) => h.jumpToBeginning(), 'fetchSince'],
    ['jumpToEnd', (h: LogViewerHandle) => h.jumpToEnd(), 'fetchUntil'],
    ['jumpToCursor', (h: LogViewerHandle) => h.jumpToCursor('c0'), 'fetchBefore'],
  ] as const)('%s resolves after loading finishes', async (_, jump, fetchMethod) => {
    const client = createMockClient();
    const logViewerRef = createRef<LogViewerHandle>();

    render(
      <LogViewer ref={logViewerRef} client={client} estimateRowHeight={() => 24} initialPosition={{ type: 'head' }}>
        {() => null}
      </LogViewer>,
    );

    await waitFor(() => {
      expect(logViewerRef.current?.getSnapshot().isLoading).toBe(false);
    });

    const deferred = createDeferred<{ records: LogRecord[]; nextCursor: null }>();
    client[fetchMethod].mockReturnValue(deferred.promise);

    let isResolved = false;
    act(() => {
      if (logViewerRef.current) {
        void jump(logViewerRef.current).then(() => {
          isResolved = true;
        });
      }
    });

    await waitFor(() => {
      expect(client[fetchMethod]).toHaveBeenCalledTimes(fetchMethod === 'fetchSince' ? 2 : 1);
    });
    expect(isResolved).toBe(false);
    expect(logViewerRef.current?.getSnapshot().isLoading).toBe(true);

    act(() => deferred.resolve({ records: createMockRecords(2), nextCursor: null }));

    await waitFor(() => {
      expect(isResolved).toBe(true);
    });
    expect(logViewerRef.current?.getSnapshot().isLoading).toBe(false);
  });

  it('resolves a pending jump on unmount', async () => {
    const client = createMockClient();
    const logViewerRef = createRef<LogViewerHandle>();

    const { unmount } = render(
      <LogViewer ref={logViewerRef} client={client} estimateRowHeight={() => 24} initialPosition={{ type: 'head' }}>
        {() => null}
      </LogViewer>,
    );

    await waitFor(() => {
      expect(logViewerRef.current?.getSnapshot().isLoading).toBe(false);
    });

    client.fetchUntil.mockReturnValue(new Promise(() => {}));

    let jumpPromise: Promise<void> | undefined;
    act(() => {
      jumpPromise = logViewerRef.current?.jumpToEnd();
    });

    unmount();
    await expect(jumpPromise).resolves.toBeUndefined();
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
