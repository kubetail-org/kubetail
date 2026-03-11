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
import { CLI_LATEST_VERSION } from '@/lib/graphql/dashboard/ops';
import { renderElement } from '@/test-utils';

import { UpgradeNotificationProvider, useUpgradeNotification, compareSemver } from './upgrade-notifications';

const LATEST_VERSION_CACHE_KEY = 'kubetail:versionCheck:cliLatest';
const DISMISSED_KEY = 'kubetail:versionCheck:dismissed';
const IGNORED_VERSIONS_KEY = 'kubetail:versionCheck:ignoredVersions';

function TestConsumer() {
  const { showBanner, updateAvailable, latestVersion } = useUpgradeNotification();
  return (
    <div>
      {showBanner && <span data-testid="banner-visible">visible</span>}
      {updateAvailable && <span data-testid="cli-update">{latestVersion}</span>}
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

const latestVersionMock = {
  request: { query: CLI_LATEST_VERSION },
  result: { data: { cliLatestVersion: '1.0.0' } },
};

const nullLatestMock = {
  request: { query: CLI_LATEST_VERSION },
  result: { data: { cliLatestVersion: null } },
};

beforeEach(() => {
  localStorage.clear();
  vi.useFakeTimers({ shouldAdvanceTime: true });
  Object.defineProperty(appConfig, 'environment', { value: 'desktop', writable: true });
  Object.defineProperty(appConfig, 'cliVersion', { value: '0.9.0', writable: true });
});

afterEach(() => {
  vi.useRealTimers();
});

describe('compareSemver', () => {
  it('returns positive when a > b', () => {
    expect(compareSemver('1.0.0', '0.9.0')).toBeGreaterThan(0);
  });

  it('returns negative when a < b', () => {
    expect(compareSemver('0.9.0', '1.0.0')).toBeLessThan(0);
  });

  it('returns 0 when equal', () => {
    expect(compareSemver('1.0.0', '1.0.0')).toBe(0);
  });

  it('handles v prefix', () => {
    expect(compareSemver('v1.0.0', '0.9.0')).toBeGreaterThan(0);
  });
});

describe('useUpgradeNotification', () => {
  it('does not show banner immediately (respects delay)', () => {
    renderWithProvider([latestVersionMock]);
    expect(screen.queryByTestId('banner-visible')).not.toBeInTheDocument();
  });

  it('shows banner after delay when update is available', async () => {
    renderWithProvider([latestVersionMock]);

    await act(async () => {
      vi.advanceTimersByTime(5000);
    });

    await waitFor(() => {
      expect(screen.getByTestId('banner-visible')).toBeInTheDocument();
    });
  });

  it('does not show banner if dismissed less than 24h ago', async () => {
    localStorage.setItem(DISMISSED_KEY, String(Date.now()));
    renderWithProvider([latestVersionMock]);

    await act(async () => {
      vi.advanceTimersByTime(5000);
    });

    await waitFor(() => {
      expect(screen.queryByTestId('banner-visible')).not.toBeInTheDocument();
    });
  });

  it('does not show banner if version is in ignored list', async () => {
    localStorage.setItem(IGNORED_VERSIONS_KEY, JSON.stringify(['1.0.0']));
    renderWithProvider([latestVersionMock]);

    await act(async () => {
      vi.advanceTimersByTime(5000);
    });

    await waitFor(() => {
      expect(screen.queryByTestId('banner-visible')).not.toBeInTheDocument();
    });
  });

  it('does not show banner when query returns null (fail silently)', async () => {
    renderWithProvider([nullLatestMock]);

    await act(async () => {
      vi.advanceTimersByTime(5000);
    });

    await waitFor(() => {
      expect(screen.queryByTestId('banner-visible')).not.toBeInTheDocument();
    });
  });

  it('uses cached latestVersion when cache is fresh', async () => {
    const entry = { timestamp: Date.now(), version: '0.9.0' };
    localStorage.setItem(LATEST_VERSION_CACHE_KEY, JSON.stringify(entry));

    renderWithProvider([]);

    await act(async () => {
      vi.advanceTimersByTime(5000);
    });

    await waitFor(() => {
      expect(screen.queryByTestId('banner-visible')).not.toBeInTheDocument();
    });
  });

  it('does not show banner when current version equals latest', async () => {
    Object.defineProperty(appConfig, 'cliVersion', { value: '1.0.0', writable: true });
    renderWithProvider([latestVersionMock]);

    await act(async () => {
      vi.advanceTimersByTime(5000);
    });

    await waitFor(() => {
      expect(screen.queryByTestId('banner-visible')).not.toBeInTheDocument();
    });
  });
});
