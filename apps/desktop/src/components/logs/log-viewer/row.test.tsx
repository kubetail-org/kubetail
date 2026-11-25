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

import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { useEffect, useState } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { LogLine } from '@/lib/log-viewer';

import { type Column, DEFAULT_COLUMNS, dotColor } from './columns';
import {
  CutNotice,
  LOG_GAP_ROW_H,
  LOG_ROW_H,
  LOG_ROW_PX,
  LogViewerRow,
  type LogViewerRowProps,
  estimateRowHeight,
  gridTemplate,
} from './row';

// Built in local time, since the row draws local time.
const local = (...parts: [number, number, number, number, number, number, number]) => new Date(...parts).toISOString();

const line = (over: Partial<LogLine> = {}): LogLine => ({
  timestamp: local(2026, 9, 15, 9, 5, 3, 123),
  cursor: 'c1',
  message: 'GET /healthz 200',
  truncatedBytes: 0,
  stream: { namespace: 'prod', pod: 'webapp-7f9c', container: 'istio-proxy', previous: false },
  place: { node: 'node-a', region: 'eu-west-1', zone: 'eu-west-1b', os: 'linux', arch: 'arm64' },
  gap: null,
  ...over,
});

const ALL: Column[] = ['Timestamp', 'Dot', 'Pod', 'Container', 'Node', 'Region', 'Zone', 'OS', 'Arch', 'Message'];
const widths = new Map();

function Row({
  rowKey = 1,
  columns = DEFAULT_COLUMNS,
  wrap = false,
  of = line(),
  ...selection
}: {
  rowKey?: number;
  columns?: Column[];
  wrap?: boolean;
  of?: LogLine;
} & Partial<LogViewerRowProps>) {
  return (
    <LogViewerRow
      {...selection}
      line={of}
      rowKey={rowKey}
      columns={columns}
      gridTemplate={gridTemplate(columns)}
      widths={widths}
      rowWidth={0}
      wrap={wrap}
    />
  );
}

const row = () => screen.getByTestId('log-row');
const cellOf = (col: string) => row().querySelector<HTMLElement>(`[data-col="${col}"]`);
const cols = () => [...row().querySelectorAll<HTMLElement>('[data-col]')].map((el) => el.dataset.col);

beforeEach(() => {
  vi.useFakeTimers();
  vi.setSystemTime(new Date(2026, 9, 15, 12, 0, 0));
});

afterEach(() => {
  vi.useRealTimers();
});

