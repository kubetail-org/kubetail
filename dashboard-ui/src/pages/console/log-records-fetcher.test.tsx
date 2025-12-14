// Copyright 2024-2025 Andres Morey
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

import { act, render } from '@testing-library/react';
import { createStore, Provider } from 'jotai';
import { createRef } from 'react';
import { describe, expect, it, vi } from 'vitest';

import { LogRecordsQueryMode } from '@/lib/graphql/dashboard/__generated__/graphql';

import { LogRecordsFetcher, LogRecordsFetcherHandle } from './log-records-fetcher';
import { isFollowAtom } from './state';

const mockRefetch = vi.fn();
const mockSubscribeToMore = vi.fn();

vi.mock('@apollo/client', () => ({
  useQuery: vi.fn(() => ({
    refetch: mockRefetch,
    subscribeToMore: mockSubscribeToMore,
  })),
}));

describe('LogRecordsFetcher', () => {
  it('renders null', () => {
    const { container } = render(
      <LogRecordsFetcher
        kubeContext="test-context"
        sources={['test-source']}
        sourceFilter={{}}
        grep={null}
        onFollowData={() => {}}
      />,
    );

    expect(container.firstChild).toBeNull();
  });
});

describe('LogRecordsFetcherHandle', () => {
  it('fetch() calls refetch with HEAD mode', async () => {
    const ref = createRef<LogRecordsFetcherHandle>();

    mockRefetch.mockResolvedValue({
      data: {
        logRecordsFetch: {
          records: [
            { timestamp: '2024-01-01T00:00:00Z', message: 'log1' },
            { timestamp: '2024-01-01T00:00:01Z', message: 'log2' },
          ],
        },
      },
    });

    render(
      <LogRecordsFetcher
        ref={ref}
        kubeContext="test-context"
        sources={['test-source']}
        sourceFilter={{}}
        grep={null}
        onFollowData={() => {}}
      />,
    );

    const result = await act(async () =>
      ref.current?.fetch({
        mode: LogRecordsQueryMode.Head,
        since: '2024-01-01T00:00:00Z',
      }),
    );

    expect(mockRefetch).toHaveBeenCalledWith({
      mode: LogRecordsQueryMode.Head,
      since: '2024-01-01T00:00:00Z',
      after: undefined,
      before: undefined,
    });

    expect(result).toEqual({
      records: [
        { timestamp: '2024-01-01T00:00:00Z', message: 'log1' },
        { timestamp: '2024-01-01T00:00:01Z', message: 'log2' },
      ],
      nextCursor: null,
    });
  });

  it('fetch() calls refetch with TAIL mode', async () => {
    const ref = createRef<LogRecordsFetcherHandle>();

    mockRefetch.mockResolvedValue({
      data: {
        logRecordsFetch: {
          records: [
            { timestamp: '2024-01-01T00:00:00Z', message: 'log1' },
            { timestamp: '2024-01-01T00:00:01Z', message: 'log2' },
          ],
        },
      },
    });

    render(
      <LogRecordsFetcher
        ref={ref}
        kubeContext="test-context"
        sources={['test-source']}
        sourceFilter={{}}
        grep={null}
        onFollowData={() => {}}
      />,
    );

    const result = await act(async () =>
      ref.current?.fetch({
        mode: LogRecordsQueryMode.Tail,
      }),
    );

    expect(mockRefetch).toHaveBeenCalledWith({
      mode: LogRecordsQueryMode.Tail,
      after: undefined,
      before: undefined,
      since: undefined,
    });

    expect(result).toEqual({
      records: [
        { timestamp: '2024-01-01T00:00:00Z', message: 'log1' },
        { timestamp: '2024-01-01T00:00:01Z', message: 'log2' },
      ],
      nextCursor: null,
    });
  });

  it('fetch() returns nextCursor when more records available in HEAD mode', async () => {
    const ref = createRef<LogRecordsFetcherHandle>();

    // Create 301 records (batchSize is 300)
    const records = Array.from({ length: 301 }, (_, i) => ({
      timestamp: `2024-01-01T00:00:${String(i).padStart(2, '0')}Z`,
      message: `log${i}`,
    }));

    mockRefetch.mockResolvedValue({
      data: {
        logRecordsFetch: {
          records,
        },
      },
    });

    render(
      <LogRecordsFetcher
        ref={ref}
        kubeContext="test-context"
        sources={['test-source']}
        sourceFilter={{}}
        grep={null}
        onFollowData={() => {}}
      />,
    );

    const result = await act(async () =>
      ref.current?.fetch({
        mode: LogRecordsQueryMode.Head,
      }),
    );

    expect(result?.records).toHaveLength(300);
    expect(result?.nextCursor).toBe('2024-01-01T00:00:299Z');
  });

  it('reset() clears internal state', async () => {
    const ref = createRef<LogRecordsFetcherHandle>();

    mockRefetch.mockResolvedValue({
      data: {
        logRecordsFetch: {
          records: [{ timestamp: '2024-01-01T00:00:00Z', message: 'log1' }],
        },
      },
    });

    render(
      <LogRecordsFetcher
        ref={ref}
        kubeContext="test-context"
        sources={['test-source']}
        sourceFilter={{}}
        grep={null}
        onFollowData={() => {}}
      />,
    );

    // Fetch some data first
    await act(async () => ref.current?.fetch({ mode: LogRecordsQueryMode.Tail }));

    // Reset should not throw
    act(() => {
      ref.current?.reset();
    });
  });

  it('onFollowData is called when new records arrive in follow mode', async () => {
    const store = createStore();
    const ref = createRef<LogRecordsFetcherHandle>();
    const onFollowData = vi.fn();
    let updateQueryCallback: any;

    // Mock subscribeToMore to capture the updateQuery callback
    mockSubscribeToMore.mockImplementation((options) => {
      updateQueryCallback = options.updateQuery;
      return () => {}; // Return unsubscribe function
    });

    mockRefetch.mockResolvedValue({
      data: {
        logRecordsFetch: {
          records: [{ timestamp: '2024-01-01T00:00:00Z', message: 'log1' }],
        },
      },
    });

    render(
      <Provider store={store}>
        <LogRecordsFetcher
          ref={ref}
          kubeContext="test-context"
          sources={['test-source']}
          sourceFilter={{}}
          grep={null}
          onFollowData={onFollowData}
        />
      </Provider>,
    );

    // Fetch data in TAIL mode to reach the end
    await act(async () => ref.current?.fetch({ mode: LogRecordsQueryMode.Tail }));

    // Enable follow mode
    store.set(isFollowAtom, true);

    // Wait for subscribeToMore to be called
    expect(mockSubscribeToMore).toHaveBeenCalled();

    // Simulate new record arriving via subscription
    const newRecord = { timestamp: '2024-01-01T00:00:01Z', message: 'new log' };
    act(() => {
      updateQueryCallback(null, {
        subscriptionData: {
          data: {
            logRecordsFollow: newRecord,
          },
        },
      });
    });

    // Verify onFollowData was called with the new record
    expect(onFollowData).toHaveBeenCalledWith(newRecord);
    expect(onFollowData).toHaveBeenCalledTimes(1);
  });
});
