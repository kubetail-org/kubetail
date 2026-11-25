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
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';

import type { SandboxPath, SandboxPathFolder } from '@/lib/sandbox-path';

import { SandboxPathSection } from './sandbox-path';

const folder = (path: string, over: Partial<SandboxPathFolder> = {}): SandboxPathFolder => ({
  path,
  origin: 'Shell',
  target: path,
  searched: true,
  reason: null,
  addable: false,
  shared: false,
  ...over,
});

function pathOf(over: Partial<SandboxPath> = {}): SandboxPath {
  return {
    value: '$PATH',
    refusal: null,
    folders: [],
    fault: null,
    saving: false,
    refreshing: false,
    saveError: null,
    refreshError: null,
    save: vi.fn(async () => true),
    refresh: vi.fn(async () => true),
    ...over,
  };
}

const field = () => screen.getByRole('textbox', { name: 'PATH' });

describe('SandboxPathSection', () => {
  it('draws the saved value, and each folder with whether it is searched and why', () => {
    const folders = [
      folder('/opt/homebrew/bin'),
      folder('/Users/ren/bin', { origin: 'Typed', shared: true }),
      folder('/Users/ren/.local/bin', {
        searched: false,
        reason: 'No sandboxed command reads this folder; type it into your PATH to include it.',
        addable: true,
      }),
    ];
    render(<SandboxPathSection path={pathOf({ value: '~/bin:$PATH', folders })} onRefreshed={vi.fn()} />);
    expect(field()).toHaveValue('~/bin:$PATH');
    const rows = screen.getAllByRole('listitem');
    expect(rows[0]).toHaveTextContent('/opt/homebrew/bin');
    expect(rows[0]).toHaveTextContent('from your shell');
    expect(rows[1]).toHaveTextContent('typed');
    expect(rows[1]).toHaveTextContent('shared with its group');
    expect(rows[2]).toHaveTextContent('not searched');
    expect(rows[2]).toHaveTextContent('No sandboxed command reads this folder');
  });

  it('saves the field, then probes the executables, and holds Save while unchanged', async () => {
    const onRefreshed = vi.fn();
    const path = pathOf();
    render(<SandboxPathSection path={path} onRefreshed={onRefreshed} />);
    expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled();

    await userEvent.type(field(), ':/opt/bin');
    await userEvent.click(screen.getByRole('button', { name: 'Save' }));
    expect(path.save).toHaveBeenCalledWith('$PATH:/opt/bin');
    expect(onRefreshed).toHaveBeenCalled();
  });

  it('keeps the typed text and the refusal of a save that was not taken', async () => {
    const onRefreshed = vi.fn();
    const { rerender } = render(
      <SandboxPathSection path={pathOf({ save: vi.fn(async () => false) })} onRefreshed={onRefreshed} />,
    );
    await userEvent.clear(field());
    await userEvent.type(field(), 'bin');
    await userEvent.click(screen.getByRole('button', { name: 'Save' }));
    rerender(
      <SandboxPathSection
        path={pathOf({ save: vi.fn(async () => false), saveError: '"bin" is not a full path.' })}
        onRefreshed={onRefreshed}
      />,
    );
    expect(field()).toHaveValue('bin');
    expect(screen.getByText('"bin" is not a full path.')).toBeInTheDocument();
    expect(onRefreshed).not.toHaveBeenCalled();
  });

  // A save answered KUBETAIL_SAVED_UNREAD was taken: the field shows the saved
  // value again, and the message stays drawn.
  it('takes a save that was not read back, keeping its message', async () => {
    const message = 'Your PATH was saved, but Kubetail could not read the settings back. Reopen Settings to see them.';
    const { rerender } = render(<SandboxPathSection path={pathOf()} onRefreshed={vi.fn()} />);
    await userEvent.type(field(), ':/opt/bin');
    await userEvent.click(screen.getByRole('button', { name: 'Save' }));
    rerender(
      <SandboxPathSection path={pathOf({ value: '$PATH:/opt/bin', saveError: message })} onRefreshed={vi.fn()} />,
    );
    expect(field()).toHaveValue('$PATH:/opt/bin');
    expect(screen.getByText(message)).toBeInTheDocument();
  });

  it('shows a refused value with its refusal, editable, and Reset saves $PATH', async () => {
    const path = pathOf({ value: 'bin', refusal: '"bin" is not a full path.', folders: [] });
    render(<SandboxPathSection path={path} onRefreshed={vi.fn()} />);
    expect(screen.getByText(/"bin" is not a full path\./)).toBeInTheDocument();
    expect(field()).toBeEnabled();
    await userEvent.click(screen.getByRole('button', { name: 'Reset' }));
    expect(path.save).toHaveBeenCalledWith('$PATH');
  });

  it('offers Add exactly where it would help, appending the folder to the field without saving', async () => {
    const path = pathOf({
      folders: [
        folder('/Users/ren/.local/bin', {
          searched: false,
          reason: 'Not read.',
          addable: true,
          target: '/Users/ren/real',
        }),
        folder('/tmp/world', { searched: false, reason: 'Anyone on this machine can add programs to it.' }),
      ],
    });
    render(<SandboxPathSection path={path} onRefreshed={vi.fn()} />);
    const adds = screen.getAllByRole('button', { name: 'Add' });
    expect(adds).toHaveLength(1);
    await userEvent.click(adds[0]);
    expect(field()).toHaveValue('$PATH:/Users/ren/real');
    expect(path.save).not.toHaveBeenCalled();
  });

  // A text field draws no hidden character, so a draft holding one is drawn
  // again, each spelled, before it is saved.
  it('draws a draft that hides a character, spelled', async () => {
    render(<SandboxPathSection path={pathOf()} onRefreshed={vi.fn()} />);
    expect(screen.queryByTestId('draft')).toBeNull();
    await userEvent.type(field(), ':/opt/b\u202Ein');
    expect(screen.getByTestId('draft')).toHaveTextContent('$PATH:/opt/b\\u{202E}in');
  });

  it('imports from the shell, then probes the executables, but not on a refusal', async () => {
    const onRefreshed = vi.fn();
    const path = pathOf();
    const { unmount } = render(<SandboxPathSection path={path} onRefreshed={onRefreshed} />);
    await userEvent.click(screen.getByRole('button', { name: 'Import from shell' }));
    expect(path.refresh).toHaveBeenCalled();
    expect(onRefreshed).toHaveBeenCalled();
    unmount();

    onRefreshed.mockClear();
    render(
      <SandboxPathSection
        path={pathOf({ refresh: vi.fn(async () => false), refreshError: 'shell timed out' })}
        onRefreshed={onRefreshed}
      />,
    );
    await userEvent.click(screen.getByRole('button', { name: 'Import from shell' }));
    expect(onRefreshed).not.toHaveBeenCalled();
    expect(screen.getByText('shell timed out')).toBeInTheDocument();
  });

  it('holds Import while a refresh runs', () => {
    render(<SandboxPathSection path={pathOf({ refreshing: true })} onRefreshed={vi.fn()} />);
    expect(screen.getByRole('button', { name: 'Import from shell' })).toBeDisabled();
  });

  it('draws an empty field before the sidecar answers, and holds Save and Reset', () => {
    render(<SandboxPathSection path={pathOf({ value: undefined, folders: undefined })} onRefreshed={vi.fn()} />);
    expect(field()).toHaveValue('');
    expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Reset' })).toBeDisabled();
  });

  it('adds a folder as an entry of its own, whatever the field ends in', async () => {
    const path = pathOf({
      folders: [folder('/Users/ren/.local/bin', { searched: false, reason: 'Not read.', addable: true })],
    });
    render(<SandboxPathSection path={path} onRefreshed={vi.fn()} />);
    await userEvent.clear(field());
    await userEvent.click(screen.getByRole('button', { name: 'Add' }));
    expect(field()).toHaveValue('/Users/ren/.local/bin');

    await userEvent.clear(field());
    await userEvent.type(field(), '$PATH:');
    await userEvent.click(screen.getByRole('button', { name: 'Add' }));
    expect(field()).toHaveValue('$PATH:/Users/ren/.local/bin');
  });

  it('says when the shell could not be read', () => {
    render(<SandboxPathSection path={pathOf({ fault: 'zsh exited 1' })} onRefreshed={vi.fn()} />);
    expect(
      screen.getByText(
        "Kubetail could not read your shell's PATH: zsh exited 1. $PATH stands for the last one it read.",
      ),
    ).toBeInTheDocument();
  });
});
