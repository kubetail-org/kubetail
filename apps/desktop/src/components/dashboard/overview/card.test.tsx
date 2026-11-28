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

import { act, screen, waitFor, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';

import { OverviewCard } from './card';
import type { CardColumn } from './card';
import { bodyRows, renderOnDashboard } from './testutil';

const COLUMNS: CardColumn<string>[] = [
  { key: 'name', header: 'Name', cell: (r) => r },
  { key: 'size', header: 'Size', cell: (r) => r.length },
];

function card(rows: string[] | undefined, always = false) {
  return (
    <OverviewCard
      title="Things"
      count={rows?.length}
      grid="grid-cols-[minmax(0,1fr)_40px]"
      columns={COLUMNS}
      rows={rows}
      rowKey={(r) => r}
      empty="No things"
      link={{ label: 'Show all', resource: 'pods', always }}
    />
  );
}

describe('OverviewCard', () => {
  it('draws its title, count, column headers and rows', async () => {
    await renderOnDashboard(card(['a', 'bb']));
    const region = screen.getByRole('region', { name: 'Things' });
    expect(within(region).getByRole('heading', { name: 'Things' })).toBeInTheDocument();
    expect(region).toHaveTextContent('Things2');
    expect(screen.getAllByRole('columnheader').map((h) => h.textContent)).toEqual(['Name', 'Size']);
    expect(bodyRows('Things').map((r) => r.textContent)).toEqual(['a1', 'bb2']);
  });

  it('shows three rows at most, and its link only when it cuts some', async () => {
    await renderOnDashboard(card(['a', 'b', 'c']));
    expect(bodyRows('Things')).toHaveLength(3);
    expect(screen.queryByRole('link')).not.toBeInTheDocument();
  });

  it('links to its page, keeping the search, when there are more rows than it shows', async () => {
    const { router } = await renderOnDashboard(
      card(['a', 'b', 'c', 'd']),
      '/dashboard?kubeContext=prod&namespace=shop',
    );
    expect(bodyRows('Things')).toHaveLength(3);
    act(() => screen.getByRole('link', { name: 'Show all' }).click());
    await waitFor(() =>
      expect(router.state.location.search).toEqual({ kubeContext: 'prod', namespace: 'shop', resource: 'pods' }),
    );
  });

  it('always shows a link marked so', async () => {
    await renderOnDashboard(card([], true));
    expect(screen.getByRole('link', { name: 'Show all' })).toBeInTheDocument();
  });

  it('says it is empty in one row', async () => {
    await renderOnDashboard(card([]));
    expect(bodyRows('Things').map((r) => r.textContent)).toEqual(['No things']);
  });

  it('draws a spinner, and no count or link, while loading', async () => {
    await renderOnDashboard(card(undefined, true));
    const region = screen.getByRole('region', { name: 'Things' });
    expect(region).toHaveTextContent(/^Things.*Loading…$/);
    expect(screen.queryByRole('table')).not.toBeInTheDocument();
    expect(screen.queryByRole('link')).not.toBeInTheDocument();
  });
});
