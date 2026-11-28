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

import { chevronColumn } from './chevron-column';

type Row = { name: string };

describe('chevronColumn', () => {
  it('is a pinned, headerless column with one button for the chevron alone', () => {
    const column = chevronColumn<Row>({ nameOf: (r) => r.name, onOpen: vi.fn() });
    expect(column).toMatchObject({ key: 'actions', header: '', pinned: true, width: 46 });
    render(<>{column.cell({ name: 'web' })}</>);
    expect(screen.getAllByRole('button')).toHaveLength(1);
  });

  it('widens by the buttons drawn before the chevron', () => {
    const column = chevronColumn<Row>({
      nameOf: (r) => r.name,
      onOpen: vi.fn(),
      before: { buttons: 1, cell: () => <button type="button">menu</button> },
    });
    expect(column.width).toBe(74);
    render(<>{column.cell({ name: 'web' })}</>);
    expect(screen.getAllByRole('button').map((b) => b.textContent)).toEqual(['menu', '']);
  });

  it('opens the row, named as it spells it', async () => {
    const onOpen = vi.fn();
    const row = { name: 'web​1' };
    render(<>{chevronColumn<Row>({ nameOf: (r) => r.name, onOpen }).cell(row)}</>);
    await userEvent.click(screen.getByRole('button', { name: 'Open web\\u{200B}1' }));
    expect(onOpen).toHaveBeenCalledWith(row);
  });
});
