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

import { render } from '@testing-library/react';

import { SelectionOverlay } from './selection-overlay';
import { ViewerColumn } from './shared';

const POS_COL_WIDTH = 48;

function rectFor(col: ViewerColumn) {
  return document.querySelector<HTMLElement>(`[data-overlay-col="${col}"]`);
}

function anchorRect() {
  return document.querySelector<HTMLElement>('[data-overlay-anchor="true"]');
}

const defaults = {
  visibleCols: new Set([ViewerColumn.Timestamp, ViewerColumn.ColorDot, ViewerColumn.Message]),
  colWidths: new Map<ViewerColumn, number>([
    [ViewerColumn.Timestamp, 100],
    [ViewerColumn.ColorDot, 20],
    [ViewerColumn.Message, 400],
  ]),
  posColWidth: POS_COL_WIDTH,
  rowWidth: 0,
  selectedColsAbove: undefined as Set<ViewerColumn> | undefined,
  selectedColsBelow: undefined as Set<ViewerColumn> | undefined,
  anchorCol: undefined as ViewerColumn | undefined,
};

describe('SelectionOverlay', () => {
  it('renders one rect with all 4 borders for an isolated selected cell', () => {
    render(<SelectionOverlay {...defaults} selectedCols={new Set([ViewerColumn.Message])} />);
    const r = rectFor(ViewerColumn.Message);
    expect(r).not.toBeNull();
    expect(r!.style.borderTopWidth).toBe('2px');
    expect(r!.style.borderBottomWidth).toBe('2px');
    expect(r!.style.borderLeftWidth).toBe('2px');
    expect(r!.style.borderRightWidth).toBe('2px');
  });

  it('horizontally adjacent selected cells share an edge (no inner border)', () => {
    render(<SelectionOverlay {...defaults} selectedCols={new Set([ViewerColumn.Timestamp, ViewerColumn.Message])} />);
    const ts = rectFor(ViewerColumn.Timestamp);
    const msg = rectFor(ViewerColumn.Message);
    expect(ts!.style.borderLeftWidth).toBe('2px');
    expect(ts!.style.borderRightWidth).toBe('0px');
    expect(msg!.style.borderLeftWidth).toBe('0px');
    expect(msg!.style.borderRightWidth).toBe('2px');
  });

  it('cell with same column selected above has no top border', () => {
    render(
      <SelectionOverlay
        {...defaults}
        selectedCols={new Set([ViewerColumn.Message])}
        selectedColsAbove={new Set([ViewerColumn.Message])}
      />,
    );
    const r = rectFor(ViewerColumn.Message);
    expect(r!.style.borderTopWidth).toBe('0px');
    expect(r!.style.borderBottomWidth).toBe('2px');
  });

  it('cell with same column selected below has no bottom border', () => {
    render(
      <SelectionOverlay
        {...defaults}
        selectedCols={new Set([ViewerColumn.Message])}
        selectedColsBelow={new Set([ViewerColumn.Message])}
      />,
    );
    const r = rectFor(ViewerColumn.Message);
    expect(r!.style.borderTopWidth).toBe('2px');
    expect(r!.style.borderBottomWidth).toBe('0px');
  });

  it('ColorDot is included with top/bottom borders when both neighbors selected', () => {
    render(<SelectionOverlay {...defaults} selectedCols={new Set([ViewerColumn.Timestamp, ViewerColumn.Message])} />);
    const dot = rectFor(ViewerColumn.ColorDot);
    expect(dot).not.toBeNull();
    expect(dot!.style.borderTopWidth).toBe('2px');
    expect(dot!.style.borderBottomWidth).toBe('2px');
    expect(dot!.style.borderLeftWidth).toBe('0px');
    expect(dot!.style.borderRightWidth).toBe('0px');
  });

  it('ColorDot is not included when only one neighbor is selected', () => {
    render(<SelectionOverlay {...defaults} selectedCols={new Set([ViewerColumn.Message])} />);
    expect(rectFor(ViewerColumn.ColorDot)).toBeNull();
  });

  it('ColorDot has no top border when both neighbors selected above (continuous run)', () => {
    render(
      <SelectionOverlay
        {...defaults}
        selectedCols={new Set([ViewerColumn.Timestamp, ViewerColumn.Message])}
        selectedColsAbove={new Set([ViewerColumn.Timestamp, ViewerColumn.Message])}
      />,
    );
    const dot = rectFor(ViewerColumn.ColorDot);
    expect(dot!.style.borderTopWidth).toBe('0px');
    expect(dot!.style.borderBottomWidth).toBe('2px');
  });

  it('skips ColorDot when computing left/right adjacency', () => {
    render(<SelectionOverlay {...defaults} selectedCols={new Set([ViewerColumn.Timestamp, ViewerColumn.Message])} />);
    const ts = rectFor(ViewerColumn.Timestamp);
    expect(ts!.style.borderRightWidth).toBe('0px');
  });

  it('positions rects from colWidths and posColWidth, height fills row', () => {
    render(<SelectionOverlay {...defaults} selectedCols={new Set([ViewerColumn.Message])} />);
    const r = rectFor(ViewerColumn.Message);
    // x = posColWidth (48) + Timestamp (100) + ColorDot (20) = 168
    expect(r!.style.left).toBe('168px');
    expect(r!.style.width).toBe('400px');
    // The rect fills the row's height by stretching top→bottom of its parent.
    expect(r!.style.top).toBe('0px');
    expect(r!.style.bottom).toBe('0px');
  });

  it('renders nothing when selectedCols is empty', () => {
    const { container } = render(<SelectionOverlay {...defaults} selectedCols={new Set()} />);
    expect(container.querySelectorAll('[data-overlay-col]').length).toBe(0);
  });

  it('renders an anchor rect with full 4-side border when anchorCol is set', () => {
    render(
      <SelectionOverlay
        {...defaults}
        selectedCols={new Set([ViewerColumn.Message])}
        anchorCol={ViewerColumn.Message}
      />,
    );
    const a = anchorRect();
    expect(a).not.toBeNull();
    expect(a!.style.borderTopWidth).toBe('2px');
    expect(a!.style.borderBottomWidth).toBe('2px');
    expect(a!.style.borderLeftWidth).toBe('2px');
    expect(a!.style.borderRightWidth).toBe('2px');
  });

  it('anchor includes a corner-dot indicator', () => {
    render(
      <SelectionOverlay
        {...defaults}
        selectedCols={new Set([ViewerColumn.Message])}
        anchorCol={ViewerColumn.Message}
      />,
    );
    expect(document.querySelector('[data-overlay-anchor-dot]')).not.toBeNull();
  });

  it('does not render anchor rect when anchorCol is undefined', () => {
    render(<SelectionOverlay {...defaults} selectedCols={new Set([ViewerColumn.Message])} />);
    expect(anchorRect()).toBeNull();
  });
});
