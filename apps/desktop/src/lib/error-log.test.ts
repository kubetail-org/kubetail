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

import { waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { mockTauriCore } from '@/test-utils';

const { invokeMock, factory } = mockTauriCore();
vi.mock('@tauri-apps/api/core', () => factory());

const { onError, reportError } = await import('./error-bus');
const { MAX_IN_FLIGHT, MAX_STACK_CHARS, RESERVED_IN_FLIGHT, installGlobalErrorHandlers, startErrorLogForwarder } =
  await import('./error-log');

type Sent = {
  source: string;
  message: string;
  stack?: string;
  componentStack?: string;
  context?: Record<string, unknown>;
};

function sent(): Sent[] {
  return invokeMock.mock.calls
    .filter(([cmd]) => cmd === 'webview_record_error')
    .map(([, arg]) => (arg as { record: Sent }).record);
}

describe('startErrorLogForwarder', () => {
  let stop: () => void;

  beforeEach(() => {
    invokeMock.mockReset();
    invokeMock.mockResolvedValue(undefined);
    stop = startErrorLogForwarder();
  });

  afterEach(() => {
    stop();
  });

  it('forwards a bus error once with its message, source and a bounded stack', () => {
    const cause = new Error('boom');
    cause.stack = `Error: boom\n${'    at x\n'.repeat(5000)}`;
    reportError({ source: 'graphql', message: 'boom', cause });

    expect(sent()).toHaveLength(1);
    const [record] = sent();
    expect(record.source).toBe('graphql');
    expect(record.message).toBe('boom');
    expect(record.stack).toMatch(/^Error: boom/);
    expect(record.stack!.length).toBeLessThanOrEqual(MAX_STACK_CHARS + 1);
  });

  it('forwards the component stack of a boundary crash', () => {
    reportError({ source: 'render', message: 'kaboom', cause: new Error('kaboom'), componentStack: '\n    at Boom' });
    expect(sent()[0].componentStack).toBe('\n    at Boom');
  });

  it('still forwards a record whose cause refuses its stack, and never re-enters the bus', () => {
    const seen = vi.fn();
    const off = onError(seen);
    const cause = {
      get stack(): string {
        throw new Error('no stack for you');
      },
    };
    reportError({ source: 'network', message: 'dial failed', cause });
    off();

    expect(sent()).toEqual([{ source: 'network', message: 'dial failed' }]);
    expect(seen).toHaveBeenCalledTimes(1);
  });

  it('forwards a record whose context will not serialize without the context', () => {
    reportError({ source: 'graphql', message: 'odd context', context: { size: 1n } });
    expect(sent()).toEqual([{ source: 'graphql', message: 'odd context' }]);
  });

  it('swallows a rejected invoke without touching the bus', async () => {
    const seen = vi.fn();
    const off = onError(seen);
    invokeMock.mockRejectedValue(new Error('host gone'));
    reportError({ source: 'graphql', message: 'x' });
    // Let the rejection settle; a re-entry would land on `seen`.
    await waitFor(() => expect(invokeMock).toHaveBeenCalledTimes(1));
    await Promise.resolve();
    off();
    expect(seen).toHaveBeenCalledTimes(1);
  });

  it('drops sends past the in-flight cap, keeps a reserve for render, and reports the drops', async () => {
    const resolvers: (() => void)[] = [];
    invokeMock.mockImplementation(
      () =>
        new Promise<void>((resolve) => {
          resolvers.push(resolve);
        }),
    );

    const ordinary = MAX_IN_FLIGHT - RESERVED_IN_FLIGHT;
    for (let i = 0; i < ordinary + 3; i += 1) {
      reportError({ source: 'subscription', message: `retry ${i}` });
    }
    expect(sent()).toHaveLength(ordinary);

    // The reserve admits the crash, and it is the next record that goes, so it
    // carries the count.
    reportError({ source: 'render', message: 'crash' });
    expect(sent()).toHaveLength(ordinary + 1);
    expect(sent().at(-1)!).toMatchObject({ message: 'crash', context: { dropped: 3 } });

    // Once the sends settle, ordinary records flow again.
    resolvers.forEach((resolve) => resolve());
    await waitFor(() => {
      reportError({ source: 'graphql', message: 'after' });
      expect(sent().at(-1)!.message).toBe('after');
    });
  });

  it('carries the drop count once, on the record that goes', () => {
    invokeMock.mockImplementation(() => new Promise<void>(() => {}));
    for (let i = 0; i < MAX_IN_FLIGHT; i += 1) {
      reportError({ source: 'render', message: `r${i}` });
    }
    expect(sent()).toHaveLength(MAX_IN_FLIGHT);
    expect(sent().every((r) => r.context === undefined)).toBe(true);
  });

  it('stops forwarding once unsubscribed', () => {
    stop();
    reportError({ source: 'graphql', message: 'after stop' });
    expect(sent()).toHaveLength(0);
  });
});

describe('installGlobalErrorHandlers', () => {
  it('reports an unhandled rejection to the bus as a render error', () => {
    const seen = vi.fn();
    const off = onError(seen);
    const uninstall = installGlobalErrorHandlers();

    const reason = new Error('late failure');
    window.dispatchEvent(Object.assign(new Event('unhandledrejection'), { reason }));

    uninstall();
    off();
    expect(seen).toHaveBeenCalledTimes(1);
    expect(seen.mock.calls[0][0]).toMatchObject({ source: 'render', message: 'late failure', cause: reason });
  });

  it('reports an uncaught error to the bus as a render error', () => {
    const seen = vi.fn();
    const off = onError(seen);
    const uninstall = installGlobalErrorHandlers();

    const error = new Error('thrown outside React');
    window.dispatchEvent(new ErrorEvent('error', { error, message: error.message }));

    uninstall();
    off();
    expect(seen).toHaveBeenCalledTimes(1);
    expect(seen.mock.calls[0][0]).toMatchObject({ source: 'render', message: 'thrown outside React', cause: error });
  });

  it('reports an error event with no error by its message', () => {
    const seen = vi.fn();
    const off = onError(seen);
    const uninstall = installGlobalErrorHandlers();

    window.dispatchEvent(new ErrorEvent('error', { error: null, message: 'Script error.' }));

    uninstall();
    off();
    expect(seen.mock.calls[0][0]).toMatchObject({ source: 'render', message: 'Script error.' });
  });

  it('ignores a ResizeObserver loop notice', () => {
    const seen = vi.fn();
    const off = onError(seen);
    const uninstall = installGlobalErrorHandlers();

    window.dispatchEvent(
      new ErrorEvent('error', {
        error: null,
        message: 'ResizeObserver loop completed with undelivered notifications.',
      }),
    );

    uninstall();
    off();
    expect(seen).not.toHaveBeenCalled();
  });

  it('stops reporting once uninstalled', () => {
    const seen = vi.fn();
    const off = onError(seen);
    installGlobalErrorHandlers()();
    window.dispatchEvent(Object.assign(new Event('unhandledrejection'), { reason: new Error('x') }));
    off();
    expect(seen).not.toHaveBeenCalled();
  });
});