describe('LogViewerRow', () => {
  it('draws the position, then the default columns: the time, the dot and the message', () => {
    render(<Row />);
    expect(cols()).toEqual(['Pos', 'Timestamp', 'Dot', 'Message']);
    expect(cellOf('Timestamp')).toHaveTextContent('09:05:03.123');
    expect(cellOf('Message')).toHaveTextContent('GET /healthz 200');
    expect(row()).toHaveClass(LOG_ROW_H);
  });

  it('draws every column as its own cell, the pod and the container apart', () => {
    render(<Row columns={ALL} />);
    expect(cols()).toEqual(['Pos', ...ALL]);
    expect(cellOf('Pod')).toHaveTextContent(/^webapp-7f9c$/);
    expect(cellOf('Container')).toHaveTextContent(/^istio-proxy$/);
    expect(cellOf('Node')).toHaveTextContent('node-a');
    expect(cellOf('Region')).toHaveTextContent('eu-west-1');
    expect(cellOf('Zone')).toHaveTextContent('eu-west-1b');
    expect(cellOf('OS')).toHaveTextContent('linux');
    expect(cellOf('Arch')).toHaveTextContent('arm64');
  });

  it('draws no cell for a hidden column', () => {
    render(<Row columns={['Pod', 'Message']} />);
    expect(cols()).toEqual(['Pos', 'Pod', 'Message']);
  });

  it('lays the row out as a grid: the position, a track per column, the message taking the rest', () => {
    expect(gridTemplate(['Timestamp', 'Pod', 'Message'])).toBe('minmax(48px, auto) auto auto 1fr');
    render(<Row />);
    expect(row()).toHaveStyle({ gridTemplateColumns: 'minmax(48px, auto) auto auto 1fr' });
  });

  it('draws the position as a sign and a magnitude, and a bare 0 where the view landed', () => {
    const { rerender } = render(<Row rowKey={3} />);
    expect(cellOf('Pos')).toHaveTextContent(/^\+3$/);
    rerender(<Row rowKey={-12} />);
    expect(cellOf('Pos')).toHaveTextContent(/^-12$/);
    rerender(<Row rowKey={0} />);
    expect(cellOf('Pos')).toHaveTextContent(/^0$/);
  });

  it('marks the landed row on its position cell, not the row', () => {
    const { rerender } = render(<Row rowKey={1} />);
    expect(cellOf('Pos')).not.toHaveClass('border-l-2');
    rerender(<Row rowKey={0} />);
    expect(cellOf('Pos')).toHaveClass('border-l-2');
    expect(row()).not.toHaveClass('border-l-2');
  });

  it('colours the dot by its container, hides it, and names the stream to a screen reader', () => {
    render(<Row />);
    const dot = cellOf('Dot')!;
    const circle = dot.querySelector<HTMLElement>('[aria-hidden="true"]')!;
    expect(circle.style.backgroundColor).not.toBe('');
    expect(circle).toHaveStyle({ backgroundColor: dotColor('prod', 'webapp-7f9c', 'istio-proxy') });
    expect(dot.querySelector('.sr-only')).toHaveTextContent('webapp-7f9c/istio-proxy');
  });

  // An unpositioned scroller does not clip an absolute span anchored past it, so the
  // names would stretch the window.
  it('anchors the screen-reader names inside the dot', () => {
    render(<Row />);
    expect(cellOf('Dot')!.querySelector('.sr-only')!.parentElement).toHaveClass('relative');
  });

  it('spells out what cluster text could hide', () => {
    render(<Row of={line({ message: 'ok\u202Eevil' })} />);
    expect(cellOf('Message')!.querySelector('mark')).not.toBeNull();
  });

  it('draws markup in a line as its text', () => {
    const markup = '<img src=x onerror="alert(1)"><b>bold</b>';
    render(<Row columns={ALL} of={line({ message: markup, stream: { ...line().stream, pod: '<i>pod</i>' } })} />);
    expect(row().querySelector('img, b, i')).toBeNull();
    expect(cellOf('Message')).toHaveTextContent(markup);
    expect(cellOf('Pod')).toHaveTextContent('<i>pod</i>');
  });

  it('draws a message with its colours', () => {
    render(<Row of={line({ message: '\x1b[31merror\x1b[0m: disk' })} />);
    expect(cellOf('Message')).toHaveTextContent('error: disk');
    expect(cellOf('Message')!.querySelector<HTMLElement>('span[style]')?.style.color).toBe('var(--ansi-red, #cc0000)');
  });

  it('puts the date before a time from another day', () => {
    render(<Row of={line({ timestamp: local(2026, 9, 14, 23, 59, 59, 5) })} />);
    expect(cellOf('Timestamp')).toHaveTextContent('Oct 14 23:59:59.005');
  });

  it('draws the milliseconds of a time with nine fraction digits', () => {
    const nine = line().timestamp.replace(/Z$/, '456789Z');
    render(<Row of={line({ timestamp: nine })} />);
    expect(cellOf('Timestamp')).toHaveTextContent('09:05:03.123');
  });

  it('wraps the message and drops the fixed height when asked', () => {
    const { rerender } = render(<Row />);
    expect(cellOf('Message')).not.toHaveClass('whitespace-pre-wrap');
    rerender(<Row wrap />);
    expect(cellOf('Message')).toHaveClass('whitespace-pre-wrap');
    expect(row()).not.toHaveClass(LOG_ROW_H);
  });

  it('draws a hole before the line as a notice over its cells, naming the stream and the hole', () => {
    const gap = { after: local(2026, 9, 15, 9, 0, 0, 0), before: local(2026, 9, 15, 9, 5, 3, 123) };
    render(<Row of={line({ gap })} />);
    expect(screen.getByTestId('log-gap')).toHaveTextContent(
      'Lines may be missing from prod/webapp-7f9c/istio-proxy between 09:00:00.000 and 09:05:03.123: the cluster no longer holds them.',
    );
    expect(screen.getByTestId('log-gap')).not.toHaveAttribute('role');
    expect(cols()).toEqual(['Pos', 'Timestamp', 'Dot', 'Message']);
    expect(row()).toHaveClass(LOG_GAP_ROW_H);
  });

  it("names a previous instance's hole as the previous instance's", () => {
    const gap = { after: local(2026, 9, 15, 9, 0, 0, 0), before: local(2026, 9, 15, 9, 5, 3, 123) };
    const stream = { namespace: 'prod', pod: 'webapp-7f9c', container: 'istio-proxy', previous: true };
    render(<Row of={line({ gap, stream })} />);
    expect(screen.getByTestId('log-gap')).toHaveTextContent(
      'Lines may be missing from prod/webapp-7f9c/istio-proxy (previous) between',
    );
  });

  it('ends a cut line’s message with what the server cut', () => {
    render(<Row of={line({ truncatedBytes: 18_432 })} />);
    expect(cellOf('Message')).toHaveTextContent('GET /healthz 200 … 18 KiB cut');
    expect(screen.getByTitle('Cut by the server')).toBeInTheDocument();
  });

  it('draws no cut on a whole line', () => {
    render(<Row />);
    expect(screen.queryByTitle('Cut by the server')).toBeNull();
  });

  it('draws no notice for a line with no hole', () => {
    render(<Row />);
    expect(screen.queryByTestId('log-gap')).toBeNull();
    expect(row()).toHaveClass(LOG_ROW_H);
  });

  it("spells out what cluster text could hide in a hole's notice", () => {
    const gap = { after: local(2026, 9, 15, 9, 0, 0, 0), before: local(2026, 9, 15, 9, 5, 3, 123) };
    render(<Row of={line({ gap, stream: { ...line().stream, pod: 'web\u202Eevil' } })} />);
    expect(screen.getByTestId('log-gap').querySelector('mark')).not.toBeNull();
  });

  it('re-renders only when what it draws changes', () => {
    // The row reads the message on every render, so a getter counts its renders.
    let reads = 0;
    const counted = {
      ...line(),
      get message() {
        reads += 1;
        return 'GET /healthz 200';
      },
    };
    const controls = { setTop: (_top: number) => {}, bump: () => {} };
    function Parent() {
      const [top, set] = useState(0);
      const [, tick] = useState(0);
      useEffect(() => {
        controls.setTop = set;
        controls.bump = () => tick((n) => n + 1);
      }, []);
      return (
        <LogViewerRow
          line={counted}
          rowKey={1}
          columns={DEFAULT_COLUMNS}
          gridTemplate={gridTemplate(DEFAULT_COLUMNS)}
          widths={widths}
          rowWidth={0}
          wrap={false}
          top={top}
        />
      );
    }
    render(<Parent />);
    const mounted = reads;
    expect(mounted).toBeGreaterThan(0);

    act(() => controls.bump());
    expect(reads).toBe(mounted);

    // A move places the row again and reads no cell.
    act(() => controls.setTop(20));
    expect(row()).toHaveStyle({ transform: 'translateY(20px)' });
    expect(reads).toBe(mounted);
  });
});

