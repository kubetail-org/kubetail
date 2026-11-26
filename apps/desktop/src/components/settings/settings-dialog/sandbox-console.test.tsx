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

import { act, render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import type { Cluster } from '@/lib/clusters';
import type { ConsoleRun, SandboxConsoleState } from '@/lib/sandbox-console';

const { state, present } = vi.hoisted(() => ({
  state: { current: null as SandboxConsoleState | null },
  present: { current: [] as Cluster[] },
}));
vi.mock('@/lib/sandbox-console', () => ({ useSandboxConsole: () => state.current }));
vi.mock('@/lib/cluster-scope', () => ({
  EVERY: '',
  useScope: () => ({ present: present.current, archived: [], cluster: undefined }),
}));

const { SandboxConsole } = await import('./sandbox-console');

function renderConsole(props: Partial<Parameters<typeof SandboxConsole>[0]> = {}) {
  const onOpenChange = vi.fn();
  const view = render(<SandboxConsole open onOpenChange={onOpenChange} networkAvailable {...props} />);
  return { ...view, onOpenChange };
}

function consoleState(over: Partial<SandboxConsoleState> = {}): SandboxConsoleState {
  return { runs: [], running: false, error: null, run: vi.fn(async () => true), stop: vi.fn(), ...over };
}

const ended = (over: Partial<ConsoleRun>): ConsoleRun => ({
  number: 1,
  command: 'true',
  clusterID: null,
  network: false,
  running: false,
  output: '',
  discarded: 0,
  exitCode: 0,
  notice: null,
  durationMs: 5,
  ...over,
});

beforeEach(() => {
  state.current = consoleState();
  present.current = [
    {
      id: 'c-1',
      spec: { name: '', source: { kubeconfig: { context: 'kind-kubetail' } } },
    } as unknown as Cluster,
  ];
});

describe('SandboxConsole', () => {
  it('opens on its empty state, and a chip fills the command', async () => {
    renderConsole();
    const dialog = await screen.findByRole('dialog', { name: 'Sandbox console' });
    expect(within(dialog).getByText('Try a command in the sandbox')).toBeInTheDocument();
    expect(within(dialog).getByText(/Nothing here reaches a chat or the model\./)).toBeInTheDocument();
    expect(within(dialog).getByText('Your always grants apply')).toBeInTheDocument();

    await userEvent.click(within(dialog).getByRole('button', { name: 'cat ~/.ssh/config' }));
    expect(within(dialog).getByRole('textbox', { name: 'Command' })).toHaveValue('cat ~/.ssh/config');
  });

  it('runs the typed command with the cluster and network picked, then clears the box', async () => {
    renderConsole();
    const box = await screen.findByRole('textbox', { name: 'Command' });
    expect(screen.getByRole('button', { name: 'Run' })).toBeDisabled();

    await userEvent.click(screen.getByRole('combobox', { name: 'Cluster' }));
    await userEvent.click(await screen.findByRole('option', { name: 'kind-kubetail' }));
    expect(screen.getByRole('combobox', { name: 'Cluster' })).toHaveTextContent('kind-kubetail');
    await userEvent.click(screen.getByRole('checkbox', { name: 'Network' }));
    await userEvent.type(box, 'kubectl get ns{Enter}');

    expect(state.current!.run).toHaveBeenCalledWith('kubectl get ns', 'c-1', true);
    expect(box).toHaveValue('');
  });

  it('sends nothing on Enter with nothing typed', async () => {
    renderConsole();
    await userEvent.type(await screen.findByRole('textbox', { name: 'Command' }), '  {Enter}');
    expect(state.current!.run).not.toHaveBeenCalled();
  });

  it('sends nothing on Enter while a command runs', async () => {
    state.current = consoleState({ running: true });
    renderConsole();
    await userEvent.type(await screen.findByRole('textbox', { name: 'Command' }), 'ls{Enter}');
    expect(state.current.run).not.toHaveBeenCalled();
  });

  it('keeps what was typed while the run was asked for', async () => {
    let answer!: (started: boolean) => void;
    state.current = consoleState({
      run: vi.fn(
        () =>
          new Promise<boolean>((resolve) => {
            answer = resolve;
          }),
      ),
    });
    renderConsole();
    const box = await screen.findByRole('textbox', { name: 'Command' });
    await userEvent.type(box, 'ls{Enter}');
    await userEvent.type(box, ' -la');

    await act(async () => answer(true));
    expect(box).toHaveValue('ls -la');
  });

  it('keeps the command a refused run leaves, and shows why', async () => {
    state.current = consoleState({ run: vi.fn(async () => false) });
    const { rerender } = renderConsole();
    const box = await screen.findByRole('textbox', { name: 'Command' });
    await userEvent.type(box, 'ls');
    await userEvent.click(screen.getByRole('button', { name: 'Run' }));
    expect(state.current.run).toHaveBeenCalledWith('ls', null, false);
    expect(box).toHaveValue('ls');

    state.current = consoleState({ error: 'a console command is already running' });
    rerender(<SandboxConsole open onOpenChange={vi.fn()} networkAvailable />);
    expect(screen.getByRole('alert')).toHaveTextContent('a console command is already running');
  });

  it('offers no cluster or each kubeconfig cluster', async () => {
    renderConsole();
    const cluster = await screen.findByRole('combobox', { name: 'Cluster' });
    expect(cluster).toHaveTextContent('No cluster');
    await userEvent.click(cluster);
    expect((await screen.findAllByRole('option')).map((o) => o.textContent)).toEqual(['No cluster', 'kind-kubetail']);
  });

  it('runs with no cluster once the user picks No cluster again', async () => {
    renderConsole();
    const cluster = await screen.findByRole('combobox', { name: 'Cluster' });
    await userEvent.click(cluster);
    await userEvent.click(await screen.findByRole('option', { name: 'kind-kubetail' }));
    await userEvent.click(cluster);
    await userEvent.click(await screen.findByRole('option', { name: 'No cluster' }));
    expect(cluster).toHaveTextContent('No cluster');

    await userEvent.type(screen.getByRole('textbox', { name: 'Command' }), 'ls{Enter}');
    expect(state.current!.run).toHaveBeenCalledWith('ls', null, false);
  });

  it('holds Run while the picked cluster is missing from the list, rather than run with none', async () => {
    const { rerender } = renderConsole();
    await userEvent.click(await screen.findByRole('combobox', { name: 'Cluster' }));
    await userEvent.click(await screen.findByRole('option', { name: 'kind-kubetail' }));
    present.current = [];
    rerender(<SandboxConsole open onOpenChange={vi.fn()} networkAvailable />);
    expect(screen.getByRole('combobox', { name: 'Cluster' })).toHaveTextContent('c-1');

    await userEvent.type(screen.getByRole('textbox', { name: 'Command' }), 'ls{Enter}');
    expect(screen.getByRole('button', { name: 'Run' })).toBeDisabled();
    expect(state.current!.run).not.toHaveBeenCalled();
  });

  it('offers the network only where the machine can give it, and never sends it where it cannot', async () => {
    const { unmount } = renderConsole();
    expect(await screen.findByRole('checkbox', { name: 'Network' })).not.toHaveAttribute('aria-disabled');
    unmount();
    renderConsole({ networkAvailable: false });
    expect(await screen.findByRole('checkbox', { name: 'Network' })).toHaveAttribute('aria-disabled', 'true');
    await userEvent.type(screen.getByRole('textbox', { name: 'Command' }), 'ls{Enter}');
    expect(state.current!.run).toHaveBeenCalledWith('ls', null, false);
  });

  it('offers Stop while a command runs, and closing stops it', async () => {
    state.current = consoleState({ running: true, runs: [ended({ running: true, exitCode: null, durationMs: 0 })] });
    const { onOpenChange } = renderConsole();
    expect(await screen.findByText('Running…')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Run' })).toBeNull();

    await userEvent.click(screen.getByRole('button', { name: 'Stop' }));
    expect(state.current.stop).toHaveBeenCalledTimes(1);

    await userEvent.click(screen.getByRole('button', { name: 'Close' }));
    expect(state.current.stop).toHaveBeenCalledTimes(2);
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });

  it('closing stops a run even while none is drawn running, as during a reconnect', async () => {
    const { onOpenChange } = renderConsole();
    await userEvent.click(await screen.findByRole('button', { name: 'Close' }));
    expect(state.current!.stop).toHaveBeenCalledTimes(1);
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });

  it('draws each run: its command, its output stripped and spelled, and how it ended', async () => {
    state.current = consoleState({
      runs: [
        ended({
          number: 1,
          command: 'cat ~/.aws/credentials',
          output: '\u001b[31mOperation not permitted\u001b[0m ',
          exitCode: 1,
          durationMs: 8,
        }),
        ended({ number: 2, command: 'kubectl get ns', output: 'default', durationMs: 1500, discarded: 12 }),
        ended({ number: 3, command: 'sleep 999', exitCode: 137, notice: 'Command cancelled (exit code 137)' }),
        ended({ number: 4, command: 'ls', exitCode: null, notice: 'could not start: the chat\u0027s cluster is gone' }),
        ended({ number: 5, command: 'true', exitCode: null, durationMs: 3 }),
      ],
    });
    renderConsole();
    const runs = await screen.findAllByRole('listitem');
    expect(runs[0]).toHaveTextContent('$ cat ~/.aws/credentials');
    expect(within(runs[0]).getByTestId('output').textContent).toBe('Operation not permitted\\u{20}');
    expect(within(runs[0]).getByText('exit 1 · 8ms')).toHaveClass('text-destructive');
    expect(within(runs[1]).getByText('exit 0 · 1s')).not.toHaveClass('text-destructive');
    expect(within(runs[1]).getByText('… 12 more bytes not shown')).toBeInTheDocument();
    expect(within(runs[2]).getByText('Command cancelled (exit code 137) · 5ms')).toHaveClass('text-destructive');
    expect(within(runs[3]).getByText(/could not start/)).toHaveClass('text-destructive');
    expect(within(runs[3]).queryByTestId('output')).toBeNull();
    expect(within(runs[4]).getByText('ended · 3ms')).toHaveClass('text-destructive');
    expect(screen.queryByText('Try a command in the sandbox')).toBeNull();
  });
});
