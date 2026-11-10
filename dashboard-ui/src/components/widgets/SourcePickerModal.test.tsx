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
import { fireEvent, screen, waitFor } from '@testing-library/react';
import { useState } from 'react';
import { useLocation } from 'react-router';

import * as dashboardOps from '@/lib/graphql/dashboard/ops';
import { PreferencesProvider } from '@/lib/preferences';
import { renderElement } from '@/test-utils';

import SourcePickerModal from './SourcePickerModal';

const deploymentNames = ['web', 'api', 'worker'];

const deploymentItems = deploymentNames.map((name) => ({
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
  },
}));

const deploymentsListMock: MockLink.MockedResponse = {
  request: { query: dashboardOps.SOURCE_PICKER_DEPLOYMENTS_LIST_FETCH, variables: () => true },
  result: {
    data: {
      appsV1DeploymentsList: {
        __typename: 'AppsV1DeploymentList',
        metadata: { __typename: 'MetaV1ListMeta', continue: '', resourceVersion: 'v1' },
        items: deploymentItems,
      },
    },
  },
};

// Every other query and watch the modal opens stays pending.
const pendingMocks: MockLink.MockedResponse[] = Object.entries(dashboardOps)
  .filter(([name]) => name.startsWith('SOURCE_PICKER_') && name !== 'SOURCE_PICKER_DEPLOYMENTS_LIST_FETCH')
  .map(([, query]) => ({ request: { query, variables: () => true }, delay: Infinity, maxUsageCount: Infinity }));

// Renders the modal beside the router's search string and a button that reopens it.
const Harness = () => {
  const [open, setOpen] = useState(true);
  return (
    <PreferencesProvider>
      <div data-testid="location-search">{useLocation().search}</div>
      <button type="button" onClick={() => setOpen(true)}>
        Reopen
      </button>
      <SourcePickerModal open={open} onOpenChange={setOpen} />
    </PreferencesProvider>
  );
};

const webCheckbox = () => document.querySelector('[data-id="id-web"]') as HTMLElement;

const locationSources = () => new URLSearchParams(screen.getByTestId('location-search').textContent).getAll('source');

const renderedNames = () =>
  screen.queryAllByText(new RegExp(`^(${deploymentNames.join('|')})$`)).map((el) => el.textContent);

describe('SourcePickerModal', () => {
  it('lists a workload kind sorted by name and toggles the order', async () => {
    renderElement(
      <PreferencesProvider>
        <SourcePickerModal open />
      </PreferencesProvider>,
      [deploymentsListMock, ...pendingMocks],
    );

    fireEvent.click(screen.getByText(/^Deployments/));
    await waitFor(() => {
      expect(renderedNames()).toEqual(['api', 'web', 'worker']);
    });

    fireEvent.click(screen.getByText('Name'));
    await waitFor(() => {
      expect(renderedNames()).toEqual(['worker', 'web', 'api']);
    });
  });

  it('writes the chosen sources to the URL without reloading the page', async () => {
    renderElement(<Harness />, [deploymentsListMock, ...pendingMocks]);

    fireEvent.click(screen.getByText(/^Deployments/));
    await waitFor(() => {
      expect(webCheckbox()).toBeInTheDocument();
    });

    fireEvent.click(webCheckbox());
    fireEvent.click(screen.getByRole('button', { name: 'Update' }));

    await waitFor(() => {
      expect(locationSources()).toEqual(['default:deployments/web/*']);
    });
  });

  it('starts each opening from the sources in the URL', async () => {
    renderElement(<Harness />, [deploymentsListMock, ...pendingMocks]);

    fireEvent.click(screen.getByText(/^Deployments/));
    await waitFor(() => {
      expect(webCheckbox()).toBeInTheDocument();
    });

    fireEvent.click(webCheckbox());
    expect(webCheckbox()).toHaveAttribute('aria-checked', 'true');

    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    await waitFor(() => {
      expect(screen.queryByRole('button', { name: 'Cancel' })).not.toBeInTheDocument();
    });

    fireEvent.click(screen.getByRole('button', { name: 'Reopen' }));
    fireEvent.click(await screen.findByText(/^Deployments/));
    await waitFor(() => {
      expect(webCheckbox()).toHaveAttribute('aria-checked', 'false');
    });
    expect(locationSources()).toEqual([]);
  });
});
