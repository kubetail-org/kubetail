import { loadErrorMessages, loadDevMessages } from '@apollo/client/dev';
import '@testing-library/jest-dom'; // adds .toBeInTheDocument() to global `expect`
import { cleanup } from '@testing-library/react';
import type { Mock } from 'vitest';

import { getSession, useSession } from '@/lib/auth';
import { useIsClusterAPIEnabled } from '@/lib/hooks';

vi.mock('@/lib/auth', async (importOriginal) => {
  const mod = await importOriginal<typeof import('@/lib/auth')>();
  return {
    ...mod,
    getSession: vi.fn(),
    useSession: vi.fn(),
  };
});

vi.mock('@/lib/hooks', async (importOriginal) => {
  const mod = await importOriginal<typeof import('@/lib/hooks')>();
  return {
    ...mod,
    useIsClusterAPIEnabled: vi.fn().mockResolvedValue(undefined),
  };
});

// Stub the global ResizeObserver
class ResizeObserverMock {
  observe = vi.fn();

  unobserve = vi.fn();

  disconnect = vi.fn();
}

class BroadcastChannelMock {
  static instances: BroadcastChannelMock[] = [];

  name: string;

  onmessage: ((ev: MessageEvent) => void) | null = null;

  postMessage = vi.fn((data: unknown) => {
    // Deliver to all other instances with the same channel name
    BroadcastChannelMock.instances
      .filter((instance) => instance !== this && instance.name === this.name && instance.onmessage)
      .forEach((instance) => instance.onmessage!(new MessageEvent('message', { data })));
  });

  close = vi.fn(() => {
    const idx = BroadcastChannelMock.instances.indexOf(this);
    if (idx !== -1) BroadcastChannelMock.instances.splice(idx, 1);
  });

  constructor(name: string) {
    this.name = name;
    BroadcastChannelMock.instances.push(this);
  }
}

vi.stubGlobal('BroadcastChannel', BroadcastChannelMock);
vi.stubGlobal('ResizeObserver', ResizeObserverMock);
vi.stubGlobal('requestAnimationFrame', (callback: FrameRequestCallback) => window.setTimeout(callback, 0));
vi.stubGlobal('cancelAnimationFrame', (id: number) => window.clearTimeout(id));

// Display apollo error messages in console
loadDevMessages();
loadErrorMessages();

beforeEach(() => {
  // init auth mocks
  (getSession as Mock).mockReturnValue({
    user: null,
    timestamp: new Date(),
  });

  (useSession as Mock).mockReturnValue({
    loading: false,
    session: { user: null },
  });

  (useIsClusterAPIEnabled as Mock).mockReturnValue(true);
});

afterEach(() => {
  cleanup();
  BroadcastChannelMock.instances.length = 0;
  vi.resetAllMocks();
});
