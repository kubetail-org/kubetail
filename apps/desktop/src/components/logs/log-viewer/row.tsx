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

// One log line as a row: its position from where the view landed, then the columns the user
// picked, as a grid. Every cell but the position and the dot is cluster text, so each goes
// through `VisibleText`, the message with its colours through `AnsiText`. It is memoized over
// primitives, the line's own object, the column state's and its share of the selection, so a
// frame that adds a line draws the new row alone, and a prepend that moves every row
// re-renders them without laying them out again.
import { cn } from '@kubetail/ui/lib/utils';
import { stripAnsi } from 'fancy-ansi';
import { type KeyboardEvent, type MouseEvent, memo, useMemo } from 'react';

import { AnsiText } from '@/components/logs/ansi-text';
import { VisibleText } from '@/components/primitives/visible-text';
import { lineTime } from '@/lib/log-viewer';
import type { LogLine } from '@/lib/log-viewer';

import { type Column, dotColor } from './columns';
import { CellContextMenu } from './context-menu';
import { getPlainAttribute, streamName } from './selection';
import { SelectionOverlay } from './selection-overlay';
import { POS_MIN_PX, type WidthKey } from './widths';

/** The class every row and edge row carries: kubetail's row height, its line height with it. */
export const LOG_ROW_H = 'h-6';
/** `LOG_ROW_H` in pixels, read off a rendered row: jsdom has no layout to check it against. */
export const LOG_ROW_PX = 24;
/** A row whose line carries a hole: its notice's line, then the line's. */
export const LOG_GAP_ROW_H = 'h-12';

/** The advance of one `text-xs` mono character, for the wrap estimate; the measure corrects it. */
const CHAR_PX = 7.2;

/**
 * A row's height before it is measured: its message's lines, wrapped across `messageWidth` when
 * wrap is on, and one more for a hole's notice. Rows are measured only with wrap on, so with it
 * off this is the height.
 */
export function estimateRowHeight(line: LogLine, messageWidth?: number): number {
  const lines = messageWidth ? Math.max(1, Math.ceil((stripAnsi(line.message).length * CHAR_PX) / messageWidth)) : 1;
  return (lines + (line.gap ? 1 : 0)) * LOG_ROW_PX;
}

/**
 * The grid every row and the header share: the position, a track per column, the message
 * taking the rest. The viewer builds it once per column set and hands every row the string.
 */
export const gridTemplate = (columns: Column[]) =>
  [`minmax(${POS_MIN_PX}px, auto)`, ...columns.map((col) => (col === 'Message' ? '1fr' : 'auto'))].join(' ');

/**
 * A cell's measured width as its `minWidth`. Rows are positioned apart, each its own grid, so
 * this is what lines their tracks up.
 */
export function cellStyle(widths: Map<WidthKey, number>, key: WidthKey) {
  const px = widths.get(key);
  return px ? { minWidth: px } : undefined;
}

// A container's colour, drawn inline so no CSS name is built from cluster text. The dot is
// the default set's only word on the stream, so a screen reader hears the names.
// `relative` anchors the `sr-only` names here: a transcript card draws the dot in a
// scroller that is not positioned, and an absolute span anchored past it escapes its clip.
export function Dot({ line }: { line: LogLine }) {
  const { namespace, pod, container } = line.stream;
  return (
    <span className="relative inline-flex items-center">
      <span
        aria-hidden
        className="inline-block size-2 rounded-full"
        style={{ backgroundColor: dotColor(namespace, pod, container) }}
      />
      <span className="sr-only">
        <VisibleText text={`${pod}/${container}`} />
      </span>
    </span>
  );
}

// The hole before a line, in a grid row of its own over every column past the position. It
// is no cell: selection passes over it, and a row's copy writes it (`gapNotice`). Outside a
// grid, its column is ignored.
export function GapNotice({ line, gap }: { line: LogLine; gap: NonNullable<LogLine['gap']> }) {
  return (
    <div
      data-testid="log-gap"
      className="cursor-default truncate px-2 text-muted-foreground italic select-none"
      style={{ gridColumn: '2 / -1' }}
    >
      Lines may be missing from <VisibleText text={streamName(line.stream)} /> between {lineTime(gap.after)} and{' '}
      {lineTime(gap.before)}: the cluster no longer holds them.
    </div>
  );
}

