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

import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';

import { DataRateSparkline, SPARK_H, SPARK_W } from './data-rate-sparkline';

const sample = (received: number, sent: number) => ({ receivedBytesPerSecond: received, sentBytesPerSecond: sent });
// A series of `slots` slots holding samples; the sidecar's interval is 2s.
const series = (slots: number, ...samples: ReturnType<typeof sample>[]) => ({
  intervalSeconds: 2,
  windowSeconds: 2 * slots,
  samples,
});

// Each line's points as [x, y] pairs: received first, then sent.
function linesOf(container: HTMLElement): [number, number][][] {
  return [...container.querySelectorAll('polyline')].map((line) =>
    line
      .getAttribute('points')!
      .split(' ')
      .map((p) => p.split(',').map(Number) as [number, number]),
  );
}

describe('DataRateSparkline', () => {
  afterEach(cleanup);

  it('draws nothing for an empty series', () => {
    const { container } = render(<DataRateSparkline series={series(60)} />);

    expect(container.querySelector('svg')).toBeNull();
  });

  it('draws a line for each direction, hidden from assistive tech', () => {
    const { container } = render(<DataRateSparkline series={series(60, sample(1, 2), sample(3, 4))} />);

    expect(container.querySelector('svg')).toHaveAttribute('aria-hidden', 'true');
    expect(linesOf(container)).toHaveLength(2);
  });

  // The newest sample sits at the right edge; a short history leaves the left empty.
  it('places the newest sample at the right edge, one slot per sample', () => {
    const { container } = render(<DataRateSparkline series={series(5, sample(0, 0), sample(0, 0))} />);

    const [received] = linesOf(container);
    expect(received.map(([x]) => x)).toEqual([(3 / 4) * SPARK_W, SPARK_W]);
  });

  // One scale for both lines, so they read against each other.
  it('draws both lines on one scale', () => {
    const { container } = render(<DataRateSparkline series={series(60, sample(100, 50))} />);

    const [received, sent] = linesOf(container);
    const top = 1;
    const bottom = SPARK_H - 1;
    expect(received[0][1]).toBe(top);
    expect(sent[0][1]).toBe((top + bottom) / 2);
  });

  // The download row's chart: the received line alone, on its own scale, over a fill to the bottom.
  it('draws the received rate alone, scaled to itself, over a fill', () => {
    const { container } = render(
      <DataRateSparkline series={series(5, sample(50, 500), sample(100, 0))} receivedOnly />,
    );

    const lines = linesOf(container);
    expect(lines).toHaveLength(1);
    expect(lines[0].map(([, y]) => y)).toEqual([(1 + (SPARK_H - 1)) / 2, 1]);
    expect(container.querySelector('polygon')!.getAttribute('points')).toBe(
      `${(3 / 4) * SPARK_W},${SPARK_H / 2} ${SPARK_W},1 ${SPARK_W},${SPARK_H} ${(3 / 4) * SPARK_W},${SPARK_H}`,
    );
  });

  // 25 of 100 is a quarter of the way up linearly, half of it on a square-root scale.
  it('draws the square root of each value on a sqrt scale, zero and the peak unmoved', () => {
    const { container } = render(
      <DataRateSparkline series={series(3, sample(0, 0), sample(25, 0), sample(100, 0))} receivedOnly scale="sqrt" />,
    );

    const top = 1;
    const bottom = SPARK_H - 1;
    expect(linesOf(container)[0].map(([, y]) => y)).toEqual([bottom, (top + bottom) / 2, top]);
  });

  it('draws no fill under the two lines', () => {
    const { container } = render(<DataRateSparkline series={series(60, sample(1, 2))} />);
    expect(container.querySelector('polygon')).toBeNull();
  });

  it('draws an idle connection as two flat lines on the baseline', () => {
    const { container } = render(<DataRateSparkline series={series(60, sample(0, 0), sample(0, 0))} />);

    expect(linesOf(container).map((line) => line.map(([, y]) => y))).toEqual([
      [SPARK_H - 1, SPARK_H - 1],
      [SPARK_H - 1, SPARK_H - 1],
    ]);
  });

  // jsdom lays nothing out, so the SVG is given a 100px box at the left edge.
  function hoverable() {
    const view = render(
      <DataRateSparkline
        series={series(5, sample(10, 0), sample(20, 0), sample(40, 0))}
        receivedOnly
        tooltip={(s) => `${s.receivedBytesPerSecond} B/s`}
      />,
    );
    const svg = view.container.querySelector('svg')!;
    svg.getBoundingClientRect = () => ({ left: 0, width: 100 }) as DOMRect;
    return { svg, ...view };
  }

  // Five slots across 100px, three samples in the right three: slot 2 is sample 0.
  it('shows the sample under the pointer in a tooltip, and none on leave', () => {
    const { svg } = hoverable();
    expect(screen.queryByRole('tooltip')).toBeNull();

    fireEvent.pointerMove(svg, { clientX: 74 });
    expect(screen.getByRole('tooltip')).toHaveTextContent('20 B/s');
    fireEvent.pointerMove(svg, { clientX: 100 });
    expect(screen.getByRole('tooltip')).toHaveTextContent('40 B/s');
    fireEvent.pointerLeave(svg);
    expect(screen.queryByRole('tooltip')).toBeNull();
  });

  it('shows the oldest sample over the empty slots before it', () => {
    const { svg } = hoverable();
    fireEvent.pointerMove(svg, { clientX: 0 });
    expect(screen.getByRole('tooltip')).toHaveTextContent('10 B/s');
  });

  // jsdom applies no Tailwind, so the centering is read off the classes.
  it('centers the tooltip over the chart wherever the pointer is', () => {
    const { svg } = hoverable();
    fireEvent.pointerMove(svg, { clientX: 74 });
    expect(screen.getByRole('tooltip')).toHaveClass('left-1/2', '-translate-x-1/2');
    fireEvent.pointerMove(svg, { clientX: 0 });
    expect(screen.getByRole('tooltip')).toHaveClass('left-1/2', '-translate-x-1/2');
  });

  it('marks the hovered sample with a guide and a dot on its line', () => {
    const { svg, container } = hoverable();
    expect(container.querySelector('line')).toBeNull();

    fireEvent.pointerMove(svg, { clientX: 74 });
    const dot = container.querySelector('[data-testid="sparkline-dot"]')!;
    expect(dot).toHaveAttribute('x1', String((3 / 4) * SPARK_W));
    expect(dot).toHaveAttribute('y1', String(SPARK_H / 2));
    expect(container.querySelectorAll('line')).toHaveLength(2);
  });

  it('follows a growing series under a still pointer', () => {
    const { svg, container, rerender } = hoverable();
    fireEvent.pointerMove(svg, { clientX: 100 });
    expect(screen.getByRole('tooltip')).toHaveTextContent('40 B/s');

    rerender(
      <DataRateSparkline
        series={series(5, sample(10, 0), sample(20, 0), sample(40, 0), sample(80, 0))}
        receivedOnly
        tooltip={(s) => `${s.receivedBytesPerSecond} B/s`}
      />,
    );
    expect(screen.getByRole('tooltip')).toHaveTextContent('80 B/s');
    expect(container.querySelector('[data-testid="sparkline-dot"]')).toHaveAttribute('x1', String(SPARK_W));
  });

  it('takes no hover without a tooltip', () => {
    const { container } = render(<DataRateSparkline series={series(5, sample(10, 0))} />);
    fireEvent.pointerMove(container.querySelector('svg')!, { clientX: 0 });
    expect(container.querySelector('line')).toBeNull();
  });
});