describe('LogViewerRow selection', () => {
  it('carries its key and whether it is selected', () => {
    const { rerender } = render(<Row rowKey={-4} />);
    expect(row().dataset.rowKey).toBe('-4');
    expect(row()).toHaveAttribute('aria-selected', 'false');
    rerender(<Row rowKey={-4} selected />);
    expect(row()).toHaveAttribute('aria-selected', 'true');
  });

  it("draws a line at a run's top and bottom edges", () => {
    const { rerender } = render(<Row selected selectionTop />);
    expect(row().style.boxShadow).toBe('inset 0 1px 0 0 var(--ring)');
    rerender(<Row selected selectionBottom />);
    expect(row().style.boxShadow).toBe('inset 0 -1px 0 0 var(--ring)');
    rerender(<Row selected selectionTop selectionBottom />);
    expect(row().style.boxShadow).toBe('inset 0 1px 0 0 var(--ring), inset 0 -1px 0 0 var(--ring)');
    rerender(<Row selected />);
    expect(row().style.boxShadow).toBe('');
  });

  it('makes the position cell a button that selects its row by mouse, Enter or Space', () => {
    const onRowMouseDown = vi.fn();
    render(<Row rowKey={2} onRowMouseDown={onRowMouseDown} />);
    const pos = cellOf('Pos')!;
    expect(pos).toHaveAttribute('role', 'button');
    expect(pos).toHaveAttribute('tabindex', '0');
    fireEvent.mouseDown(pos);
    fireEvent.keyDown(pos, { key: 'Enter' });
    fireEvent.keyDown(pos, { key: ' ' });
    expect(onRowMouseDown).toHaveBeenCalledTimes(3);
    expect(onRowMouseDown.mock.calls.map((c) => c[0])).toEqual([2, 2, 2]);
  });

  it("keeps a Space that selects from the scroller's own key listener", () => {
    const outer = vi.fn();
    const { container } = render(<Row onRowMouseDown={vi.fn()} onCellMouseDown={vi.fn()} />);
    container.addEventListener('keydown', outer);
    fireEvent.keyDown(cellOf('Pos')!, { key: ' ' });
    fireEvent.keyDown(cellOf('Message')!, { key: ' ' });
    expect(outer).not.toHaveBeenCalled();
    fireEvent.keyDown(cellOf('Message')!, { key: 'ArrowDown' });
    expect(outer).toHaveBeenCalledTimes(1);
  });

  it('makes every cell but the dot a focusable gridcell that selects by mouse or Enter', () => {
    const onCellMouseDown = vi.fn();
    render(<Row rowKey={3} columns={['Pod', 'Dot', 'Message']} onCellMouseDown={onCellMouseDown} />);
    expect(cellOf('Pod')).toHaveAttribute('role', 'gridcell');
    expect(cellOf('Pod')).toHaveAttribute('tabindex', '0');
    expect(cellOf('Dot')).not.toHaveAttribute('role');
    expect(cellOf('Dot')).not.toHaveAttribute('tabindex');
    fireEvent.mouseDown(cellOf('Pod')!);
    fireEvent.keyDown(cellOf('Message')!, { key: 'Enter' });
    fireEvent.mouseDown(cellOf('Dot')!);
    expect(onCellMouseDown.mock.calls.map((c) => [c[0], c[1]])).toEqual([
      [3, 'Pod'],
      [3, 'Message'],
    ]);
  });

  it('draws the overlay over its selected cells, with the anchor marked', () => {
    const { rerender } = render(<Row />);
    expect(row().querySelector('[data-overlay-col]')).toBeNull();
    rerender(<Row selectedCellCols={new Set(['Message'])} anchorCol="Message" />);
    expect(row().querySelector('[data-overlay-col="Message"]')).toHaveAttribute('data-overlay-anchor', 'true');
  });

  it('offers to copy a cell on right-click', async () => {
    // The menu opens on its own timers.
    vi.useRealTimers();
    render(<Row />);
    fireEvent.contextMenu(cellOf('Message')!);
    await waitFor(() => expect(screen.getByRole('menu')).toBeInTheDocument());
    expect(screen.getByText('Copy message')).toBeInTheDocument();
  });
});

