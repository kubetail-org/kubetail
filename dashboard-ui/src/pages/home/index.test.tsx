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

import type { MockLink } from '@apollo/client/testing';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { createMemoryHistory } from 'history';
import { Router } from 'react-router';
import type { Mock } from 'vitest';

import { useSession } from '@/lib/auth';
import * as dashboardOps from '@/lib/graphql/dashboard/ops';
import { PreferencesProvider } from '@/lib/preferences';
import { mocks } from '@/mocks/home';
import { renderElement } from '@/test-utils';

import Page from '.';

describe('auth tests', () => {
  it('blocks access if user is unauthenticated', () => {
    const history = createMemoryHistory();

    render(
      <Router location={history.location} navigator={history}>
        <Page />
      </Router>,
    );

    // Assertions
    expect(history.location.pathname).toBe('/auth/login');
  });

  it('reders page if user is authenticated', async () => {
    // Mock auth
    (useSession as Mock).mockReturnValue({
      session: { user: 'test' },
    });

    const { getByText } = renderElement(<Page />, mocks);

    // Check initial message before kubeConfig is resolved
    await waitFor(() => {
      expect(getByText('Connecting...')).toBeInTheDocument();
    });

    // Check message after kubeConfig is resolved
    await waitFor(() => {
      expect(getByText('Dashboard')).toBeInTheDocument();
    });
  });
});

describe('initial connection', () => {
  it('shows loading message while waiting for kubeConfig to return', async () => {
    // Mock auth
    (useSession as Mock).mockReturnValue({
      session: { user: 'test' },
    });

    const { getByText } = renderElement(<Page />, [
      {
        request: {
          query: dashboardOps.KUBE_CONFIG_WATCH,
        },
        delay: Infinity,
      },
    ]);

    // Check looading message
    await waitFor(() => {
      expect(getByText('Connecting...')).toBeInTheDocument();
    });
  });
});

describe('workload table', () => {
  const deploymentNames = ['delta', 'alpha', 'foxtrot', 'charlie', 'echo', 'bravo'];

  // Listed first, so it answers the deployments query ahead of the empty list in `mocks`.
  const deploymentsListMock: MockLink.MockedResponse = {
    request: {
      query: dashboardOps.HOME_DEPLOYMENTS_LIST_FETCH,
      variables: { kubeContext: 'test1', namespace: '', continue: '' },
    },
    result: {
      data: {
        appsV1DeploymentsList: {
          __typename: 'AppsV1DeploymentList',
          metadata: { __typename: 'MetaV1ListMeta', continue: '', resourceVersion: 'v1' },
          items: deploymentNames.map((name) => ({
            __typename: 'AppsV1Deployment',
            id: `id-${name}`,
            metadata: {
              __typename: 'MetaV1ObjectMeta',
              namespace: 'default',
              name,
              uid: `uid-${name}`,
              creationTimestamp: '2024-01-01T00:00:00Z',
              deletionTimestamp: null,
              resourceVersion: 'v1',
              ownerReferences: [],
            },
          })),
        },
      },
    },
  };

  const namePattern = new RegExp(`^(${deploymentNames.join('|')})$`);
  const renderedNames = () => screen.queryAllByText(namePattern).map((el) => el.textContent);

  beforeEach(() => {
    (useSession as Mock).mockReturnValue({ session: { user: 'test' } });
  });

  it('sorts by name, toggles the order and shows the rest on demand', async () => {
    renderElement(
      <PreferencesProvider>
        <Page />
      </PreferencesProvider>,
      [deploymentsListMock, ...mocks],
    );

    await waitFor(() => {
      expect(renderedNames()).toEqual(['alpha', 'bravo', 'charlie', 'delta', 'echo']);
    });

    // Every workload kind shares one <table>; the deployments' header is the last one before its rows.
    const alpha = screen.getByText('alpha');
    const nameHeaders = screen
      .getAllByText('Name')
      .filter((el) => el.compareDocumentPosition(alpha) & Node.DOCUMENT_POSITION_FOLLOWING);
    fireEvent.click(nameHeaders.at(-1) as HTMLElement);
    await waitFor(() => {
      expect(renderedNames()).toEqual(['foxtrot', 'echo', 'delta', 'charlie', 'bravo']);
    });

    fireEvent.click(screen.getByText('Show 1 more...'));
    await waitFor(() => {
      expect(renderedNames()).toEqual(['foxtrot', 'echo', 'delta', 'charlie', 'bravo', 'alpha']);
    });
  });
});
