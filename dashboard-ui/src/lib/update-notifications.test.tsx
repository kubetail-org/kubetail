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
import { CLI_LATEST_VERSION, CLUSTER_VERSION_STATUS, KUBE_CONFIG_WATCH } from '@/lib/graphql/dashboard/ops';
import { renderElement } from '@/test-utils';

import {
  compareSemver,
  UpdateNotificationProvider,
  useCLIUpdateNotification,
  useClusterUpdateNotification,
} from './update-notifications';

const STORAGE_KEY = 'kubetail:updates:cli';

/** Satisfies KUBE_CONFIG_WATCH inside UpdateNotificationProvider for CLI-only tests. */
const notificationProviderKubeMock: MockedResponse = {
  request: { query: KUBE_CONFIG_WATCH },
  maxUsageCount: 20,
  result: {
    data: {
      kubeConfigWatch: {
        __typename: 'KubeConfigWatchEvent',
        type: 'ADDED',
        object: {
          __typename: 'KubeConfig',
          currentContext: '',
          contexts: [],
        },
      },
    },
  },
};

function TestConsumer() {
  const { showBanner, updateAvailable, latestVersion } = useCLIUpdateNotification();
  return (
    <div>
      {showBanner && <span data-testid="banner-visible">visible</span>}
      {updateAvailable && <span data-testid="cli-update">{latestVersion}</span>}
    </div>
  );
}

