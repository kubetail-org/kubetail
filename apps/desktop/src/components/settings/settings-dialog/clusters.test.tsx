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
import { beforeEach, describe, expect, it, type Mock, vi } from 'vitest';

import type { CacheContents } from '@/lib/cluster-diagnostics';
import type { Cluster } from '@/lib/clusters';
import { clusterRecord } from '@/test-utils';

const { state, mutations, useClusterCacheStatsStateWatch } = vi.hoisted(() => ({
  state: { clusters: null } as { clusters: Cluster[] | null },
  mutations: {} as Record<string, Mock<(variables: unknown) => Promise<unknown>>>,
  useClusterCacheStatsStateWatch: vi.fn(),
}));
vi.mock('urql', () => ({
  useQuery: vi.fn(),
  useMutation: (doc: string) => {
    mutations[doc] ??= vi.fn();
    return [{}, mutations[doc]];
  },
}));
// Each document is its name, so the mock can tell the mutations apart.
vi.mock('@/gql', () => ({ graphql: (source: string) => /(?:query|mutation|subscription) (\w+)/.exec(source)![1] }));
vi.mock('@/lib/clusters', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/clusters')>()),
  useClusters: () => ({ clusters: state.clusters, connected: true }),
}));
vi.mock('@/lib/cluster-diagnostics', () => ({ useClusterCacheStatsStateWatch }));

const { ClustersSettings } = await import('./clusters');

type Row = {
  context: string;
  name?: string;
  enabled?: boolean;
  syncEnabled?: boolean;
  isDefault?: boolean;
  present?: boolean;
  server?: string | null;
  user?: string;
  cache?: boolean;
};

// A cluster as useClusters serves it, in the state the row describes.
function cluster({
  context,
  name = '',
  enabled = true,
  syncEnabled = true,
  isDefault = false,
  present = true,
  server = `https://${context}.example:6443`,
  user = `${context}-user`,
  cache = true,
}: Row): Cluster {
  const c = clusterRecord({ context });
  const kubeconfig = c.status.source.kubeconfig!;
  return {
    ...c,
    spec: { ...c.spec, name, enabled, syncEnabled, isDefault },
    status: {
      ...c.status,
      source: {
        kubeconfig: {
          ...kubeconfig,
          isPresent: present,
          cluster: { name: context, entry: server === null ? null : { server, insecureSkipTLSVerify: false } },
          user: { name: user },
        },
      },
    },
    activeCache: cache ? c.activeCache : null,
  };
}

const ON_DISK: CacheContents = {
  exists: true,
  bytes: 1.5 * 1024 * 1024,
  dbBytes: 0,
  walBytes: 0,
  shmBytes: 0,
  objectCount: 0,
  kindCount: 0,
};

beforeEach(() => {
  vi.clearAllMocks();
  state.clusters = null;
  useClusterCacheStatsStateWatch.mockReturnValue(ON_DISK);
});

const group = (name: string) => screen.getByRole('group', { name });

