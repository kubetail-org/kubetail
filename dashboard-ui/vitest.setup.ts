import { loadErrorMessages, loadDevMessages } from '@apollo/client/dev';
import '@testing-library/jest-dom'; // adds .toBeInTheDocument() to global `expect`
import { cleanup } from '@testing-library/react';
import type { Mock } from 'vitest';

import { getSession, useSession } from '@/lib/auth';

vi.mock('@/lib/auth', async (importOriginal) => {
  const mod = await importOriginal<typeof import('@/lib/auth')>();
  return {
    ...mod,
    getSession: vi.fn(),
    useSession: vi.fn(),
  };
});

vi.mock('@/lib/util', async (importOriginal) => {
  const mod = await importOriginal<typeof import('@/lib/util')>();
  return {
    ...mod,
    getCSRFToken: vi.fn().mockResolvedValue('testtoken'),
  };
});

// Stub the global ResizeObserver
class ResizeObserverMock {
  observe = vi.fn();

  unobserve = vi.fn();

  disconnect = vi.fn();
}

vi.stubGlobal('ResizeObserver', ResizeObserverMock);

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
});

afterEach(() => {
  cleanup();
  vi.resetAllMocks();
});
