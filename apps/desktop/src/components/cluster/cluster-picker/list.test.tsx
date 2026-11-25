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

import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';

import type { Cluster } from '@/lib/clusters';
import type { KubeClusterEntry, KubeContextInfo } from '@/lib/kube-config';
import { clusterRecord, expectCodeText } from '@/test-utils';

import { ClusterList } from './list';

const context = (name: string, clusterEntry: KubeClusterEntry | null = null): KubeContextInfo => ({
  name,
  cluster: name,
  user: name,
  clusterEntry,
});

function renderList({
  names = ['prod', 'staging', 'dev'],
  current = 'staging',
  clusters = [],
  onSelect = vi.fn<(name: string) => void>(),
  contexts = names.map((name) => context(name)),
}: {
  names?: string[];
  current?: string;
  clusters?: Cluster[] | null;
  onSelect?: (name: string) => void;
  contexts?: KubeContextInfo[];
} = {}) {
  render(
    <ClusterList
      contexts={contexts}
      current={current}
      clusters={clusters}
      onSelect={onSelect}
      filterRef={{ current: null }}
    />,
  );
  return onSelect;
}

const rows = () => screen.getAllByRole('button');
const row = (name: string) => screen.getByText(name, { selector: '[data-testid="cluster-name"]' }).closest('button')!;

describe('ClusterList', () => {
  it('renders a row per context, the current one first and marked current', () => {
    renderList();
    expect(rows().map((r) => within(r).getByTestId('cluster-name').textContent)).toEqual(['staging', 'prod', 'dev']);
    expect(row('staging')).toHaveAttribute('aria-current', 'true');
    expect(row('prod')).not.toHaveAttribute('aria-current');
  });

  it('shows a row’s record tone on its dot and its summary', () => {
    renderList({ clusters: [clusterRecord({ context: 'prod', connected: 'False' })] });
    expect(within(row('prod')).getByTestId('cluster-dot')).toHaveAttribute('data-tone', 'error');
    expect(within(row('prod')).getByText('Disconnected · Stalled')).toBeInTheDocument();
  });

  it('reads a context with no record as not reached', () => {
    renderList({ clusters: [clusterRecord({ context: 'prod' })] });
    expect(within(row('dev')).getByTestId('cluster-dot')).toHaveAttribute('data-tone', 'muted');
    expect(within(row('dev')).getByText('Not reached')).toBeInTheDocument();
  });

  it('reads every context as not reached before the clusters snapshot lands', () => {
    renderList({ clusters: null });
    expect(screen.getAllByText('Not reached')).toHaveLength(3);
  });

  it('shows the TLS badge on a row whose cluster entry skips verification, and not otherwise', () => {
    renderList({
      contexts: [
        context('prod', { server: 'https://prod:6443', insecureSkipTLSVerify: false }),
        context('dev', { server: 'https://dev:6443', insecureSkipTLSVerify: true }),
      ],
      current: 'prod',
    });
    expect(within(row('dev')).getByText('Unverified TLS')).toBeInTheDocument();
    expect(within(row('prod')).queryByText('Unverified TLS')).not.toBeInTheDocument();
  });

  it('calls onSelect with a row’s name on its click', async () => {
    const user = userEvent.setup();
    const onSelect = renderList();
    await user.click(row('dev'));
    expect(onSelect).toHaveBeenCalledWith('dev');
  });

  it('hides the filter at five contexts', () => {
    renderList({ names: ['a', 'b', 'c', 'd', 'e'], current: 'a' });
    expect(screen.queryByRole('textbox', { name: 'Filter clusters' })).not.toBeInTheDocument();
  });

  it('shows the filter at six contexts, as code text', () => {
    renderList({ names: ['a', 'b', 'c', 'd', 'e', 'f'], current: 'a' });
    expectCodeText(screen.getByRole('textbox', { name: 'Filter clusters' }));
  });

  it('narrows the rows case-insensitively, and says so when none match', async () => {
    const user = userEvent.setup();
    renderList({ names: ['Prod-EU', 'prod-us', 'staging', 'dev', 'qa', 'perf'], current: 'staging' });
    const filter = screen.getByRole('textbox', { name: 'Filter clusters' });

    await user.type(filter, 'PROD');
    expect(screen.getAllByTestId('cluster-name').map((n) => n.textContent)).toEqual(['Prod-EU', 'prod-us']);

    await user.clear(filter);
    await user.type(filter, 'nothing');
    expect(screen.queryAllByTestId('cluster-name')).toHaveLength(0);
    expect(screen.getByText('No cluster matches.')).toBeInTheDocument();
  });

  it('picks the first visible row on Enter', async () => {
    const user = userEvent.setup();
    const onSelect = renderList({ names: ['prod', 'staging', 'dev', 'qa', 'perf', 'devtest'], current: 'prod' });

    await user.type(screen.getByRole('textbox', { name: 'Filter clusters' }), 'dev{Enter}');
    expect(onSelect).toHaveBeenCalledWith('dev');
  });

  it('picks nothing on Enter when no row matches', async () => {
    const user = userEvent.setup();
    const onSelect = renderList({ names: ['a', 'b', 'c', 'd', 'e', 'f'], current: 'a' });

    await user.type(screen.getByRole('textbox', { name: 'Filter clusters' }), 'zzz{Enter}');
    expect(onSelect).not.toHaveBeenCalled();
  });
});
