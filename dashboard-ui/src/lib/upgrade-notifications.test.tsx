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

import type { MockedResponse } from '@apollo/client/testing';
import { act, screen, waitFor } from '@testing-library/react';

import appConfig from '@/app-config';
import { CLI_VERSION_STATUS } from '@/lib/graphql/dashboard/ops';
import { renderElement } from '@/test-utils';

import { UpgradeNotificationProvider, useUpgradeNotification } from './upgrade-notifications';

const CLI_CACHE_KEY = 'kubetail:versionCheck:cli';
const DISMISSED_KEY = 'kubetail:versionCheck:dismissed';
const IGNORED_VERSIONS_KEY = 'kubetail:versionCheck:ignoredVersions';

function TestConsumer() {
  const { showBanner, cliStatus } = useUpgradeNotification();
  return (
    <div>
      {showBanner && <span data-testid="banner-visible">visible</span>}
      {cliStatus?.updateAvailable && <span data-testid="cli-update">{cliStatus.latestVersion}</span>}
    </div>
  );
}

function renderWithProvider(mocks: MockedResponse[]) {
  return renderElement(
    <UpgradeNotificationProvider>
      <TestConsumer />
    </UpgradeNotificationProvider>,
    mocks,
  );
}

const cliMock = {
  request: { query: CLI_VERSION_STATUS },
  result: {
    data: {
      cliVersionStatus: {
        __typename: 'VersionStatus' as const,
        currentVersion: '0.9.0',
        latestVersion: '1.0.0',
        updateAvailable: true,
      },
    },
  },
};

const nullCliMock = {
  request: { query: CLI_VERSION_STATUS },
  result: { data: { cliVersionStatus: null } },
};

beforeEach(() => {
  localStorage.clear();
  vi.useFakeTimers({ shouldAdvanceTime: true });
  Object.defineProperty(appConfig, 'environment', { value: 'desktop', writable: true });
});

afterEach(() => {
  vi.useRealTimers();
});

describe('useUpgradeNotification', () => {
  it('does not show banner immediately (respects delay)', () => {
    renderWithProvider([cliMock]);
    expect(screen.queryByTestId('banner-visible')).not.toBeInTheDocument();
  });

  it('shows banner after delay when update is available', async () => {
    renderWithProvider([cliMock]);

    await act(async () => {
      vi.advanceTimersByTime(5000);
    });

    await waitFor(() => {
      expect(screen.getByTestId('banner-visible')).toBeInTheDocument();
    });
  });

  it('does not show banner if dismissed less than 24h ago', async () => {
    localStorage.setItem(DISMISSED_KEY, String(Date.now()));
    renderWithProvider([cliMock]);

    await act(async () => {
      vi.advanceTimersByTime(5000);
    });

    await waitFor(() => {
      expect(screen.queryByTestId('banner-visible')).not.toBeInTheDocument();
    });
  });

  it('does not show banner if version is in ignored list', async () => {
    localStorage.setItem(IGNORED_VERSIONS_KEY, JSON.stringify(['1.0.0']));
    renderWithProvider([cliMock]);

    await act(async () => {
      vi.advanceTimersByTime(5000);
    });

    await waitFor(() => {
      expect(screen.queryByTestId('banner-visible')).not.toBeInTheDocument();
    });
  });

  it('does not show banner when query returns null (fail silently)', async () => {
    renderWithProvider([nullCliMock]);

    await act(async () => {
      vi.advanceTimersByTime(5000);
    });

    await waitFor(() => {
      expect(screen.queryByTestId('banner-visible')).not.toBeInTheDocument();
    });
  });

  it('uses cached CLI data when cache is fresh', async () => {
    const entry = {
      timestamp: Date.now(),
      data: { currentVersion: '1.0.0', latestVersion: '1.0.0', updateAvailable: false },
    };
    localStorage.setItem(CLI_CACHE_KEY, JSON.stringify(entry));

    renderWithProvider([]);

    await act(async () => {
      vi.advanceTimersByTime(5000);
    });

    await waitFor(() => {
      expect(screen.queryByTestId('banner-visible')).not.toBeInTheDocument();
    });
  });

  it('re-queries backend when cached CLI data had update', async () => {
    const entry = {
      timestamp: Date.now(),
      data: { currentVersion: '0.9.0', latestVersion: '1.0.0', updateAvailable: true },
    };
    localStorage.setItem(CLI_CACHE_KEY, JSON.stringify(entry));

    const noUpdateCliMock = {
      request: { query: CLI_VERSION_STATUS },
      result: {
        data: {
          cliVersionStatus: {
            __typename: 'VersionStatus' as const,
            currentVersion: '1.0.0',
            latestVersion: '1.0.0',
            updateAvailable: false,
          },
        },
      },
    };

    renderWithProvider([noUpdateCliMock]);

    await act(async () => {
      vi.advanceTimersByTime(5000);
    });

    await waitFor(() => {
      expect(screen.queryByTestId('banner-visible')).not.toBeInTheDocument();
    });
  });
});
