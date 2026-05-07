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

import { memo, useMemo } from 'react';

import { isSelectableViewerColumn } from './selection';
import { ViewerColumn } from './shared';

const BORDER_WIDTH = '2px';
const BORDER_COLOR = 'var(--color-blue-500)';

type Edges = { top: boolean; bottom: boolean; left: boolean; right: boolean };

type SelectionRect = {
  col: ViewerColumn;
  x: number;
  w: number;
  edges: Edges;
};

type Props = {
  selectedCols: Set<ViewerColumn>;
  selectedColsAbove: Set<ViewerColumn> | undefined;
  selectedColsBelow: Set<ViewerColumn> | undefined;
  // The anchor's column when the anchor is in this row, otherwise undefined.
  // The anchor's rect is drawn with all four borders + a corner-dot indicator.
  anchorCol: ViewerColumn | undefined;
  visibleCols: Set<ViewerColumn>;
  colWidths: Map<ViewerColumn, number>;
  posColWidth: number;
  // Total rendered width of the row. Required for accurate Message-column
  // positioning — Message uses `1fr` in the grid, so its rendered width is
  // `rowWidth − Pos − sum(other cols)`. Pass 0 when not yet measured; the
  // overlay falls back to the Message column's measured content width.
  rowWidth: number;
};

function computeRowSelectionRects(
  selectedCols: Set<ViewerColumn>,
  selectedColsAbove: Set<ViewerColumn> | undefined,
  selectedColsBelow: Set<ViewerColumn> | undefined,
  visibleCols: Set<ViewerColumn>,
  colWidths: Map<ViewerColumn, number>,
  posColWidth: number,
  rowWidth: number,
): SelectionRect[] {
  const cols = [...visibleCols];
  const selectableCols = cols.filter(isSelectableViewerColumn);

  // Message uses `1fr` in the row's grid template, so its rendered width is
  // `rowWidth − Pos − sum(other cols)` rather than its measured colWidth.
  const measuredMessage = colWidths.get(ViewerColumn.Message) ?? 0;
  let widthOfNonMessageCols = posColWidth;
  cols.forEach((c) => {
    if (c !== ViewerColumn.Message) widthOfNonMessageCols += colWidths.get(c) ?? 0;
  });
  const stretchedMessage =
    visibleCols.has(ViewerColumn.Message) && rowWidth > 0
      ? Math.max(measuredMessage, rowWidth - widthOfNonMessageCols)
      : measuredMessage;

  const widthOf = (col: ViewerColumn) => (col === ViewerColumn.Message ? stretchedMessage : (colWidths.get(col) ?? 0));

  // x-offset per visible column, including the leading Pos column.
  const colX = new Map<ViewerColumn, number>();
  let x = posColWidth;
  cols.forEach((col) => {
    colX.set(col, x);
    x += widthOf(col);
  });

  // For ColorDot: "selected here" means both selectable neighbors are selected
  // in `selSet` (continuous-run rule). For other columns: ordinary membership.
  const isSelectedIn = (selSet: Set<ViewerColumn> | undefined, i: number): boolean => {
    if (!selSet) return false;
    const col = cols[i];
    if (col === ViewerColumn.ColorDot) {
      return i > 0 && i < cols.length - 1 && selSet.has(cols[i - 1]) && selSet.has(cols[i + 1]);
    }
    return selSet.has(col);
  };

  const out: SelectionRect[] = [];
  cols.forEach((col, i) => {
    if (!isSelectedIn(selectedCols, i)) return;

    if (col === ViewerColumn.ColorDot) {
      // ColorDot acts as an interior connector — only top/bottom edges.
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

    // Left/right adjacency hops over ColorDot via the selectable-only list.
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

function rectStyle(x: number, w: number, edges: Edges): React.CSSProperties {
  return {
    position: 'absolute',
    left: `${x}px`,
    top: '0px',
    bottom: '0px',
    width: `${w}px`,
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

// Borders are real CSS borders on empty divs (not inset box-shadows) to avoid
// Firefox's stacked-shadow rendering quirk and to keep the chrome painting as
// a normal layer above the cell content.
export const SelectionOverlay = memo(
  ({
    selectedCols,
    selectedColsAbove,
    selectedColsBelow,
    anchorCol,
    visibleCols,
    colWidths,
    posColWidth,
    rowWidth,
  }: Props) => {
    const rects = useMemo(
      () =>
        computeRowSelectionRects(
          selectedCols,
          selectedColsAbove,
          selectedColsBelow,
          visibleCols,
          colWidths,
          posColWidth,
          rowWidth,
        ),
      [selectedCols, selectedColsAbove, selectedColsBelow, visibleCols, colWidths, posColWidth, rowWidth],
    );

    return (
      <div className="pointer-events-none absolute inset-0">
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
                <span data-overlay-anchor-dot aria-hidden className="absolute right-0 bottom-0 size-1.5 bg-blue-500" />
              )}
            </div>
          );
        })}
      </div>
    );
  },
);