function renderWithProvider(mocks: MockedResponse[]) {
  return renderElement(
    <UpdateNotificationProvider>
      <TestConsumer />
    </UpdateNotificationProvider>,
    [notificationProviderKubeMock, ...mocks],
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

describe('useCLIUpdateNotification', () => {
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
    localStorage.setItem(STORAGE_KEY, JSON.stringify({ dismissedAt: Date.now() }));
    renderWithProvider([latestVersionMock]);

    await act(async () => {
      vi.advanceTimersByTime(5000);
    });

    await waitFor(() => {
      expect(screen.queryByTestId('banner-visible')).not.toBeInTheDocument();
    });
  });

  it('does not show banner if version is in skipped list', async () => {
    localStorage.setItem(STORAGE_KEY, JSON.stringify({ skippedVersions: ['1.0.0'] }));
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
    localStorage.setItem(STORAGE_KEY, JSON.stringify({ latestVersion: '0.9.0', fetchedAt: Date.now() }));

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

const CLUSTER_STORAGE_PREFIX = 'kubetail:updates:cluster:';
const KUBE_CONTEXT = 'test-cluster';

/** Keeps CLI query satisfied while cluster tests run under UpdateNotificationProvider. */
const clusterTestCliMock: MockedResponse = {
  request: { query: CLI_LATEST_VERSION },
  result: { data: { cliLatestVersion: '0.9.0' } },
};

function kubeConfigWatchMock(contextNames: string[], currentContext?: string): MockedResponse {
  return {
    request: { query: KUBE_CONFIG_WATCH },
    result: {
      data: {
        kubeConfigWatch: {
          type: 'ADDED',
          object: {
            currentContext: currentContext ?? contextNames[0] ?? '',
            contexts: contextNames.map((name) => ({ name, cluster: 'c', namespace: 'default' })),
          },
        },
      },
    },
  };
}

function ClusterTestConsumer({ kubeContext = KUBE_CONTEXT }: { kubeContext?: string }) {
  const { updateAvailable, currentVersion, latestVersion } = useClusterUpdateNotification(kubeContext);
  return (
    <div>
      {updateAvailable && <span data-testid="cluster-update">{latestVersion}</span>}
      {currentVersion && <span data-testid="current-version">{currentVersion}</span>}
    </div>
  );
}

function renderClusterWithMocks(
  mocks: MockedResponse[],
  kubeContext?: string,
  kubeContextNames: string[] = [KUBE_CONTEXT],
) {
  return renderElement(
    <UpdateNotificationProvider>
      <ClusterTestConsumer kubeContext={kubeContext} />
    </UpdateNotificationProvider>,
    [clusterTestCliMock, kubeConfigWatchMock(kubeContextNames), ...mocks],
  );
}

const clusterUpdateAvailableMock: MockedResponse = {
  request: { query: CLUSTER_VERSION_STATUS, variables: { kubeContext: KUBE_CONTEXT } },
  result: {
    data: { clusterVersionStatus: { currentVersion: '0.9.0', latestVersion: '1.0.0', updateAvailable: true } },
  },
};

const clusterNoUpdateMock: MockedResponse = {
  request: { query: CLUSTER_VERSION_STATUS, variables: { kubeContext: KUBE_CONTEXT } },
  result: {
    data: { clusterVersionStatus: { currentVersion: '1.0.0', latestVersion: '1.0.0', updateAvailable: false } },
  },
};

const clusterNullResultMock: MockedResponse = {
  request: { query: CLUSTER_VERSION_STATUS, variables: { kubeContext: KUBE_CONTEXT } },
  result: { data: { clusterVersionStatus: null } },
};

const clusterErrorMock: MockedResponse = {
  request: { query: CLUSTER_VERSION_STATUS, variables: { kubeContext: KUBE_CONTEXT } },
  error: new Error('Internal Server Error'),
};

describe('useClusterUpdateNotification', () => {
  it('shows update notification when updateAvailable is true', async () => {
    renderClusterWithMocks([clusterUpdateAvailableMock]);

    await waitFor(() => {
      expect(screen.getByTestId('cluster-update')).toBeInTheDocument();
      expect(screen.getByTestId('cluster-update')).toHaveTextContent('1.0.0');
    });
  });

  it('does not show update notification when updateAvailable is false', async () => {
    renderClusterWithMocks([clusterNoUpdateMock]);

    await act(async () => {
      vi.advanceTimersByTime(1000);
    });

    await waitFor(() => {
      expect(screen.queryByTestId('cluster-update')).not.toBeInTheDocument();
    });
  });

  it('does not show notification when dismissed less than 24h ago', async () => {
    localStorage.setItem(`${CLUSTER_STORAGE_PREFIX}${KUBE_CONTEXT}`, JSON.stringify({ dismissedAt: Date.now() }));
    renderClusterWithMocks([clusterUpdateAvailableMock]);

    await act(async () => {
      vi.advanceTimersByTime(1000);
    });

    await waitFor(() => {
      expect(screen.queryByTestId('cluster-update')).not.toBeInTheDocument();
    });
  });

  it('does not show notification when version is in skipped list', async () => {
    localStorage.setItem(`${CLUSTER_STORAGE_PREFIX}${KUBE_CONTEXT}`, JSON.stringify({ skippedVersions: ['1.0.0'] }));
    renderClusterWithMocks([clusterUpdateAvailableMock]);

    await act(async () => {
      vi.advanceTimersByTime(1000);
    });

    await waitFor(() => {
      expect(screen.queryByTestId('cluster-update')).not.toBeInTheDocument();
    });
  });

  it('fails silently when query returns null', async () => {
    renderClusterWithMocks([clusterNullResultMock]);

    await act(async () => {
      vi.advanceTimersByTime(1000);
    });

    await waitFor(() => {
      expect(screen.queryByTestId('cluster-update')).not.toBeInTheDocument();
    });
  });

  it('fails silently when query errors', async () => {
    renderClusterWithMocks([clusterErrorMock]);

    await act(async () => {
      vi.advanceTimersByTime(1000);
    });

    await waitFor(() => {
      expect(screen.queryByTestId('cluster-update')).not.toBeInTheDocument();
    });
  });

  it('uses cached data when cache is fresh', async () => {
    localStorage.setItem(
      `${CLUSTER_STORAGE_PREFIX}${KUBE_CONTEXT}`,
      JSON.stringify({
        currentVersion: '0.9.0',
        latestVersion: '0.9.0',
        fetchedAt: Date.now(),
      }),
    );

    renderClusterWithMocks([]);

    await act(async () => {
      vi.advanceTimersByTime(1000);
    });

    await waitFor(() => {
      expect(screen.queryByTestId('cluster-update')).not.toBeInTheDocument();
    });
  });

  it('skips query when environment is not desktop', async () => {
    Object.defineProperty(appConfig, 'environment', { value: 'cluster', writable: true });
    renderClusterWithMocks([clusterUpdateAvailableMock]);

    await act(async () => {
      vi.advanceTimersByTime(1000);
    });

    await waitFor(() => {
      expect(screen.queryByTestId('cluster-update')).not.toBeInTheDocument();
    });
  });

  it('keys state per kubeContext', async () => {
    const context2 = 'other-cluster';
    const mock2: MockedResponse = {
      request: { query: CLUSTER_VERSION_STATUS, variables: { kubeContext: context2 } },
      result: {
        data: { clusterVersionStatus: { currentVersion: '0.8.0', latestVersion: '1.0.0', updateAvailable: true } },
      },
    };

    localStorage.setItem(`${CLUSTER_STORAGE_PREFIX}${KUBE_CONTEXT}`, JSON.stringify({ dismissedAt: Date.now() }));

    renderClusterWithMocks([clusterUpdateAvailableMock, mock2], context2, [KUBE_CONTEXT, context2]);

    await waitFor(() => {
      expect(screen.getByTestId('cluster-update')).toHaveTextContent('1.0.0');
    });
  });
});