// What the server cut off a line's message, at its end. Nothing opens the whole line.
export function CutNotice({ bytes }: { bytes: number }) {
  if (bytes === 0) return null;
  const size = bytes < 1024 ? `${bytes} B` : `${Number((bytes / 1024).toFixed(1))} KiB`;
  return (
    <span className="text-muted-foreground" title="Cut by the server">
      {` … ${size} cut`}
    </span>
  );
}

// A run of selected rows is outlined at its first row's top and its last row's bottom.
function selectionBoxShadow(isTop: boolean, isBottom: boolean): string | undefined {
  if (isTop && isBottom) return 'inset 0 1px 0 0 var(--ring), inset 0 -1px 0 0 var(--ring)';
  if (isTop) return 'inset 0 1px 0 0 var(--ring)';
  if (isBottom) return 'inset 0 -1px 0 0 var(--ring)';
  return undefined;
}

type Press = MouseEvent | KeyboardEvent;

// Enter and Space act as the mouse down. They are taken in the capture phase, which React
// runs before the scroller's own key listener, so the scroller never reads a Space that
// selected as a scroll down — at the bottom, that would pin.
const pressOnKey = (press: (event: KeyboardEvent) => void) => (event: KeyboardEvent) => {
  if (event.key !== 'Enter' && event.key !== ' ') return;
  event.preventDefault();
  event.stopPropagation();
  press(event);
};

export type LogViewerRowProps = {
  line: LogLine;
  /** The core's key: 0 where the view landed, counting down before it and up after. */
  rowKey: number;
  columns: Column[];
  /** `gridTemplate(columns)`, built once by the caller. */
  gridTemplate: string;
  /** Each column's measured width, which every cell carries as `minWidth` so rows line up. */
  widths: Map<WidthKey, number>;
  /** The widest row measured: every row's `minWidth` while wrap is off. */
  rowWidth: number;
  wrap: boolean;
  /** Where the row sits under a scroller, from the top of its sizer. A row with none is in the flow. */
  top?: number;
  /** The core's index, for its `measureElement`. */
  index?: number;
  measureCell?: (el: HTMLElement | null) => void;
  measureRow?: (el: HTMLElement | null) => void;
  measureElement?: (el: Element | null) => void;
  selected?: boolean;
  /** The first row of a run of selected rows. */
  selectionTop?: boolean;
  /** The last row of a run of selected rows. */
  selectionBottom?: boolean;
  /** The columns selected in this row, and in the rows above and below it, for the overlay's edges. */
  selectedCellCols?: Set<Column>;
  selectedCellColsAbove?: Set<Column>;
  selectedCellColsBelow?: Set<Column>;
  /** The anchor's column when it is in this row and more than one cell is selected. */
  anchorCol?: Column;
  onRowMouseDown?: (key: number, event: Press) => void;
  onCellMouseDown?: (key: number, col: Column, event: Press) => void;
};

function cellContent(line: LogLine, col: Column) {
  if (col === 'Dot') return <Dot line={line} />;
  if (col === 'Message') {
    return (
      <>
        <AnsiText text={line.message} />
        <CutNotice bytes={line.truncatedBytes} />
      </>
    );
  }
  return <VisibleText text={getPlainAttribute(line, col)} />;
}

