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

import { act, fireEvent, screen, waitFor } from '@testing-library/react';

import appConfig from '@/app-config';
import { CLI_VERSION_STATUS, CLUSTER_VERSION_STATUS } from '@/lib/graphql/dashboard/ops';
import { renderElement } from '@/test-utils';

import UpgradeBanner from '../UpgradeBanner';

const CACHE_KEY = 'kubetail:versionCheck:cache';
const DISMISSED_KEY = 'kubetail:versionCheck:dismissed';
const IGNORED_VERSIONS_KEY = 'kubetail:versionCheck:ignoredVersions';

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

const clusterMock = {
  request: { query: CLUSTER_VERSION_STATUS, variables: {} },
  result: {
    data: {
      clusterVersionStatus: {
        __typename: 'VersionStatus' as const,
        currentVersion: '0.8.0',
        latestVersion: '0.9.0',
        updateAvailable: true,
      },
    },
  },
};

const noUpdateClusterMock = {
  request: { query: CLUSTER_VERSION_STATUS, variables: {} },
  result: {
    data: {
      clusterVersionStatus: {
        __typename: 'VersionStatus' as const,
        currentVersion: '1.0.0',
        latestVersion: '1.0.0',
        updateAvailable: false,
      },
    },
  },
};

const nullClusterMock = {
  request: { query: CLUSTER_VERSION_STATUS, variables: {} },
  result: {
    data: {
      clusterVersionStatus: null,
    },
  },
};

const nullCliMock = {
  request: { query: CLI_VERSION_STATUS },
  result: {
    data: {
      cliVersionStatus: null,
    },
  },
};

beforeEach(() => {
  localStorage.clear();
  vi.useFakeTimers({ shouldAdvanceTime: true });
  Object.defineProperty(appConfig, 'environment', { value: 'desktop', writable: true });
});

afterEach(() => {
  vi.useRealTimers();
});

describe('UpgradeBanner', () => {
  it('does not show banner immediately (respects delay)', () => {
    renderElement(<UpgradeBanner />, [cliMock, clusterMock]);

    expect(screen.queryByRole('status')).not.toBeInTheDocument();
  });

  it('shows banner when update is available after delay', async () => {
    renderElement(<UpgradeBanner />, [cliMock, clusterMock]);

    await act(async () => {
      vi.advanceTimersByTime(5000);
    });

    await waitFor(() => {
      expect(screen.getByRole('status')).toBeInTheDocument();
      expect(screen.getByText(/1\.0\.0/)).toBeInTheDocument();
    });
  });

  it('does not show banner if dismissed less than 24h ago', async () => {
    localStorage.setItem(DISMISSED_KEY, String(Date.now()));

    renderElement(<UpgradeBanner />, [cliMock, clusterMock]);

    await act(async () => {
      vi.advanceTimersByTime(5000);
    });

    await waitFor(() => {
      expect(screen.queryByRole('status')).not.toBeInTheDocument();
    });
  });

  it('does not show banner if version is in ignored list', async () => {
    localStorage.setItem(IGNORED_VERSIONS_KEY, JSON.stringify(['1.0.0', '0.9.0']));

    renderElement(<UpgradeBanner />, [cliMock, clusterMock]);

    await act(async () => {
      vi.advanceTimersByTime(5000);
    });

    await waitFor(() => {
      expect(screen.queryByRole('status')).not.toBeInTheDocument();
    });
  });

  it('does not show banner when query returns null (fail silently)', async () => {
    renderElement(<UpgradeBanner />, [nullCliMock, nullClusterMock]);

    await act(async () => {
      vi.advanceTimersByTime(5000);
    });

    await waitFor(() => {
      expect(screen.queryByRole('status')).not.toBeInTheDocument();
    });
  });

  it('does not show banner when no update available', async () => {
    renderElement(<UpgradeBanner />, [nullCliMock, noUpdateClusterMock]);

    await act(async () => {
      vi.advanceTimersByTime(5000);
    });

    await waitFor(() => {
      expect(screen.queryByRole('status')).not.toBeInTheDocument();
    });
  });

  it('uses cached data and skips query when cache is fresh and no update pending', async () => {
    const cachedData = {
      timestamp: Date.now(),
      cli: { currentVersion: '1.0.0', latestVersion: '1.0.0', updateAvailable: false },
      cluster: { currentVersion: '1.0.0', latestVersion: '1.0.0', updateAvailable: false },
    };
    localStorage.setItem(CACHE_KEY, JSON.stringify(cachedData));

    renderElement(<UpgradeBanner />, []);

    await act(async () => {
      vi.advanceTimersByTime(5000);
    });

    await waitFor(() => {
      expect(screen.queryByRole('status')).not.toBeInTheDocument();
    });
  });

  it('re-queries backend when cached data had update (user may have upgraded)', async () => {
    const cachedData = {
      timestamp: Date.now(),
      cli: { currentVersion: '0.9.0', latestVersion: '1.0.0', updateAvailable: true },
      cluster: null,
    };
    localStorage.setItem(CACHE_KEY, JSON.stringify(cachedData));

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

    renderElement(<UpgradeBanner />, [noUpdateCliMock, nullClusterMock]);

    await act(async () => {
      vi.advanceTimersByTime(5000);
    });

    await waitFor(() => {
      expect(screen.queryByRole('status')).not.toBeInTheDocument();
    });
  });

  it('dismiss button hides banner for 24 hours', async () => {
    renderElement(<UpgradeBanner />, [cliMock, clusterMock]);

    await act(async () => {
      vi.advanceTimersByTime(5000);
    });

    await waitFor(() => {
      expect(screen.getByRole('status')).toBeInTheDocument();
    });

    fireEvent.click(screen.getByLabelText('Dismiss'));

    await waitFor(() => {
      expect(screen.queryByRole('status')).not.toBeInTheDocument();
    });
    expect(localStorage.getItem(DISMISSED_KEY)).not.toBeNull();
  });

  it('"Don\'t remind me" button hides banner and saves version', async () => {
    renderElement(<UpgradeBanner />, [cliMock, clusterMock]);

    await act(async () => {
      vi.advanceTimersByTime(5000);
    });

    await waitFor(() => {
      expect(screen.getByRole('status')).toBeInTheDocument();
    });

    fireEvent.click(screen.getByText("Don't remind me"));

    await waitFor(() => {
      expect(screen.queryByRole('status')).not.toBeInTheDocument();
    });
    const ignored = JSON.parse(localStorage.getItem(IGNORED_VERSIONS_KEY) || '[]');
    expect(ignored).toContain('1.0.0');
  });

  it('shows only cluster update in cluster mode', async () => {
    Object.defineProperty(appConfig, 'environment', { value: 'cluster', writable: true });

    renderElement(<UpgradeBanner />, [clusterMock]);

    await act(async () => {
      vi.advanceTimersByTime(5000);
    });

    await waitFor(() => {
      expect(screen.getByRole('status')).toBeInTheDocument();
      expect(screen.getByText(/Helm chart/)).toBeInTheDocument();
    });
  });
});
