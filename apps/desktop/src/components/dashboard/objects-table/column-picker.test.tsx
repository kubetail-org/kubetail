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
import type { ComponentProps } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { ColumnPicker } from './column-picker';

const COLUMNS = [
  { key: 'name', header: 'Name', shown: true, hideable: false },
  { key: 'status', header: 'Status', shown: true, hideable: true },
  { key: 'node', header: 'No​de', shown: false, hideable: true },
];

async function open(props: Partial<ComponentProps<typeof ColumnPicker>> = {}) {
  const user = userEvent.setup();
  render(
    <ColumnPicker
      columns={COLUMNS}
      onToggle={vi.fn()}
      onReorder={vi.fn()}
      onReset={vi.fn()}
      changed={false}
      {...props}
    />,
  );
  await user.click(screen.getByRole('button', { name: 'Columns' }));
  return user;
}

// jsdom lays nothing out: each row is 24px tall, stacked in list order, so the keyboard
// sensor has places to move between.
function stackRows() {
  vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function rect(this: HTMLElement) {
    const row = this.closest<HTMLElement>('[data-column]');
    const i = row ? COLUMNS.findIndex((c) => c.key === row.dataset.column) : 0;
    return DOMRect.fromRect({ x: 0, y: i * 24, width: 160, height: 24 });
  });
}

afterEach(() => vi.restoreAllMocks());

describe('ColumnPicker', () => {
  it('lists the columns in order, checks the shown ones and reports a toggle', async () => {
    const onToggle = vi.fn();
    const user = await open({ onToggle });

    const [name, status, node] = await screen.findAllByRole('checkbox');
    expect(name).toBeChecked();
    expect(status).toBeChecked();
    expect(node).not.toBeChecked();

    await user.click(status);
    expect(onToggle).toHaveBeenLastCalledWith('status', false);
    await user.click(node);
    expect(onToggle).toHaveBeenLastCalledWith('node', true);
  });

  // A window shorter than the list: the header stays, the rows scroll.
  it('holds the popup to the height it has and scrolls the list', async () => {
    await open();
    const list = screen.getByTestId('column-list');
    expect(list).toHaveClass('overflow-y-auto');
    expect(list.parentElement).toHaveClass('max-h-(--available-height)');
  });

  it('keeps a column that cannot be hidden checked', async () => {
    const onToggle = vi.fn();
    const user = await open({ onToggle });
    const [name] = await screen.findAllByRole('checkbox');
    expect(name).toHaveAttribute('aria-disabled', 'true');
    await user.click(name);
    expect(onToggle).not.toHaveBeenCalled();
  });

  it('names a column through VisibleText', async () => {
    await open();
    expect(await screen.findByText(/^No/)).toHaveTextContent('No\\u{200B}de');
  });

  it('moves a column from the keyboard, reporting the new order', async () => {
    stackRows();
    const onReorder = vi.fn();
    const user = await open({ onReorder });

    (await screen.findByRole('button', { name: 'Move Name' })).focus();
    await user.keyboard(' ');
    await user.keyboard('{ArrowDown}');
    await user.keyboard(' ');
    expect(onReorder).toHaveBeenCalledWith(['status', 'name', 'node']);
  });

  it('reports nothing for a column dropped where it was', async () => {
    stackRows();
    const onReorder = vi.fn();
    const user = await open({ onReorder });
    (await screen.findByRole('button', { name: 'Move Status' })).focus();
    await user.keyboard(' ');
    await user.keyboard(' ');
    expect(onReorder).not.toHaveBeenCalled();
  });

  it('resets only once something differs from the defaults', async () => {
    const onReset = vi.fn();
    const user = await open({ onReset, changed: true });
    await user.click(screen.getByRole('button', { name: 'Reset' }));
    expect(onReset).toHaveBeenCalled();
  });

  it('disables Reset with nothing to reset', async () => {
    await open();
    expect(await screen.findByRole('button', { name: 'Reset' })).toBeDisabled();
  });
});
