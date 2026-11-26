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

import { render, screen, act } from '@testing-library/react';
import { Provider as UrqlProvider } from 'urql';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { flushSubscriptionsSynchronously, mockTauriCore, pushClusters } from '@/test-utils';

// Mocks ---------------------------------------------------------------

// Frames pushed here are asserted on immediately.
flushSubscriptionsSynchronously();

const { invokeMock, channels, channelFor, factory } = mockTauriCore();
vi.mock('@tauri-apps/api/core', () => factory());

const { createGraphqlClient } = await import('@/lib/graphql/client');
const { ClustersProvider } = await import('./clusters');
const { KubeConfigProvider, useKubeConfig, tlsUnverifiedReason } = await import('./kube-config');

// Helpers -------------------------------------------------------------

const flush = () => act(async () => {});

// A probe that renders the derived kubeconfig so tests can assert on it.
function Probe() {
  const { kubeConfig } = useKubeConfig();
  return <div data-testid="probe">{kubeConfig === null ? 'null' : JSON.stringify(kubeConfig)}</div>;
}

function renderProvider() {
  return render(
    <UrqlProvider value={createGraphqlClient()}>
      <ClustersProvider>
        <KubeConfigProvider>
          <Probe />
        </KubeConfigProvider>
      </ClustersProvider>
    </UrqlProvider>,
  );
}

describe('useKubeConfig', () => {
  beforeEach(() => {
    invokeMock.mockReset();
    channels.length = 0;
    let id = 0;
    invokeMock.mockImplementation(async (cmd: string) => {
      if (cmd === 'graphql_subscribe') {
        id += 1;
        return id;
      }
      if (cmd === 'graphql_unsubscribe') return undefined;
      throw new Error(`unexpected ${cmd}`);
    });
  });

  it('excludes disabled and orphaned clusters from the context list', async () => {
    renderProvider();
    await flush();

    await act(async () => {
      pushClusters(channelFor, [
        { id: 'a', name: 'prod', enabled: true, present: true, current: true },
        { id: 'b', name: 'staging', enabled: false, present: true },
        { id: 'c', name: 'gone', enabled: true, present: false },
      ]);
    });

    const probe = screen.getByTestId('probe');
    const kubeConfig = JSON.parse(probe.textContent ?? '');
    expect(kubeConfig.contexts).toEqual([
      {
        name: 'prod',
        cluster: 'prod-cluster',
        user: 'prod-user',
        clusterEntry: { server: 'https://prod.example:6443', insecureSkipTLSVerify: false },
      },
    ]);
    expect(kubeConfig.currentContext).toBe('prod');
  });

  // The window opens on the user's default cluster first, the kubeconfig's current
  // context second, and on nothing when neither is usable.
  it.each([
    { what: 'the default cluster wins over the current context', defaultID: 'b', currentID: 'a', expected: 'staging' },
    {
      what: 'a disabled default cluster falls to the current context',
      defaultID: 'c',
      currentID: 'a',
      expected: 'prod',
    },
    {
      what: 'an archived default cluster falls to the current context',
      defaultID: 'd',
      currentID: 'a',
      expected: 'prod',
    },
    { what: 'no default cluster falls to the current context', defaultID: undefined, currentID: 'a', expected: 'prod' },
    { what: "neither gives ''", defaultID: undefined, currentID: undefined, expected: '' },
  ])('defaultContext: $what', async ({ defaultID, currentID, expected }) => {
    renderProvider();
    await flush();

    await act(async () => {
      pushClusters(
        channelFor,
        [
          { id: 'a', name: 'prod' },
          { id: 'b', name: 'staging' },
          { id: 'c', name: 'off', enabled: false },
          { id: 'd', name: 'gone', present: false },
        ].map((r) => ({ ...r, current: r.id === currentID, isDefault: r.id === defaultID })),
      );
    });

    expect(JSON.parse(screen.getByTestId('probe').textContent ?? '').defaultContext).toBe(expected);
  });

  // Every switcher reads this context; a value that moved on each draw would draw them all.
  it('hands its readers the same value while the clusters stand still', async () => {
    const seen: unknown[] = [];
    function Reader() {
      seen.push(useKubeConfig());
      return null;
    }
    const client = createGraphqlClient();
    const tree = () => (
      <UrqlProvider value={client}>
        <ClustersProvider>
          <KubeConfigProvider>
            <Reader />
          </KubeConfigProvider>
        </ClustersProvider>
      </UrqlProvider>
    );
    const view = render(tree());
    await flush();
    await act(async () => {
      pushClusters(channelFor, [{ id: 'a', name: 'prod', enabled: true, present: true, current: true }]);
    });

    const before = seen.length;
    view.rerender(tree());
    expect(seen.length).toBeGreaterThan(before);
    expect(seen.at(-1)).toBe(seen[before - 1]);
  });
});

describe('tlsUnverifiedReason', () => {
  it('names skip-verify, which the file states outright', () => {
    expect(tlsUnverifiedReason({ server: 'https://prod.example:6443', insecureSkipTLSVerify: true })).toBe(
      'skip-verify',
    );
  });

  // No certificate is presented at all, so the entry's own flag says nothing.
  it('names plain http, whatever the flag says', () => {
    expect(tlsUnverifiedReason({ server: 'http://localhost:8080', insecureSkipTLSVerify: false })).toBe('plain-http');
  });

  it('reads the scheme case-insensitively, and does not match https', () => {
    expect(tlsUnverifiedReason({ server: 'HTTP://localhost:8080', insecureSkipTLSVerify: false })).toBe('plain-http');
    expect(tlsUnverifiedReason({ server: 'https://prod.example:6443', insecureSkipTLSVerify: false })).toBeNull();
  });

  // An entry the kubeconfig never defined resolves to nothing, so there is no
  // connection to call unverified; the record's own isPresent reports that.
  it('says nothing about a missing entry', () => {
    expect(tlsUnverifiedReason(null)).toBeNull();
  });
});