export const LogViewerRow = memo(
  ({
    line,
    rowKey,
    columns,
    gridTemplate: template,
    widths,
    rowWidth,
    wrap,
    top,
    index,
    measureCell,
    measureRow,
    measureElement,
    selected = false,
    selectionTop = false,
    selectionBottom = false,
    selectedCellCols,
    selectedCellColsAbove,
    selectedCellColsBelow,
    anchorCol,
    onRowMouseDown,
    onCellMouseDown,
  }: LogViewerRowProps) => {
    const landed = rowKey === 0;

    // A prepend moves every row, and a move must not redraw a row's cells.
    const cells = useMemo(
      () =>
        columns.map((col) => {
          // A wrapped message takes what is left of the row and as many lines as it needs, so
          // neither its width nor the row's is measured. `min-w-0` lets its track shrink below an
          // unbroken token's width, which `break-word` alone does not.
          const wrapped = col === 'Message' && wrap;
          // The dot takes no selection and no menu; every other cell is a `gridcell` with both.
          const dot = col === 'Dot';
          // What is measured is the content, so it is an inline-block of its own; the dot's already
          // is, and a wrapped message is not measured.
          const content = cellContent(line, col);
          const cell = (
            // oxlint-disable-next-line jsx-a11y/no-static-element-interactions -- a cell with handlers is a `gridcell`; the dot has neither
            <div
              key={col}
              ref={wrapped ? undefined : measureCell}
              data-col={col}
              role={dot ? undefined : 'gridcell'}
              tabIndex={dot ? undefined : 0}
              onMouseDown={dot ? undefined : (e) => onCellMouseDown?.(rowKey, col, e)}
              onKeyDownCapture={dot ? undefined : pressOnKey((e) => onCellMouseDown?.(rowKey, col, e))}
              className={cn(
                'cursor-default px-2 outline-none select-none',
                wrapped && 'min-w-0 whitespace-pre-wrap wrap-break-word',
                !wrapped && (col === 'Message' ? 'whitespace-pre' : 'whitespace-nowrap text-muted-foreground'),
              )}
              style={wrapped ? undefined : cellStyle(widths, col)}
            >
              {dot || wrapped ? content : <span className="inline-block">{content}</span>}
            </div>
          );
          return dot ? (
            cell
          ) : (
            <CellContextMenu key={col} col={col} line={line}>
              {cell}
            </CellContextMenu>
          );
        }),
      [columns, line, wrap, widths, measureCell, rowKey, onCellMouseDown],
    );

    return (
      <div
        ref={(el) => {
          measureElement?.(el);
          if (!wrap) measureRow?.(el);
        }}
        data-testid="log-row"
        data-index={index}
        data-row-key={rowKey}
        role="row"
        aria-selected={selected}
        style={{
          gridTemplateColumns: template,
          minWidth: wrap ? '100%' : rowWidth || '100%',
          ...(top === undefined ? {} : { transform: `translateY(${top}px)` }),
          boxShadow: selectionBoxShadow(selectionTop, selectionBottom),
        }}
        className={cn(
          // The font and its size are the scroller's, inherited.
          'grid leading-6',
          !wrap && (line.gap ? LOG_GAP_ROW_H : LOG_ROW_H),
          // The overlay is positioned against the row.
          top === undefined ? 'relative' : 'absolute top-0 left-0',
        )}
      >
        {/* The sign is its own span, larger, so `+` and `-` read at a glance beside the digits.
            The landed row's marker sits here, its padding keeping the digits where the other rows' are. */}
        <div
          ref={measureCell}
          data-col="Pos"
          role="button"
          tabIndex={0}
          onMouseDown={(e) => onRowMouseDown?.(rowKey, e)}
          onKeyDownCapture={pressOnKey((e) => onRowMouseDown?.(rowKey, e))}
          className={cn(
            'cursor-default pr-1.5 text-center text-[0.65rem] whitespace-nowrap tabular-nums outline-none select-none',
            landed ? 'border-l-2 border-primary pl-[7px] font-extrabold' : 'text-muted-foreground',
          )}
          style={line.gap ? { ...cellStyle(widths, 'Pos'), gridRow: 'span 2' } : cellStyle(widths, 'Pos')}
        >
          <span className="inline-block">
            {!landed && <span className="text-[0.9rem] text-muted-foreground">{rowKey > 0 ? '+' : '-'}</span>}
            {Math.abs(rowKey)}
          </span>
        </div>
        {line.gap && <GapNotice line={line} gap={line.gap} />}
        {cells}
        {selectedCellCols && (
          <SelectionOverlay
            selectedCols={selectedCellCols}
            selectedColsAbove={selectedCellColsAbove}
            selectedColsBelow={selectedCellColsBelow}
            anchorCol={anchorCol}
            columns={columns}
            widths={widths}
            top={line.gap ? LOG_ROW_PX : 0}
          />
        )}
      </div>
    );
  },
);
