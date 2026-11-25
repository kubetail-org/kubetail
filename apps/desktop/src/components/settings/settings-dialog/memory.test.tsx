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

import { fireEvent, render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { type Cluster, displayName } from '@/lib/clusters';
import type { Memory } from '@/lib/memories';
import { clusterRecord } from '@/test-utils';

// The seams: the cluster records, the kubeconfig's contexts (none before the
// records), the window's cluster, the memories watch and urql's mutation hook.
const { state, watched, saveMock } = vi.hoisted(() => ({
  state: { clusters: null as Cluster[] | null, contexts: [] as string[], activeID: undefined as string | undefined },
  watched: { ids: [] as (string | undefined)[], memories: [] as unknown[], phase: 'live' },
  saveMock: vi.fn(),
}));
vi.mock('@/lib/clusters', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/clusters')>()),
  useClusters: () => ({ clusters: state.clusters, connected: true }),
}));
vi.mock('@/lib/kube-config', () => ({
  useKubeConfig: () => ({ kubeConfig: state.clusters && { contexts: state.contexts.map((name) => ({ name })) } }),
}));
vi.mock('@/lib/active-cluster', () => ({ useActiveCluster: () => ({ clusterID: state.activeID }) }));
vi.mock('@/lib/memories', () => ({
  useMemories: (clusterID: string | undefined) => {
    watched.ids.push(clusterID);
    return { memories: watched.memories, phase: watched.phase };
  },
}));
vi.mock('urql', () => ({ useMutation: () => [{}, saveMock] }));
vi.mock('@/lib/dialog', () => ({ useDialogHost: () => null }));

const { MemorySettings } = await import('./memory');

function cluster(context: string, { present = true, name = '' } = {}): Cluster {
  const c = clusterRecord({ context });
  return {
    ...c,
    spec: { ...c.spec, name },
    status: { ...c.status, source: { kubeconfig: { ...c.status.source.kubeconfig!, isPresent: present } } },
  };
}

const memory = (over: Partial<Memory> = {}): Memory => ({
  id: 'm1',
  clusterID: 'id-prod',
  name: 'pages',
  body: 'Treat its pods as prod.',
  writtenBy: 'Model',
  updatedAt: '2026-09-20T10:00:00Z',
  ...over,
});

beforeEach(() => {
  vi.clearAllMocks();
  state.clusters = [
    cluster('prod'),
    cluster('dev'),
    cluster('zeta-old', { present: false }),
    cluster('alpha-old', { present: false }),
  ];
  state.activeID = 'id-dev';
  state.contexts = ['prod', 'dev'];
  watched.ids = [];
  watched.memories = [];
  watched.phase = 'live';
  saveMock.mockResolvedValue({ data: { memorySave: { id: 'm9' } } });
});

const trigger = () => screen.getByRole('combobox', { name: 'Applies to' });

async function pick(user: ReturnType<typeof userEvent.setup>, name: string | RegExp) {
  await user.click(trigger());
  await user.click(await screen.findByRole('option', { name }));
}