describe('estimateRowHeight', () => {
  const gap = { after: local(2026, 9, 15, 9, 0, 0, 0), before: local(2026, 9, 15, 9, 5, 3, 123) };

  it('is one row with wrap off, and one more for a hole', () => {
    expect(estimateRowHeight(line({ message: 'x'.repeat(500) }))).toBe(LOG_ROW_PX);
    expect(estimateRowHeight(line({ gap }))).toBe(2 * LOG_ROW_PX);
  });

  it('is the wrapped message with wrap on, and one more for a hole', () => {
    const long = line({ message: 'x'.repeat(100) });
    const width = 100 * 7.2;
    expect(estimateRowHeight(long, width)).toBe(LOG_ROW_PX);
    expect(estimateRowHeight(long, width / 3)).toBe(3 * LOG_ROW_PX);
    expect(estimateRowHeight({ ...long, gap }, width / 3)).toBe(4 * LOG_ROW_PX);
  });
});

describe('CutNotice', () => {
  const text = (bytes: number) => render(<CutNotice bytes={bytes} />).container.textContent;

  it('says the cut in KiB to one decimal, and in bytes under one KiB', () => {
    expect(text(18_432)).toBe(' … 18 KiB cut');
    expect(text(1536)).toBe(' … 1.5 KiB cut');
    expect(text(1000)).toBe(' … 1000 B cut');
  });

  it('draws nothing for a whole line', () => {
    expect(text(0)).toBe('');
  });
});
