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

// The ring around a row's selected cells, ported from kubetail's `SelectionOverlay`: one
// rect per selected cell, with a border on each edge no other selected cell touches.
import { type CSSProperties, memo, useMemo } from 'react';

import type { Column } from './columns';
import { isSelectableColumn } from './selection';
import type { WidthKey } from './widths';

const BORDER_WIDTH = '1px';
const BORDER_COLOR = 'var(--ring)';

type Edges = { top: boolean; bottom: boolean; left: boolean; right: boolean };

type SelectionRect = { col: Column; x: number; w: number | null; edges: Edges };

type Props = {
  selectedCols: Set<Column>;
  selectedColsAbove: Set<Column> | undefined;
  selectedColsBelow: Set<Column> | undefined;
  /** The anchor's column when the anchor is in this row and more than one cell is selected. */
  anchorCol: Column | undefined;
  columns: Column[];
  widths: Map<WidthKey, number>;
  /** Where the cells start in the row, below a hole's notice: 0 with none. */
  top?: number;
};

function computeRowSelectionRects(
  selectedCols: Set<Column>,
  selectedColsAbove: Set<Column> | undefined,
  selectedColsBelow: Set<Column> | undefined,
  columns: Column[],
  widths: Map<WidthKey, number>,
): SelectionRect[] {
  const selectableCols = columns.filter(isSelectableColumn);

  const colX = new Map<Column, number>();
  let x = widths.get('Pos') ?? 0;
  columns.forEach((col) => {
    colX.set(col, x);
    x += widths.get(col) ?? 0;
  });
  // The message is the last column and takes the rest of the row, wrapped or not, so its
  // rect runs to the row's right edge rather than to a measured width.
  const widthOf = (col: Column) => (col === 'Message' ? null : (widths.get(col) ?? 0));

  // The dot counts as selected where both its neighbours are, so a run reads unbroken.
  const isSelectedIn = (selSet: Set<Column> | undefined, i: number): boolean => {
    if (!selSet) return false;
    const col = columns[i];
    if (col === 'Dot') {
      return i > 0 && i < columns.length - 1 && selSet.has(columns[i - 1]) && selSet.has(columns[i + 1]);
    }
    return selSet.has(col);
  };

  const out: SelectionRect[] = [];
  columns.forEach((col, i) => {
    if (!isSelectedIn(selectedCols, i)) return;

    if (col === 'Dot') {
      out.push({
        col,
        x: colX.get(col) ?? 0,
        w: widthOf(col),
        edges: {
          top: !isSelectedIn(selectedColsAbove, i),
          bottom: !isSelectedIn(selectedColsBelow, i),
          left: false,
          right: false,
        },
      });
      return;
    }

    // Left and right adjacency hop over the dot.
    const sIdx = selectableCols.indexOf(col);
    const leftCol = sIdx > 0 ? selectableCols[sIdx - 1] : undefined;
    const rightCol = sIdx < selectableCols.length - 1 ? selectableCols[sIdx + 1] : undefined;

    out.push({
      col,
      x: colX.get(col) ?? 0,
      w: widthOf(col),
      edges: {
        top: !isSelectedIn(selectedColsAbove, i),
        bottom: !isSelectedIn(selectedColsBelow, i),
        left: leftCol === undefined || !selectedCols.has(leftCol),
        right: rightCol === undefined || !selectedCols.has(rightCol),
      },
    });
  });

  return out;
}

function rectStyle(x: number, w: number | null, edges: Edges): CSSProperties {
  return {
    position: 'absolute',
    left: `${x}px`,
    top: '0px',
    bottom: '0px',
    ...(w === null ? { right: '0px' } : { width: `${w}px` }),
    boxSizing: 'border-box',
    borderStyle: 'solid',
    borderColor: BORDER_COLOR,
    borderTopWidth: edges.top ? BORDER_WIDTH : '0px',
    borderBottomWidth: edges.bottom ? BORDER_WIDTH : '0px',
    borderLeftWidth: edges.left ? BORDER_WIDTH : '0px',
    borderRightWidth: edges.right ? BORDER_WIDTH : '0px',
  };
}

const ALL_EDGES: Edges = { top: true, bottom: true, left: true, right: true };

// Real borders on empty divs rather than inset box-shadows, which Firefox stacks badly.
export const SelectionOverlay = memo(
  ({ selectedCols, selectedColsAbove, selectedColsBelow, anchorCol, columns, widths, top = 0 }: Props) => {
    const rects = useMemo(
      () => computeRowSelectionRects(selectedCols, selectedColsAbove, selectedColsBelow, columns, widths),
      [selectedCols, selectedColsAbove, selectedColsBelow, columns, widths],
    );

    return (
      <div className="pointer-events-none absolute inset-x-0 bottom-0" style={{ top }}>
        {rects.map((r) => {
          const isAnchor = r.col === anchorCol;
          return (
            <div
              key={r.col}
              data-overlay-col={r.col}
              {...(isAnchor && { 'data-overlay-anchor': 'true' })}
              style={rectStyle(r.x, r.w, isAnchor ? ALL_EDGES : r.edges)}
            >
              {isAnchor && (
                <span data-overlay-anchor-dot aria-hidden className="absolute right-0 bottom-0 size-1.5 bg-primary" />
              )}
            </div>
          );
        })}
      </div>
    );
  },
);
