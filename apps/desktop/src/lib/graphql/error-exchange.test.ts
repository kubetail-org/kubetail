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

import { beforeEach, describe, expect, it, vi } from 'vitest';

import { onError, type AppError } from '@/lib/error-bus';

// Mocks ---------------------------------------------------------------

// The exchange's whole surface is the `onError` callback it hands urql, so capture
// the config rather than driving an operation stream through the real exchange.
const { mapExchangeMock } = vi.hoisted(() => ({ mapExchangeMock: vi.fn() }));
vi.mock('urql', () => ({ mapExchange: mapExchangeMock }));

await import('./error-exchange');

const report = mapExchangeMock.mock.calls[0][0].onError as (error: unknown, operation: { kind: string }) => void;

// Helpers -------------------------------------------------------------

let reported: AppError[];
let unsubscribe: () => void;

beforeEach(() => {
  reported = [];
  unsubscribe?.();
  unsubscribe = onError((err) => reported.push(err));
});

// Tests ---------------------------------------------------------------

describe('errorReportExchange', () => {
  it('reports a failed query on the bus', () => {
    report({ message: 'query failed' }, { kind: 'query' });
    expect(reported).toHaveLength(1);
    expect(reported[0]).toMatchObject({ source: 'graphql', message: 'query failed' });
  });

  it('prefers the network message, which names the actual failure', () => {
    // "sidecar unreachable" beats urql's "an error occurred".
    report(
      { message: '[Network] an error occurred', networkError: { message: 'sidecar unreachable' } },
      {
        kind: 'query',
      },
    );
    expect(reported[0].message).toBe('sidecar unreachable');
  });

  it('marks a subscription failure as one, so the banner softens its wording', () => {
    report({ message: 'watch died' }, { kind: 'subscription' });
    expect(reported[0].source).toBe('subscription');
  });

  it('carries the urql error as the cause', () => {
    const error = { message: 'boom' };
    report(error, { kind: 'mutation' });
    expect(reported[0].cause).toBe(error);
    expect(reported[0].source).toBe('graphql');
  });
});
