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

import { act, render, screen } from '@testing-library/react';
import type { MockInstance } from 'vitest';

// vitest.setup.ts mocks this module's exports, so each test loads a fresh copy of the real one:
// the CSRF token and the broadcast channels are module state.
type AuthModule = typeof import('./auth');
let auth: AuthModule;

// The setup file's BroadcastChannel stub has no addEventListener, which SessionProvider uses.
class FakeBroadcastChannel extends EventTarget {
  static instances: FakeBroadcastChannel[] = [];

  constructor(readonly name: string) {
    super();
    FakeBroadcastChannel.instances.push(this);
  }

  postMessage(data: unknown) {
    FakeBroadcastChannel.instances
      .filter((other) => other !== this && other.name === this.name)
      .forEach((other) => other.dispatchEvent(new MessageEvent('message', { data })));
  }

  close() {}
}

// Delivers a message as another tab's channel would.
function broadcastFromOtherTab(data: unknown) {
  // oxlint-disable-next-line unicorn/require-post-message-target-origin -- A BroadcastChannel takes no origin.
  new FakeBroadcastChannel('auth/session').postMessage(data);
}

const sessionBody = (overrides: Record<string, unknown> = {}) => ({
  auth_mode: 'token',
  user: 'alice',
  message: null,
  timestamp: '2024-01-01T00:00:00Z',
  ...overrides,
});

let fetchMock: MockInstance<typeof fetch>;

function respondWith(body: unknown, csrfToken: string | null = 'tok-1') {
  fetchMock.mockImplementation(async () => {
    const headers = new Headers({ 'Content-Type': 'application/json' });
    if (csrfToken) headers.set('X-CSRF-Token', csrfToken);
    return new Response(JSON.stringify(body), { headers });
  });
}

beforeEach(async () => {
  FakeBroadcastChannel.instances = [];
  vi.stubGlobal('BroadcastChannel', FakeBroadcastChannel);
  fetchMock = vi.spyOn(globalThis, 'fetch');
  vi.resetModules();
  auth = await vi.importActual<AuthModule>('./auth');
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe('getSession', () => {
  it('fetches the session endpoint and returns the parsed session', async () => {
    respondWith(sessionBody({ message: 'hi' }));

    const session = await auth.getSession();

    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [url, init] = fetchMock.mock.calls[0];
    expect((url as URL).href).toBe(`${window.location.origin}/api/auth/session`);
    expect(init).toEqual({ cache: 'no-store' });

    expect(session).toEqual({
      auth_mode: 'token',
      user: 'alice',
      message: 'hi',
      timestamp: new Date('2024-01-01T00:00:00Z'),
    });
  });

  it('takes the CSRF token from the X-CSRF-Token header', async () => {
    respondWith(sessionBody(), 'tok-1');
    expect(auth.getCsrfToken()).toBe('');

    await auth.getSession();
    expect(auth.getCsrfToken()).toBe('tok-1');
  });

  it('keeps the CSRF token when the response has no header', async () => {
    respondWith(sessionBody(), 'tok-1');
    await auth.getSession();

    respondWith(sessionBody(), null);
    await auth.getSession();
    expect(auth.getCsrfToken()).toBe('tok-1');
  });

  it('publishes the body to the other tabs', async () => {
    const otherTab = new FakeBroadcastChannel('auth/session');
    const listener = vi.fn();
    otherTab.addEventListener('message', listener);

    const body = sessionBody();
    respondWith(body);
    await auth.getSession();

    expect(listener).toHaveBeenCalledTimes(1);
    expect((listener.mock.calls[0][0] as MessageEvent).data).toEqual(body);
  });

  it.each([
    ['null', null],
    ['a string', 'session'],
    ['a missing auth_mode', sessionBody({ auth_mode: undefined })],
    ['a numeric user', sessionBody({ user: 1 })],
    ['a numeric message', sessionBody({ message: 1 })],
    ['a missing timestamp', sessionBody({ timestamp: undefined })],
  ])('rejects a body with %s', async (_, body) => {
    respondWith(body);
    await expect(auth.getSession()).rejects.toThrow('Invalid session response');
  });

  it('accepts a signed-out session', async () => {
    respondWith(sessionBody({ user: null, message: 'signed out' }));
    await expect(auth.getSession()).resolves.toMatchObject({ user: null, message: 'signed out' });
  });
});

describe('resetCsrfToken', () => {
  it('clears the token', async () => {
    respondWith(sessionBody());
    await auth.getSession();

    auth.resetCsrfToken();
    expect(auth.getCsrfToken()).toBe('');
  });
});

describe('waitForCsrfToken', () => {
  it('resolves without a request when the token is set', async () => {
    respondWith(sessionBody());
    await auth.getSession();
    fetchMock.mockClear();

    await auth.waitForCsrfToken();
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it('fetches the session once for concurrent callers', async () => {
    respondWith(sessionBody(), 'tok-2');

    await Promise.all([auth.waitForCsrfToken(), auth.waitForCsrfToken()]);

    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(auth.getCsrfToken()).toBe('tok-2');
  });

  it('rejects when the response carries no token, and retries on the next call', async () => {
    respondWith(sessionBody(), null);
    await expect(auth.waitForCsrfToken()).rejects.toThrow('CSRF token missing from session response');

    respondWith(sessionBody(), 'tok-3');
    await auth.waitForCsrfToken();

    expect(fetchMock).toHaveBeenCalledTimes(2);
    expect(auth.getCsrfToken()).toBe('tok-3');
  });
});

describe('SessionProvider and useSession', () => {
  function SessionView() {
    const { loading, session } = auth.useSession();
    if (loading) return <div>loading</div>;
    return <div>user: {session?.user ?? 'none'}</div>;
  }

  function renderProvider() {
    return render(
      <auth.SessionProvider>
        <SessionView />
      </auth.SessionProvider>,
    );
  }

  it('is loading until the session arrives, then provides it', async () => {
    respondWith(sessionBody());

    renderProvider();
    expect(screen.getByText('loading')).toBeInTheDocument();

    await screen.findByText('user: alice');
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it('takes a newer session from another tab and ignores an older or invalid one', async () => {
    respondWith(sessionBody());
    renderProvider();
    await screen.findByText('user: alice');

    act(() => broadcastFromOtherTab(sessionBody({ user: 'bob', timestamp: '2024-01-01T00:00:01Z' })));
    expect(screen.getByText('user: bob')).toBeInTheDocument();

    act(() => broadcastFromOtherTab(sessionBody({ user: 'carol', timestamp: '2023-12-31T00:00:00Z' })));
    act(() => broadcastFromOtherTab({ user: 'mallory' }));
    expect(screen.getByText('user: bob')).toBeInTheDocument();
  });

  it('refetches the session when the page becomes visible', async () => {
    respondWith(sessionBody());
    renderProvider();
    await screen.findByText('user: alice');

    respondWith(sessionBody({ user: null, timestamp: '2024-01-01T00:00:05Z' }));
    act(() => {
      document.dispatchEvent(new Event('visibilitychange'));
    });

    await screen.findByText('user: none');
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it('stops listening when unmounted', async () => {
    respondWith(sessionBody());
    const { unmount } = renderProvider();
    await screen.findByText('user: alice');

    unmount();
    fetchMock.mockClear();
    document.dispatchEvent(new Event('visibilitychange'));

    expect(fetchMock).not.toHaveBeenCalled();
  });
});
