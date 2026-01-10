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

import { vi, type Mock } from 'vitest';

import { LOG_VIEWER_INITIAL_STATE } from './log-viewer';
import type { LogViewerHandle } from './log-viewer';
import type { Client } from './types';

/**
 * MockClient
 */

export type MockClient = {
  fetchSince: Mock<Client['fetchSince']>;
  fetchUntil: Mock<Client['fetchUntil']>;
  fetchAfter: Mock<Client['fetchAfter']>;
  fetchBefore: Mock<Client['fetchBefore']>;
  subscribe: Mock<Client['subscribe']>;
};

export function createMockClient(overrides: Partial<Client> = {}): MockClient {
  return {
    fetchSince: vi.fn(async () => ({ records: [], nextCursor: null })),
    fetchUntil: vi.fn(async () => ({ records: [], nextCursor: null })),
    fetchAfter: vi.fn(async () => ({ records: [], nextCursor: null })),
    fetchBefore: vi.fn(async () => ({ records: [], nextCursor: null })),
    subscribe: vi.fn(() => () => {}),
    ...overrides,
  } satisfies Client as MockClient;
}

/**
 * MockLogViewerHandle
 */

export type MockLogViewerHandle = {
  jumpToBeginning: Mock<LogViewerHandle['jumpToBeginning']>;
  jumpToEnd: Mock<LogViewerHandle['jumpToEnd']>;
  jumpToCursor: Mock<LogViewerHandle['jumpToCursor']>;
  measure: Mock<LogViewerHandle['measure']>;
  subscribe: Mock<LogViewerHandle['subscribe']>;
  getSnapshot: Mock<LogViewerHandle['getSnapshot']>;
};

export function createMockLogViewerHandle(overrides: Partial<LogViewerHandle> = {}): MockLogViewerHandle {
  return {
    jumpToBeginning: vi.fn(),
    jumpToEnd: vi.fn(),
    jumpToCursor: vi.fn(),
    measure: vi.fn(),
    subscribe: vi.fn(),
    getSnapshot: vi.fn().mockReturnValue(LOG_VIEWER_INITIAL_STATE),
    ...overrides,
  } satisfies LogViewerHandle as MockLogViewerHandle;
}
