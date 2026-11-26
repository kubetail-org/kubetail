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

import { type PointerEvent, type ReactNode, useState } from 'react';

import { sampleFeedSlots } from '@/lib/graphql/sample-feed';
import type { SampleFeedFrame } from '@/lib/graphql/sample-feed';

// The viewBox. The SVG stretches it to the row, and the strokes keep their width.
export const SPARK_W = 120;
export const SPARK_H = 24;

type Rate = { receivedBytesPerSecond: number; sentBytesPerSecond: number };

const LINE = {
  fill: 'none',
  strokeWidth: 2,
  strokeLinejoin: 'round',
  strokeLinecap: 'round',
  vectorEffect: 'non-scaling-stroke',
} as const;

// The hovered sample: a guide down the chart and a dot on its line. The dot is a zero-length
// stroke with round caps, since a circle would stretch with the viewBox.
function Marker({ x, y }: { x: number; y: number }) {
  return (
    <>
      <line
        x1={x}
        x2={x}
        y1={0}
        y2={SPARK_H}
        stroke="var(--muted-foreground)"
        strokeWidth={1}
        vectorEffect="non-scaling-stroke"
      />
      <line
        data-testid="sparkline-dot"
        x1={x}
        x2={x}
        y1={y}
        y2={y}
        stroke="var(--series-received)"
        strokeWidth={6}
        strokeLinecap="round"
        vectorEffect="non-scaling-stroke"
      />
    </>
  );
}

// A connection's recent data rate: received and sent as two lines on one scale, one point per
// slot, the newest at the right edge. A sample after a sleep is just the next slot; nothing
// here reads a time. Nothing is drawn for an empty series. aria-hidden, since the text row
// beside it says the numbers.
//
// `receivedOnly` draws the received rate alone, scaled to itself, over a faint fill to the
// bottom: for a row that names the download rate and nothing else.
//
// `scale="sqrt"` draws each value's square root, so a burst does not flatten the quieter
// samples beside it into the baseline. Zero stays on the baseline and the peak at the top.
//
// With `tooltip`, the pointer picks the sample under it by its slot, marks it, and draws
// `tooltip(sample)` in a small box centered above the chart, which stays put as the pointer
// moves.
export function DataRateSparkline<S extends Rate>({
  series,
  receivedOnly = false,
  scale = 'linear',
  tooltip,
  className = 'ml-2 inline-block h-4 w-24 align-middle',
}: {
  series: SampleFeedFrame<S>;
  receivedOnly?: boolean;
  scale?: 'linear' | 'sqrt';
  tooltip?: (sample: S) => ReactNode;
  className?: string;
}) {
  // The slot under the pointer, not a sample index: a growing series moves its samples left
  // under a still pointer, and the tooltip should follow the chart, not the sample.
  const [hoveredSlot, setHoveredSlot] = useState<number | null>(null);
  const { samples } = series;
  if (samples.length === 0) return null;

  const received = (s: Rate) => s.receivedBytesPerSecond;
  const sent = (s: Rate) => s.sentBytesPerSecond;
  const slots = sampleFeedSlots(series);
  const max = Math.max(0, ...samples.map(received), ...(receivedOnly ? [] : samples.map(sent)));
  const share = (v: number) => (scale === 'sqrt' ? Math.sqrt(v / max) : v / max);
  // Inset by a unit so a stroke on the baseline or the peak is not cut in half.
  const y = (v: number) => (max === 0 ? SPARK_H - 1 : SPARK_H - 1 - share(v) * (SPARK_H - 2));
  const x = (i: number) => ((slots - samples.length + i) / Math.max(slots - 1, 1)) * SPARK_W;
  const points = (value: (s: Rate) => number) => samples.map((s, i) => `${x(i)},${y(value(s))}`).join(' ');

  const pick = (e: PointerEvent<SVGSVGElement>) => {
    const rect = e.currentTarget.getBoundingClientRect();
    setHoveredSlot(Math.round(((e.clientX - rect.left) / rect.width) * (slots - 1)));
  };
  // An empty slot before the oldest sample shows the oldest.
  const hovered =
    hoveredSlot === null ? null : Math.min(Math.max(hoveredSlot - (slots - samples.length), 0), samples.length - 1);
  const mark = hovered === null ? undefined : samples[hovered];

  const svg = (
    <svg
      aria-hidden="true"
      viewBox={`0 0 ${SPARK_W} ${SPARK_H}`}
      preserveAspectRatio="none"
      className={`overflow-visible ${className}`}
      onPointerMove={tooltip && pick}
      onPointerLeave={tooltip && (() => setHoveredSlot(null))}
    >
      {receivedOnly && (
        <polygon
          points={`${points(received)} ${SPARK_W},${SPARK_H} ${x(0)},${SPARK_H}`}
          fill="var(--series-received)"
          fillOpacity={0.15}
        />
      )}
      <polyline points={points(received)} stroke="var(--series-received)" {...LINE} />
      {!receivedOnly && <polyline points={points(sent)} stroke="var(--series-sent)" {...LINE} />}
      {hovered !== null && mark && <Marker x={x(hovered)} y={y(received(mark))} />}
    </svg>
  );
  if (!tooltip) return svg;

  return (
    <div className="relative">
      {svg}
      {mark && (
        <div
          role="tooltip"
          className="pointer-events-none absolute bottom-full left-1/2 mb-1 -translate-x-1/2 rounded-md border bg-popover px-1.5 py-0.5 text-xs whitespace-nowrap text-popover-foreground tabular-nums shadow-sm"
        >
          {tooltip(mark)}
        </div>
      )}
    </div>
  );
}
