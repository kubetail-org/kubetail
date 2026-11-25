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
import { describe, expect, it } from 'vitest';

import type { Column } from './columns';
import { SelectionOverlay } from './selection-overlay';
import type { WidthKey } from './widths';

function rectFor(col: Column) {
  return document.querySelector<HTMLElement>(`[data-overlay-col="${col}"]`);
}

function anchorRect() {
  return document.querySelector<HTMLElement>('[data-overlay-anchor="true"]');
}

const defaults = {
  columns: ['Timestamp', 'Dot', 'Message'] as Column[],
  widths: new Map<WidthKey, number>([
    ['Pos', 48],
    ['Timestamp', 100],
    ['Dot', 20],
    ['Message', 400],
  ]),
  selectedColsAbove: undefined as Set<Column> | undefined,
  selectedColsBelow: undefined as Set<Column> | undefined,
  anchorCol: undefined as Column | undefined,
};

describe('SelectionOverlay', () => {
  it('renders one rect with all 4 borders for an isolated selected cell', () => {
    render(<SelectionOverlay {...defaults} selectedCols={new Set(['Message'])} />);
    const r = rectFor('Message');
    expect(r).not.toBeNull();
    expect(r!.style.borderTopWidth).toBe('1px');
    expect(r!.style.borderBottomWidth).toBe('1px');
    expect(r!.style.borderLeftWidth).toBe('1px');
    expect(r!.style.borderRightWidth).toBe('1px');
  });

  it('horizontally adjacent selected cells share an edge (no inner border)', () => {
    render(<SelectionOverlay {...defaults} selectedCols={new Set(['Timestamp', 'Message'])} />);
    const ts = rectFor('Timestamp');
    const msg = rectFor('Message');
    expect(ts!.style.borderLeftWidth).toBe('1px');
    expect(ts!.style.borderRightWidth).toBe('0px');
    expect(msg!.style.borderLeftWidth).toBe('0px');
    expect(msg!.style.borderRightWidth).toBe('1px');
  });

  it('cell with same column selected above has no top border', () => {
    render(
      <SelectionOverlay {...defaults} selectedCols={new Set(['Message'])} selectedColsAbove={new Set(['Message'])} />,
    );
    const r = rectFor('Message');
    expect(r!.style.borderTopWidth).toBe('0px');
    expect(r!.style.borderBottomWidth).toBe('1px');
  });

  it('cell with same column selected below has no bottom border', () => {
    render(
      <SelectionOverlay {...defaults} selectedCols={new Set(['Message'])} selectedColsBelow={new Set(['Message'])} />,
    );
    const r = rectFor('Message');
    expect(r!.style.borderTopWidth).toBe('1px');
    expect(r!.style.borderBottomWidth).toBe('0px');
  });

  it('ColorDot is included with top/bottom borders when both neighbors selected', () => {
    render(<SelectionOverlay {...defaults} selectedCols={new Set(['Timestamp', 'Message'])} />);
    const dot = rectFor('Dot');
    expect(dot).not.toBeNull();
    expect(dot!.style.borderTopWidth).toBe('1px');
    expect(dot!.style.borderBottomWidth).toBe('1px');
    expect(dot!.style.borderLeftWidth).toBe('0px');
    expect(dot!.style.borderRightWidth).toBe('0px');
  });

  it('ColorDot is not included when only one neighbor is selected', () => {
    render(<SelectionOverlay {...defaults} selectedCols={new Set(['Message'])} />);
    expect(rectFor('Dot')).toBeNull();
  });

  it('ColorDot has no top border when both neighbors selected above (continuous run)', () => {
    render(
      <SelectionOverlay
        {...defaults}
        selectedCols={new Set(['Timestamp', 'Message'])}
        selectedColsAbove={new Set(['Timestamp', 'Message'])}
      />,
    );
    const dot = rectFor('Dot');
    expect(dot!.style.borderTopWidth).toBe('0px');
    expect(dot!.style.borderBottomWidth).toBe('1px');
  });

  it('skips ColorDot when computing left/right adjacency', () => {
    render(<SelectionOverlay {...defaults} selectedCols={new Set(['Timestamp', 'Message'])} />);
    const ts = rectFor('Timestamp');
    expect(ts!.style.borderRightWidth).toBe('0px');
  });

  it('positions rects from the measured widths, the position included', () => {
    render(<SelectionOverlay {...defaults} selectedCols={new Set(['Timestamp'])} />);
    const r = rectFor('Timestamp');
    expect(r!.style.left).toBe('48px');
    expect(r!.style.width).toBe('100px');
  });

  it("runs the message's rect to the row's right edge, height fills row", () => {
    render(<SelectionOverlay {...defaults} selectedCols={new Set(['Message'])} />);
    const r = rectFor('Message');
    // x = Pos (48) + Timestamp (100) + Dot (20) = 168
    expect(r!.style.left).toBe('168px');
    expect(r!.style.right).toBe('0px');
    expect(r!.style.width).toBe('');
    // The rect fills the row's height by stretching top→bottom of its parent.
    expect(r!.style.top).toBe('0px');
    expect(r!.style.bottom).toBe('0px');
  });

  it("starts its rects at top, below a row's notice", () => {
    render(<SelectionOverlay {...defaults} selectedCols={new Set(['Message'])} top={24} />);
    expect(rectFor('Message')!.parentElement!.style.top).toBe('24px');
  });

  it('renders nothing when selectedCols is empty', () => {
    const { container } = render(<SelectionOverlay {...defaults} selectedCols={new Set<Column>()} />);
    expect(container.querySelectorAll('[data-overlay-col]').length).toBe(0);
  });

  it('renders an anchor rect with full 4-side border when anchorCol is set', () => {
    render(<SelectionOverlay {...defaults} selectedCols={new Set(['Message'])} anchorCol="Message" />);
    const a = anchorRect();
    expect(a).not.toBeNull();
    expect(a!.style.borderTopWidth).toBe('1px');
    expect(a!.style.borderBottomWidth).toBe('1px');
    expect(a!.style.borderLeftWidth).toBe('1px');
    expect(a!.style.borderRightWidth).toBe('1px');
  });

  it('anchor keeps all 4 borders even when adjacent to other selected cells', () => {
    render(
      <SelectionOverlay
        {...defaults}
        selectedCols={new Set(['Timestamp', 'Message'])}
        selectedColsBelow={new Set(['Timestamp', 'Message'])}
        anchorCol="Timestamp"
      />,
    );
    const a = anchorRect();
    expect(a).not.toBeNull();
    expect(a!.getAttribute('data-overlay-col')).toBe('Timestamp');
    expect(a!.style.borderTopWidth).toBe('1px');
    expect(a!.style.borderBottomWidth).toBe('1px');
    expect(a!.style.borderLeftWidth).toBe('1px');
    expect(a!.style.borderRightWidth).toBe('1px');
  });

  it('anchor includes a corner-dot indicator', () => {
    render(<SelectionOverlay {...defaults} selectedCols={new Set(['Message'])} anchorCol="Message" />);
    expect(document.querySelector('[data-overlay-anchor-dot]')).not.toBeNull();
  });

  it('does not render anchor rect when anchorCol is undefined', () => {
    render(<SelectionOverlay {...defaults} selectedCols={new Set(['Message'])} />);
    expect(anchorRect()).toBeNull();
  });
});
