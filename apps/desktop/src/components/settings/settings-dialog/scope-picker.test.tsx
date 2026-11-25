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
import { useState } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import type { Cluster } from '@/lib/clusters';
import { clusterRecord } from '@/test-utils';

const { state } = vi.hoisted(() => ({ state: { clusters: null as Cluster[] | null, contexts: [] as string[] } }));
vi.mock('@/lib/clusters', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/clusters')>()),
  useClusters: () => ({ clusters: state.clusters, connected: true }),
}));
vi.mock('@/lib/kube-config', () => ({
  useKubeConfig: () => ({ kubeConfig: state.clusters && { contexts: state.contexts.map((name) => ({ name })) } }),
}));

const { EVERY, useScope } = await import('@/lib/cluster-scope');
const { ScopePicker } = await import('./scope-picker');

function cluster(context: string, present = true): Cluster {
  const c = clusterRecord({ context });
  return {
    ...c,
    status: { ...c.status, source: { kubeconfig: { ...c.status.source.kubeconfig!, isPresent: present } } },
  };
}

function Picker({ count }: { count?: (scope: string) => number | undefined }) {
  const [picked, setPicked] = useState(EVERY);
  const scope = useScope(picked);
  return (
    <>
      <ScopePicker scope={scope} onPick={setPicked} count={count} />
      <p>picked: {scope.cluster?.id ?? 'every'}</p>
    </>
  );
}

beforeEach(() => {
  state.clusters = [cluster('prod'), cluster('dev'), cluster('zeta-old', false), cluster('alpha-old', false)];
  state.contexts = ['prod', 'dev'];
});

describe('ScopePicker', () => {
  it('is every cluster until one is picked, and again once the picked record is gone', async () => {
    const user = userEvent.setup();
    const { rerender } = render(<Picker />);
    const trigger = screen.getByRole('combobox', { name: 'Applies to' });
    expect(trigger).toHaveTextContent('Every cluster');
    expect(screen.getByText('picked: every')).toBeInTheDocument();

    await user.click(trigger);
    await user.click(await screen.findByRole('option', { name: 'dev' }));
    expect(trigger).toHaveTextContent('dev');
    expect(screen.getByText('picked: id-dev')).toBeInTheDocument();

    state.clusters = [cluster('prod')];
    rerender(<Picker />);
    expect(trigger).toHaveTextContent('Every cluster');
  });

  it('lists every cluster, then the kubeconfig, then the archived by name, each with its count', async () => {
    const user = userEvent.setup();
    render(<Picker count={(scope) => (scope === 'id-prod' ? 3 : undefined)} />);
    await user.click(screen.getByRole('combobox', { name: 'Applies to' }));

    const options = within(await screen.findByRole('listbox')).getAllByRole('option');
    expect(options.map((o) => o.textContent)).toEqual(['Every cluster', 'prod3', 'dev', 'alpha-old', 'zeta-old']);
  });

  it('lists nothing but every cluster before the records arrive', async () => {
    const user = userEvent.setup();
    state.clusters = null;
    render(<Picker />);
    await user.click(screen.getByRole('combobox', { name: 'Applies to' }));
    expect(within(await screen.findByRole('listbox')).getAllByRole('option')).toHaveLength(1);
  });
});
