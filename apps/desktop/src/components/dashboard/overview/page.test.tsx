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
import { describe, expect, it, vi } from 'vitest';

import type { Overview } from '@/lib/overview';

const { overview } = vi.hoisted(() => {
  const current: Overview = { active: true, syncing: false };
  return { overview: { current } };
});
vi.mock('@/lib/overview', () => ({ useOverview: () => overview.current }));
// Each section is covered by its own suite; this one checks what each is handed.
vi.mock('./tiles', () => ({ Tiles: (props: object) => <div data-testid="tiles">{JSON.stringify(props)}</div> }));
vi.mock('./needs-attention', () => ({
  NeedsAttention: (props: object) => <div data-testid="needs-attention">{JSON.stringify(props)}</div>,
}));
vi.mock('./namespaces', () => ({
  Namespaces: (props: object) => <div data-testid="namespaces">{JSON.stringify(props)}</div>,
}));
vi.mock('./recent-warnings', () => ({
  RecentWarnings: (props: object) => <div data-testid="recent-warnings">{JSON.stringify(props)}</div>,
}));

const { OverviewPage } = await import('./page');

const propsOf = (testId: string) => JSON.parse(screen.getByTestId(testId).textContent ?? '{}');

describe('OverviewPage', () => {
  it('hands each section its part of the overview', () => {
    const counts = { running: 1, failing: 1, pending: 0, total: 2 };
    overview.current = {
      active: true,
      syncing: false,
      nodes: { ready: 1, total: 1 },
      pods: { counts, problems: [] },
      namespaces: [{ name: 'shop', workloads: 1, pods: 2 }],
      warnings: [],
    };
    render(<OverviewPage />);
    expect(propsOf('tiles')).toEqual({
      nodes: { ready: 1, total: 1 },
      pods: counts,
      namespaces: [{ name: 'shop', workloads: 1, pods: 2 }],
      warnings: [],
    });
    expect(propsOf('needs-attention')).toEqual({ problems: [], syncing: false });
    expect(propsOf('namespaces')).toEqual({ rows: [{ name: 'shop', workloads: 1, pods: 2 }] });
    expect(propsOf('recent-warnings')).toEqual({ warnings: [], syncing: false });
    expect(screen.queryByText(/still syncing/i)).not.toBeInTheDocument();
  });

  it('draws every section while their data loads', () => {
    overview.current = { active: true, syncing: false };
    render(<OverviewPage />);
    expect(propsOf('needs-attention')).toEqual({ syncing: false });
    expect(screen.getByTestId('recent-warnings')).toBeInTheDocument();
  });

  it('says the counts may be short while the cache catches up', () => {
    overview.current = {
      active: true,
      syncing: true,
      pods: { counts: { running: 0, failing: 0, pending: 0, total: 0 }, problems: [] },
    };
    render(<OverviewPage />);
    expect(screen.getByText('Still syncing — counts may be incomplete.')).toBeInTheDocument();
    expect(propsOf('needs-attention')).toEqual({ problems: [], syncing: true });
    expect(propsOf('recent-warnings')).toEqual({ syncing: true });
  });

  it('says the cluster has no synced cache yet', () => {
    overview.current = { active: false, syncing: false };
    render(<OverviewPage />);
    expect(screen.getByText(/no synced cache for this cluster yet/i)).toBeInTheDocument();
    expect(screen.queryByTestId('tiles')).not.toBeInTheDocument();
  });
});