describe('ClustersSettings', () => {
  it('draws nothing before the clusters Bookmark', () => {
    const { container } = render(<ClustersSettings />);
    expect(container).toBeEmptyDOMElement();
  });

  it('lists present clusters under Kubeconfig and the rest under Archived', () => {
    state.clusters = [
      cluster({ context: 'prod' }),
      cluster({ context: 'off', enabled: false }),
      cluster({ context: 'old', present: false }),
    ];
    render(<ClustersSettings />);

    const rows = (name: string) => within(group(name)).getAllByRole('listitem');
    expect(rows('Kubeconfig').map((r) => r.textContent)).toEqual([
      expect.stringContaining('prod'),
      expect.stringContaining('off'),
    ]);
    expect(within(group('Kubeconfig')).getByText('Clusters Kubetail found in your kubeconfig.')).toBeInTheDocument();
    expect(rows('Archived').map((r) => r.textContent)).toEqual([expect.stringContaining('old')]);
    expect(
      within(group('Archived')).getByText('No longer in your kubeconfig. Caches are kept until you delete them.'),
    ).toBeInTheDocument();
  });

  it('hides the Archived group when no cluster is archived', () => {
    state.clusters = [cluster({ context: 'prod' })];
    render(<ClustersSettings />);
    expect(screen.queryByRole('group', { name: 'Archived' })).toBeNull();
    expect(screen.queryByText('Archived')).toBeNull();
  });

  it('says so when the kubeconfig has no cluster', () => {
    state.clusters = [cluster({ context: 'old', present: false })];
    render(<ClustersSettings />);
    expect(within(group('Kubeconfig')).getByText('No clusters in your kubeconfig.')).toBeInTheDocument();
    expect(within(group('Kubeconfig')).queryByRole('list')).toBeNull();
  });

  it('draws the name over the server and the user, the context standing in for a missing name', () => {
    state.clusters = [cluster({ context: 'prod', name: 'Production' }), cluster({ context: 'dev' })];
    render(<ClustersSettings />);

    const [prod, dev] = within(group('Kubeconfig')).getAllByRole('listitem');
    expect(within(prod).getByText('Production')).toBeInTheDocument();
    expect(within(prod).getByText('https://prod.example:6443')).toBeInTheDocument();
    expect(within(prod).getByText('prod-user')).toBeInTheDocument();
    expect(within(dev).getByText('dev')).toBeInTheDocument();
  });

  it('draws a dash for a cluster with neither a name nor a context', () => {
    state.clusters = [cluster({ context: '' })];
    render(<ClustersSettings />);
    expect(screen.getByRole('listitem').querySelector('[data-slot="name"]')).toHaveTextContent('—');
  });

  it('draws a dash for a missing server and user', () => {
    state.clusters = [cluster({ context: 'prod', server: null, user: '' })];
    render(<ClustersSettings />);

    const row = within(group('Kubeconfig')).getByRole('listitem');
    expect(within(row).getAllByText('—')).toHaveLength(2);
  });

  // The pane is narrow at the window's default width: neither value may push the other out.
  it('truncates the server and the user alike, each titled with its whole value', () => {
    const user = 'arn:aws:iam::123456789012:user/a-very-long-user-name';
    state.clusters = [cluster({ context: 'prod', user })];
    render(<ClustersSettings />);

    const server = screen.getByTitle('https://prod.example:6443');
    const userName = screen.getByTitle(user);
    for (const el of [server, userName]) expect(el).toHaveClass('min-w-0', 'truncate');
    expect(userName).not.toHaveClass('shrink-0');
  });

  it('spells invisible characters in the titles too', () => {
    state.clusters = [cluster({ context: 'prod', server: 'https://x\u202e', user: 'u\u0007' })];
    render(<ClustersSettings />);

    expect(screen.getByTitle('https://x\\u{202E}')).toBeInTheDocument();
    expect(screen.getByTitle('u\\u{7}')).toBeInTheDocument();
  });

  // Cluster data is attacker-controlled: an escape must be spelled out, never passed through.
  it('spells out control characters in cluster data', () => {
    state.clusters = [cluster({ context: 'prod', name: 'prod\u001b[2J', server: 'https://x\u202e', user: 'u\u0007' })];
    render(<ClustersSettings />);

    const row = within(group('Kubeconfig')).getByRole('listitem');
    expect(within(row).getAllByTitle('An invisible character')).toHaveLength(3);
    for (const raw of ['\u0007', '\u001b', '\u202e']) expect(row.textContent).not.toContain(raw);
  });

  it('marks a disabled row, its name muted, and a paused one', () => {
    state.clusters = [cluster({ context: 'off', enabled: false }), cluster({ context: 'paused', syncEnabled: false })];
    render(<ClustersSettings />);

    const [off, paused] = within(group('Kubeconfig')).getAllByRole('listitem');
    expect(within(off).getByText('Disabled')).toBeInTheDocument();
    expect(within(off).getByText('off')).toHaveClass('text-muted-foreground');
    expect(within(paused).getByText('Sync paused')).toBeInTheDocument();
    expect(within(paused).getByText('paused')).toHaveClass('text-popover-foreground');
  });

  it("marks the default's row alone, an archived one too", () => {
    state.clusters = [
      cluster({ context: 'prod', isDefault: true }),
      cluster({ context: 'staging' }),
      cluster({ context: 'old', present: false, isDefault: true }),
    ];
    render(<ClustersSettings />);

    const [prod, staging] = within(group('Kubeconfig')).getAllByRole('listitem');
    expect(within(prod).getByText('Default')).toBeInTheDocument();
    expect(within(staging).queryByText('Default')).toBeNull();
    expect(within(within(group('Archived')).getByRole('listitem')).getByText('Default')).toBeInTheDocument();
  });

  it("shows the cache's size, and a dash with no cache or nothing on disk", () => {
    state.clusters = [cluster({ context: 'a' }), cluster({ context: 'b', cache: false }), cluster({ context: 'c' })];
    useClusterCacheStatsStateWatch.mockImplementation((_id: string, cacheID: string) =>
      cacheID === 'cache-c' ? { ...ON_DISK, exists: false } : ON_DISK,
    );
    render(<ClustersSettings />);

    const sizes = within(group('Kubeconfig'))
      .getAllByRole('listitem')
      .map((r) => r.querySelector('[data-slot="size"]')?.textContent);
    expect(sizes).toEqual(['1.5 MB', '—', '—']);
    expect(useClusterCacheStatsStateWatch).toHaveBeenCalledWith('id-a', 'cache-a', false);
    // With no cache there is nothing to watch.
    expect(useClusterCacheStatsStateWatch).toHaveBeenCalledWith('id-b', '', true);
  });

  it('shows a dash until the cache stats answer', () => {
    state.clusters = [cluster({ context: 'a' })];
    useClusterCacheStatsStateWatch.mockReturnValue(null);
    render(<ClustersSettings />);
    expect(screen.getByRole('listitem').querySelector('[data-slot="size"]')).toHaveTextContent('—');
  });

  describe('row menu', () => {
    async function openMenu(name: string) {
      const user = userEvent.setup();
      await user.click(screen.getByRole('button', { name: `Actions for ${name}` }));
      const menu = await screen.findByRole('menu');
      return {
        user,
        menu,
        items: () =>
          within(menu)
            .getAllByRole('menuitem')
            .map((i) => i.textContent),
      };
    }

    it('lists its items in order, and each fires its mutation', async () => {
      state.clusters = [cluster({ context: 'prod' })];
      render(<ClustersSettings />);

      const { user, items } = await openMenu('prod');
      expect(items()).toEqual(['Set as default', 'Pause syncing', 'Disable', 'Reset cache']);
      expect(within(screen.getByRole('menu')).getByRole('separator')).toBeInTheDocument();

      await user.click(screen.getByRole('menuitem', { name: 'Set as default' }));
      expect(mutations.ClusterDefaultSet).toHaveBeenCalledWith({ id: 'id-prod', isDefault: true });

      await user.click(screen.getByRole('button', { name: 'Actions for prod' }));
      await user.click(await screen.findByRole('menuitem', { name: 'Pause syncing' }));
      expect(mutations.ClusterSyncEnabledSet).toHaveBeenCalledWith({ id: 'id-prod', syncEnabled: false });

      await user.click(screen.getByRole('button', { name: 'Actions for prod' }));
      await user.click(await screen.findByRole('menuitem', { name: 'Disable' }));
      expect(mutations.ClusterEnabledSet).toHaveBeenCalledWith({ id: 'id-prod', enabled: false });

      await user.click(screen.getByRole('button', { name: 'Actions for prod' }));
      await user.click(await screen.findByRole('menuitem', { name: 'Reset cache' }));
      expect(mutations.ClusterCacheClear).toHaveBeenCalledWith({ id: 'cache-prod' });
    });

    it("offers Remove as default on the default's row", async () => {
      state.clusters = [cluster({ context: 'prod', isDefault: true })];
      render(<ClustersSettings />);

      const { user } = await openMenu('prod');
      await user.click(screen.getByRole('menuitem', { name: 'Remove as default' }));
      expect(mutations.ClusterDefaultSet).toHaveBeenCalledWith({ id: 'id-prod', isDefault: false });
    });

    it('offers Resume on a paused row', async () => {
      state.clusters = [cluster({ context: 'prod', syncEnabled: false })];
      render(<ClustersSettings />);

      const { user } = await openMenu('prod');
      await user.click(screen.getByRole('menuitem', { name: 'Resume syncing' }));
      expect(mutations.ClusterSyncEnabledSet).toHaveBeenCalledWith({ id: 'id-prod', syncEnabled: true });
    });

    it('offers Enable alone, then Reset cache, on a disabled row', async () => {
      state.clusters = [cluster({ context: 'off', enabled: false })];
      render(<ClustersSettings />);

      const { user, items } = await openMenu('off');
      expect(items()).toEqual(['Enable', 'Reset cache']);
      await user.click(screen.getByRole('menuitem', { name: 'Enable' }));
      expect(mutations.ClusterEnabledSet).toHaveBeenCalledWith({ id: 'id-off', enabled: true });
    });

    it.each([
      ['no cache', { cache: false }, ON_DISK],
      ['nothing on disk', {}, { ...ON_DISK, exists: false }],
    ])('disables Reset cache with %s', async (_, row, contents) => {
      state.clusters = [cluster({ context: 'prod', ...row })];
      useClusterCacheStatsStateWatch.mockReturnValue(contents);
      render(<ClustersSettings />);

      await openMenu('prod');
      expect(screen.getByRole('menuitem', { name: 'Reset cache' })).toHaveAttribute('aria-disabled', 'true');
    });
  });

  it('deletes an archived cluster from its trash button, leaving the row to the watch', async () => {
    const user = userEvent.setup();
    state.clusters = [cluster({ context: 'old', present: false })];
    render(<ClustersSettings />);

    expect(screen.queryByRole('button', { name: 'Actions for old' })).toBeNull();
    await user.click(screen.getByRole('button', { name: 'Delete old' }));

    expect(mutations.ClusterDelete).toHaveBeenCalledWith({ id: 'id-old' });
    expect(within(group('Archived')).getByRole('listitem')).toHaveTextContent('old');
  });
});
