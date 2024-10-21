import { loadErrorMessages, loadDevMessages } from '@apollo/client/dev';
import '@testing-library/jest-dom'; // adds .toBeInTheDocument() to global `expect`
import { cleanup } from '@testing-library/react';
import type { Mock } from 'vitest';

import { getSession, useSession } from '@/lib/auth';

// display apollo error messages in console
loadDevMessages();
loadErrorMessages();

vi.mock('@/lib/auth', async (importOriginal) => {
  const mod = await importOriginal<typeof import('@/lib/auth')>();
  return {
    ...mod,
    getSession: vi.fn(),
    useSession: vi.fn(),
  };
});

vi.mock('@/lib/helpers', async (importOriginal) => {
  const mod = await importOriginal<typeof import('@/lib/helpers')>();
  return {
    ...mod,
    getCSRFToken: vi.fn().mockResolvedValue('testtoken'),
  };
});

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