describe('MemorySettings', () => {
  describe('scope', () => {
    it('applies to every cluster by default', () => {
      render(<MemorySettings />);
      expect(trigger()).toHaveTextContent('Every cluster');
    });

    it('lists every cluster, then the kubeconfig, then the archived by name', async () => {
      const user = userEvent.setup();
      render(<MemorySettings />);
      await user.click(trigger());

      const listbox = await screen.findByRole('listbox');
      expect(
        within(listbox)
          .getAllByRole('option')
          .map((o) => o.textContent),
      ).toEqual(['Every cluster', 'prod', 'dev', 'alpha-old', 'zeta-old']);
      expect(listbox.querySelector('[data-slot="select-separator"]')).toBeInTheDocument();
      const groups = within(listbox).getAllByRole('group');
      expect(groups.map((g) => g.firstChild?.textContent)).toEqual(['Kubeconfig', 'Archived']);
      expect(
        within(groups[1])
          .getAllByRole('option')
          .map((o) => o.textContent),
      ).toEqual(['alpha-old', 'zeta-old']);
    });

    it('leaves out the Archived group when nothing is archived', async () => {
      state.clusters = [cluster('prod')];
      const user = userEvent.setup();
      render(<MemorySettings />);
      await user.click(trigger());

      await screen.findByRole('listbox');
      expect(screen.queryByText('Archived')).toBeNull();
    });

    it('leaves out a context Kubetail has no record for', async () => {
      state.clusters = [cluster('prod')];
      state.contexts = ['prod', 'new'];
      const user = userEvent.setup();
      render(<MemorySettings />);
      await user.click(trigger());

      const listbox = await screen.findByRole('listbox');
      expect(
        within(listbox)
          .getAllByRole('option')
          .map((o) => o.textContent),
      ).toEqual(['Every cluster', 'prod']);
    });

    it('names the picked cluster on the trigger', async () => {
      const user = userEvent.setup();
      render(<MemorySettings />);
      await pick(user, 'alpha-old');
      expect(trigger()).toHaveTextContent('alpha-old');
    });

    it('falls back to every cluster once the picked record is gone', async () => {
      const user = userEvent.setup();
      const { rerender } = render(<MemorySettings />);
      await pick(user, 'alpha-old');

      state.clusters = state.clusters!.filter((c) => displayName(c) !== 'alpha-old');
      rerender(<MemorySettings />);
      expect(trigger()).toHaveTextContent('Every cluster');
    });

    // Cluster names are attacker-controlled: an escape must be spelled out.
    it('spells out invisible characters in cluster names', async () => {
      state.clusters = [cluster('prod', { name: 'prod\u202e' })];
      const user = userEvent.setup();
      render(<MemorySettings />);
      await user.click(trigger());

      const option = await screen.findByRole('option', { name: /prod/ });
      expect(within(option).getByTitle('An invisible character')).toBeInTheDocument();
      await user.click(option);
      expect(within(trigger()).getByTitle('An invisible character')).toBeInTheDocument();
      expect(trigger().textContent).not.toContain('\u202e');
    });
  });

  describe('notes', () => {
    const ALL = [
      memory(),
      memory({ id: 'm2', clusterID: null, name: 'prefs', body: 'Answer briefly.' }),
      memory({ id: 'm3', clusterID: 'id-dev', name: 'dev', body: 'Dev is shared.' }),
    ];

    it('heads the notes with what they are for', () => {
      render(<MemorySettings />);
      expect(screen.getByRole('heading', { name: 'Notes' })).toBeInTheDocument();
      expect(screen.getByText('Read at the start of each chat. You can edit or remove any note.')).toBeInTheDocument();
    });

    it("shows every cluster's notes alone, off the window's cluster's watch", () => {
      watched.memories = ALL;
      render(<MemorySettings />);
      expect(watched.ids.at(-1)).toBe('id-dev');
      expect(screen.getByText('Answer briefly.')).toBeInTheDocument();
      expect(screen.queryByText('Treat its pods as prod.')).toBeNull();
      expect(screen.queryByText('Dev is shared.')).toBeNull();
    });

    it('watches the first record for every cluster when the window has none', () => {
      state.activeID = undefined;
      render(<MemorySettings />);
      expect(watched.ids.at(-1)).toBe('id-prod');
    });

    it("shows a cluster's own notes alone, off its own watch", async () => {
      watched.memories = ALL;
      const user = userEvent.setup();
      render(<MemorySettings />);
      await pick(user, 'prod');

      expect(watched.ids.at(-1)).toBe('id-prod');
      expect(screen.getByText('Treat its pods as prod.')).toBeInTheDocument();
      expect(screen.queryByText('Answer briefly.')).toBeNull();
      expect(screen.queryByText('Dev is shared.')).toBeNull();
    });

    it('waits for the snapshot, never drawing the empty state', () => {
      watched.phase = 'connecting';
      render(<MemorySettings />);
      expect(screen.queryByText('No notes yet.')).toBeNull();
      expect(screen.getByText('Loading...')).toBeInTheDocument();
    });

    it('waits for the clusters, never saying none was reached', () => {
      state.clusters = null;
      state.activeID = undefined;
      render(<MemorySettings />);
      expect(screen.queryByText('Kubetail has not reached a cluster yet.')).toBeNull();
      expect(screen.getByText('Loading...')).toBeInTheDocument();
    });

    it('says so when Kubetail has no cluster record', () => {
      state.clusters = [];
      state.activeID = undefined;
      render(<MemorySettings />);
      expect(watched.ids.at(-1)).toBeUndefined();
      expect(screen.getByText('Kubetail has not reached a cluster yet.')).toBeInTheDocument();
      expect(screen.getByRole('button', { name: 'Add note' })).toBeDisabled();
    });
  });

  describe('instructions', () => {
    const box = () => screen.getByRole('textbox', { name: 'Instructions' });

    it('draws the instructions, given to every chat, over the notes', () => {
      render(<MemorySettings />);
      expect(screen.getByText('Given to every chat.')).toBeInTheDocument();
      expect(box().compareDocumentPosition(screen.getByRole('heading', { name: 'Notes' }))).toBe(
        Node.DOCUMENT_POSITION_FOLLOWING,
      );
    });

    it('drops their text on a scope switch', async () => {
      const user = userEvent.setup();
      render(<MemorySettings />);
      fireEvent.change(box(), { target: { value: 'Answer briefly.' } });
      await pick(user, 'prod');
      expect(box()).toHaveValue('');
    });
  });

  describe('Add note', () => {
    const add = () => screen.getByRole('button', { name: 'Add note' });
    const newNote = () => screen.queryByRole('textbox', { name: 'New note' });

    it.each([
      ['every cluster', undefined, null],
      ['a cluster', 'prod', 'id-prod'],
    ])("saves a new note for %s with the scope's cluster", async (_, scope, clusterID) => {
      const user = userEvent.setup();
      render(<MemorySettings />);
      if (scope) await pick(user, scope);

      await user.click(add());
      expect(add()).toBeDisabled();
      await user.type(newNote()!, 'Use traefik.');
      await user.keyboard('{Meta>}{Enter}{/Meta}');

      expect(saveMock).toHaveBeenCalledWith({
        input: { id: null, clusterID, name: 'use-traefik', body: 'Use traefik.' },
      });
      expect(newNote()).toBeNull();
      expect(add()).toBeEnabled();
    });

    it('closes an open editor on a scope switch', async () => {
      const user = userEvent.setup();
      render(<MemorySettings />);
      await user.click(add());
      expect(newNote()).toBeInTheDocument();

      await pick(user, 'prod');
      expect(newNote()).toBeNull();
      expect(add()).toBeEnabled();
    });
  });

  describe('Clear memory', () => {
    const NOTES = [
      memory(),
      memory({ id: 'm2', clusterID: 'id-prod', name: 'more', body: 'Pages go to ops.' }),
      memory({ id: 'm3', clusterID: null, name: 'prefs', body: 'Answer briefly.' }),
    ];

    async function openConfirm(user: ReturnType<typeof userEvent.setup>) {
      await user.click(screen.getByRole('button', { name: 'Clear memory' }));
      return screen.findByRole('dialog');
    }

    it("asks before clearing every cluster's memory, counting its notes", async () => {
      watched.memories = NOTES;
      const user = userEvent.setup();
      render(<MemorySettings />);

      const confirm = await openConfirm(user);
      expect(confirm).toHaveAccessibleName('Clear memory for every cluster?');
      expect(confirm).toHaveAccessibleDescription('This deletes its instructions and 1 notes.');
    });

    it("asks before clearing a cluster's memory, naming it and counting its notes", async () => {
      watched.memories = NOTES;
      const user = userEvent.setup();
      render(<MemorySettings />);
      await pick(user, 'prod');

      const confirm = await openConfirm(user);
      expect(confirm).toHaveAccessibleName('Clear memory for prod?');
      expect(confirm).toHaveAccessibleDescription(
        "This deletes its instructions and 2 notes. Other clusters aren't affected.",
      );
    });

    it('spells out invisible characters in the name', async () => {
      state.clusters = [cluster('prod', { name: 'prod\u202e' })];
      const user = userEvent.setup();
      render(<MemorySettings />);
      await pick(user, /prod/);

      const confirm = await openConfirm(user);
      expect(within(confirm).getByTitle('An invisible character')).toBeInTheDocument();
      expect(confirm.textContent).not.toContain('\u202e');
    });

    it('holds Clear disabled, and Cancel closes', async () => {
      const user = userEvent.setup();
      render(<MemorySettings />);

      const confirm = await openConfirm(user);
      expect(within(confirm).getByRole('button', { name: 'Clear' })).toBeDisabled();
      await user.click(within(confirm).getByRole('button', { name: 'Cancel' }));
      expect(screen.queryByRole('dialog')).toBeNull();
    });
  });
});
