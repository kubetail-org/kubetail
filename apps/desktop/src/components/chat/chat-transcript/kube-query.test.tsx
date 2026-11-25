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

import { render, screen, waitFor, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';

import { KubeQuery } from './kube-query';

const SQL = "SELECT name, restarts FROM objects WHERE kind = 'Pod'";

const answer = (over: Record<string, unknown> = {}) =>
  JSON.stringify({
    cluster: 'kind-kubetail',
    freshness: { status: 'watching' },
    columns: ['name', 'restarts'],
    more: false,
    rows: [
      ['worker-2xkqp', 14],
      ['worker-8hj4n', null],
    ],
    ...over,
  });

describe('KubeQuery', () => {
  it('draws its SQL, highlighted once the grammar arrives', async () => {
    const { container } = render(<KubeQuery sql={SQL} output={answer()} />);
    const pre = container.querySelector('pre')!;
    expect(pre).toHaveTextContent(SQL);
    await waitFor(() => expect(pre.querySelector('.hljs-string')).toHaveTextContent("'Pod'"));
    expect([...pre.querySelectorAll('.hljs-keyword')].map((s) => s.textContent)).toEqual(['SELECT', 'FROM', 'WHERE']);
    expect(pre).toHaveTextContent(SQL);
  });

  it('draws the rows under their columns, numbers right-aligned', () => {
    render(<KubeQuery sql={SQL} output={answer()} />);
    const table = screen.getByRole('table');
    expect(
      within(table)
        .getAllByRole('columnheader')
        .map((h) => h.textContent),
    ).toEqual(['name', 'restarts']);
    const rows = within(table).getAllByRole('row').slice(1);
    expect(
      rows.map((r) =>
        within(r)
          .getAllByRole('cell')
          .map((c) => c.textContent),
      ),
    ).toEqual([
      ['worker-2xkqp', '14'],
      ['worker-8hj4n', 'null'],
    ]);
    expect(within(rows[0]).getByText('14').closest('td')).toHaveClass('text-right');
    expect(within(rows[0]).getByText('worker-2xkqp').closest('td')).toHaveClass('text-left');
  });

  it('draws a nested cell as JSON', () => {
    render(<KubeQuery sql={SQL} output={answer({ columns: ['labels'], rows: [[{ app: 'worker' }]] })} />);
    expect(screen.getByRole('cell')).toHaveTextContent('{"app":"worker"}');
  });

  it('counts the rows, then names the cluster', () => {
    render(<KubeQuery sql={SQL} output={answer()} />);
    expect(screen.getByText('2 rows · kind-kubetail')).toBeInTheDocument();
  });

  it('counts the rows alone when the answer names no cluster', () => {
    render(<KubeQuery sql={SQL} output={answer({ cluster: '' })} />);
    expect(screen.getByText('2 rows')).toBeInTheDocument();
  });

  it('says one row, and says when rows were left out', () => {
    const { rerender } = render(<KubeQuery sql={SQL} output={answer({ rows: [['a', 1]] })} />);
    expect(screen.getByText('1 row · kind-kubetail')).toBeInTheDocument();
    rerender(<KubeQuery sql={SQL} output={answer({ more: true })} />);
    expect(screen.getByText('First 2 rows · kind-kubetail')).toBeInTheDocument();
  });

  it('draws no table for a query that named no columns', () => {
    render(<KubeQuery sql={SQL} output={answer({ columns: [], rows: [] })} />);
    expect(screen.queryByRole('table')).toBeNull();
    expect(screen.getByText('0 rows · kind-kubetail')).toBeInTheDocument();
  });

  it('draws an answer that is not rows as its text', () => {
    const output = '{"error":"sql","message":"no such column: nme"}';
    const { container } = render(<KubeQuery sql={SQL} output={output} />);
    expect(screen.queryByRole('table')).toBeNull();
    expect(container.querySelectorAll('pre')[1]).toHaveTextContent(output);
  });

  it('draws the SQL alone while the call has no output', () => {
    const { container } = render(<KubeQuery sql={SQL} output="" />);
    expect(container.querySelectorAll('pre')).toHaveLength(1);
  });

  it('spells invisible characters in a cell', () => {
    render(<KubeQuery sql={SQL} output={answer({ columns: ['name'], rows: [['ok‮gnp.exe']] })} />);
    expect(screen.getByRole('cell').querySelector('mark')).toHaveTextContent('\\u{202E}');
  });
});
