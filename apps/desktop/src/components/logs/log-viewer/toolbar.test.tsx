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

import { fireEvent, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';

import { expectCodeText } from '@/test-utils';

import type { Column } from './columns';
import { LogViewerToolbar, type LogViewerToolbarProps } from './toolbar';

function renderToolbar(over: Partial<LogViewerToolbarProps> = {}) {
  const props: LogViewerToolbarProps = {
    loading: false,
    paused: false,
    columns: ['Timestamp', 'Dot', 'Message'],
    wrap: false,
    grep: 'error',
    grepError: null,
    onJumpTo: vi.fn(),
    onStart: vi.fn(),
    onEnd: vi.fn(),
    onPausedChange: vi.fn(),
    onColumnsChange: vi.fn(),
    onWrapChange: vi.fn(),
    onGrepChange: vi.fn(),
    ...over,
  };
  render(<LogViewerToolbar {...props} />);
  return props;
}

const button = (name: string) => screen.getByRole('button', { name });

describe('LogViewerToolbar', () => {
  it("turns off the webview's suggestions in the grep box", () => {
    renderToolbar();
    expectCodeText(screen.getByRole('textbox', { name: 'Grep' }));
  });

  it('jumps to the start and the end', async () => {
    const props = renderToolbar();
    await userEvent.click(button('Jump to the start'));
    expect(props.onStart).toHaveBeenCalledOnce();
    await userEvent.click(button('Jump to the end'));
    expect(props.onEnd).toHaveBeenCalledOnce();
  });

  it('jumps to a time picked in its popover', async () => {
    const props = renderToolbar();
    await userEvent.click(button('Jump to a time'));
    await userEvent.click(button('5'));
    expect(props.onJumpTo).toHaveBeenCalledOnce();
  });

  it('offers Pause while playing and Play while paused', async () => {
    const playing = renderToolbar();
    await userEvent.click(button('Pause'));
    expect(playing.onPausedChange).toHaveBeenCalledWith(true);
  });

  it('plays again from paused', async () => {
    const paused = renderToolbar({ paused: true });
    expect(screen.queryByRole('button', { name: 'Pause' })).not.toBeInTheDocument();
    await userEvent.click(button('Play'));
    expect(paused.onPausedChange).toHaveBeenCalledWith(false);
  });

  it('disables every control while loading', () => {
    renderToolbar({ loading: true });
    ['Jump to a time', 'Jump to the start', 'Pause', 'Jump to the end', 'View'].forEach((name) =>
      expect(button(name)).toBeDisabled(),
    );
  });

  it('lists every column but the message, and no position, then Wrap', async () => {
    renderToolbar();
    await userEvent.click(button('View'));
    const boxes = await screen.findAllByRole('checkbox');
    expect(boxes.map((box) => box.getAttribute('aria-label') ?? box.closest('label')?.textContent)).toEqual([
      'Timestamp',
      'Dot',
      'Pod',
      'Container',
      'Node',
      'Region',
      'Zone',
      'OS',
      'Arch',
      'Wrap',
    ]);
    expect(screen.getByRole('checkbox', { name: 'Timestamp' })).toBeChecked();
    expect(screen.getByRole('checkbox', { name: 'Pod' })).not.toBeChecked();
  });

  it('adds and removes a column, keeping column order and the message', async () => {
    const props = renderToolbar();
    await userEvent.click(button('View'));
    await userEvent.click(await screen.findByRole('checkbox', { name: 'Pod' }));
    expect(props.onColumnsChange).toHaveBeenLastCalledWith<[Column[]]>(['Timestamp', 'Dot', 'Pod', 'Message']);
    await userEvent.click(screen.getByRole('checkbox', { name: 'Timestamp' }));
    expect(props.onColumnsChange).toHaveBeenLastCalledWith<[Column[]]>(['Dot', 'Message']);
  });

  it('toggles wrap', async () => {
    const props = renderToolbar();
    await userEvent.click(button('View'));
    await userEvent.click(await screen.findByRole('checkbox', { name: 'Wrap' }));
    expect(props.onWrapChange).toHaveBeenCalledWith(true);
  });

  describe('the grep box', () => {
    const box = () => screen.getByRole('textbox', { name: 'Grep' });

    it('starts with the grep in effect and submits a new one on Enter', async () => {
      const props = renderToolbar();
      expect(box()).toHaveValue('error');
      await userEvent.clear(box());
      await userEvent.type(box(), 'warn|fail');
      expect(props.onGrepChange).not.toHaveBeenCalled();
      await userEvent.type(box(), '{Enter}');
      expect(props.onGrepChange).toHaveBeenCalledWith('warn|fail');
    });

    it('leaves Enter to an input method composing a character', () => {
      const props = renderToolbar();
      fireEvent.keyDown(box(), { key: 'Enter', isComposing: true });
      expect(props.onGrepChange).not.toHaveBeenCalled();
      fireEvent.keyDown(box(), { key: 'Enter' });
      expect(props.onGrepChange).toHaveBeenCalledWith('error');
    });

    it('submits an empty box as no grep', async () => {
      const props = renderToolbar();
      await userEvent.clear(box());
      await userEvent.type(box(), '{Enter}');
      expect(props.onGrepChange).toHaveBeenCalledWith('');
    });

    it('marks the box and says why a grep was refused', () => {
      renderToolbar({ grepError: 'missing closing ): `(a`' });
      expect(box()).toHaveAttribute('aria-invalid', 'true');
      expect(box()).toHaveAccessibleDescription('missing closing ): `(a`');
    });

    it('is valid with no refusal', () => {
      renderToolbar();
      expect(box()).toHaveAttribute('aria-invalid', 'false');
    });
  });
});
