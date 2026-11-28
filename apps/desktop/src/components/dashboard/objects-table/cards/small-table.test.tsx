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
import { describe, expect, it } from 'vitest';

import { SmallTable } from './small-table';

describe('SmallTable', () => {
  const rows = [
    { k: 'a', v: 'one' },
    { k: 'b', v: 'two' },
  ];

  it('draws a header row, then a row per item, each on the caller’s grid', () => {
    render(
      <SmallTable
        grid="grid-cols-[90px_minmax(0,1fr)]"
        columns={[
          { header: 'Key', cell: (r) => r.k },
          { header: 'Value', cell: (r) => r.v, className: 'font-mono' },
        ]}
        rows={rows}
        rowKey={(r) => r.k}
        footer={<button type="button">more</button>}
      />,
    );
    const [header, ...body] = screen.getAllByRole('row');
    expect(
      within(header)
        .getAllByRole('columnheader')
        .map((h) => h.textContent),
    ).toEqual(['Key', 'Value']);
    expect(header).toHaveClass('grid-cols-[90px_minmax(0,1fr)]', 'py-2', 'font-medium');
    expect(
      body.map((r) =>
        within(r)
          .getAllByRole('cell')
          .map((c) => c.textContent),
      ),
    ).toEqual([
      ['a', 'one'],
      ['b', 'two'],
    ]);
    expect(body[0]).toHaveClass('grid-cols-[90px_minmax(0,1fr)]', 'border-t', 'py-2.5');
    expect(within(body[0]).getAllByRole('cell')[1]).toHaveClass('truncate', 'font-mono');
    expect(screen.getByRole('button', { name: 'more' })).toBeInTheDocument();
  });
});
