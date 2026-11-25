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

import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';

import { mockTauriCore } from '@/test-utils';

// Mocks ---------------------------------------------------------------

// Sign-in and sign-out run over the host's gRPC login; the control only asks.
const { useAuthStateMock, login, logout } = vi.hoisted(() => ({
  useAuthStateMock: vi.fn(),
  login: vi.fn(() => Promise.resolve()),
  logout: vi.fn(() => Promise.resolve()),
}));
vi.mock('@/lib/auth', () => ({ useAuthState: useAuthStateMock }));

// The account page opens in the browser, which only the host can do.
const { invokeMock, factory } = mockTauriCore();
vi.mock('@tauri-apps/api/core', () => factory());

const { AccountAvatar } = await import('./account-avatar');

// Helpers -------------------------------------------------------------

function signedOut(over: { loading?: boolean } = {}) {
  useAuthStateMock.mockReturnValue({
    authState: { authenticated: false, identity: null },
    loading: over.loading ?? false,
    login,
    logout,
  });
}

function signedIn(identity: { name?: string; email?: string } | null = { email: 'andres.morey@example.com' }) {
  useAuthStateMock.mockReturnValue({
    authState: { authenticated: true, identity },
    loading: false,
    login,
    logout,
  });
}

const avatar = () => screen.getByRole('button', { name: /^account:/i });

// The menu portals in asynchronously, so wait for the item rather than reading it.
const openMenu = async () => {
  const user = userEvent.setup();
  await user.click(avatar());
  return user;
};

const menuItem = (name: string) => screen.findByRole('menuitem', { name });

// base-ui scrolls the highlighted item into view; jsdom has no implementation.
beforeAll(() => {
  Element.prototype.scrollIntoView = vi.fn();
});

beforeEach(() => {
  vi.clearAllMocks();
  invokeMock.mockResolvedValue(undefined);
  login.mockResolvedValue(undefined);
  logout.mockResolvedValue(undefined);
  signedOut();
});

// Tests ---------------------------------------------------------------

describe('AccountAvatar', () => {
  describe('signed out', () => {
    it('is still an avatar, not a sign-in button', () => {
      render(<AccountAvatar />);
      expect(avatar()).toHaveAccessibleName('Account: Guest');
    });

    it('offers Sign in from the menu', async () => {
      render(<AccountAvatar />);
      await openMenu();
      expect(await menuItem('Sign in')).toBeInTheDocument();
    });

    it('starts the login when the menu item is pressed', async () => {
      render(<AccountAvatar />);
      const user = await openMenu();
      await user.click(await menuItem('Sign in'));
      expect(login).toHaveBeenCalledTimes(1);
    });

    it('swallows a failed login rather than reporting it twice', async () => {
      // The auth provider already surfaces the failure; an unhandled rejection
      // here would fail the suite.
      login.mockRejectedValue(new Error('no host'));
      render(<AccountAvatar />);
      const user = await openMenu();
      await user.click(await menuItem('Sign in'));
      await Promise.resolve();
      expect(login).toHaveBeenCalledTimes(1);
    });

    it('declines a second press while one login is in flight', async () => {
      signedOut({ loading: true });
      render(<AccountAvatar />);
      await openMenu();
      expect(await menuItem('Sign in')).toHaveAttribute('data-disabled');
    });
  });

  describe('signed in', () => {
    it('names the account it belongs to, and wears its initials', () => {
      signedIn({ name: 'Andres Morey', email: 'andres@example.com' });
      render(<AccountAvatar />);
      expect(avatar()).toHaveAccessibleName('Account: Andres Morey');
      expect(avatar()).toHaveTextContent('AM');
    });

    it('falls back to the email when there is no name', () => {
      signedIn({ email: 'andres.morey@example.com' });
      render(<AccountAvatar />);
      // Split on the separator, so the local part reads as two words.
      expect(avatar()).toHaveTextContent('AM');
    });

    it('takes two letters from a one-word identity', () => {
      signedIn({ name: 'andres' });
      render(<AccountAvatar />);
      expect(avatar()).toHaveTextContent('AN');
    });

    it('wears a placeholder rather than nothing for a blank identity', () => {
      signedIn({ name: '   ' });
      render(<AccountAvatar />);
      expect(avatar()).toHaveTextContent('?');
    });

    it('falls back to an icon when the session carries no identity at all', () => {
      signedIn(null);
      render(<AccountAvatar />);
      expect(avatar()).toHaveAccessibleName('Account: Signed in');
      expect(avatar()).toHaveTextContent('');
    });

    it('asks the host to open the account page in the browser', async () => {
      signedIn();
      render(<AccountAvatar />);
      const user = await openMenu();

      // The page is on the web, and the webview has no network of its own.
      await user.click(await menuItem('Account'));
      expect(invokeMock).toHaveBeenCalledWith('open_account_url');
    });

    it('swallows a failed open', async () => {
      invokeMock.mockRejectedValue(new Error('no opener'));
      signedIn();
      render(<AccountAvatar />);
      const user = await openMenu();

      await user.click(await menuItem('Account'));
      await Promise.resolve();
      expect(invokeMock).toHaveBeenCalledWith('open_account_url');
    });

    it('signs out from the menu', async () => {
      signedIn();
      render(<AccountAvatar />);
      const user = await openMenu();

      await user.click(await menuItem('Sign out'));
      expect(logout).toHaveBeenCalledTimes(1);
    });

    it('swallows a failed sign-out', async () => {
      logout.mockRejectedValue(new Error('no host'));
      signedIn();
      render(<AccountAvatar />);
      const user = await openMenu();

      await user.click(await menuItem('Sign out'));
      await Promise.resolve();
      expect(logout).toHaveBeenCalledTimes(1);
    });
  });
});
