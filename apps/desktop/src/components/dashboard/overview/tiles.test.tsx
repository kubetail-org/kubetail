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

import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';

import type { ClusterCachedDataEvent } from '@/lib/cluster-cached-data-events';
import { TONE } from '@/lib/cluster-health';

import { Tiles } from './tiles';

// A tile's three lines, by its label.
function tile(label: string): HTMLElement {
  return screen.getByText(label).parentElement!;
}

const pods = (failing: number, pending: number) => ({ running: 3, failing, pending, total: 3 + failing + pending });
const warnings = (n: number) => Array.from({ length: n }, (_, i) => ({ uid: `w${i}` }) as ClusterCachedDataEvent);

describe('Tiles', () => {
  it('draws a spinner in place of every figure while loading', () => {
    render(<Tiles />);
    expect(screen.getAllByText('Loading…')).toHaveLength(4);
  });

  it('reads the nodes ready of all, and how many are not', () => {
    const { rerender } = render(<Tiles nodes={{ ready: 3, total: 3 }} />);
    expect(tile('Nodes ready')).toHaveTextContent('Nodes ready3 / 3All nodes Ready');
    expect(screen.getByText('All nodes Ready')).toHaveClass(TONE.ok.text);

    rerender(<Tiles nodes={{ ready: 2, total: 3 }} />);
    expect(screen.getByText('1 not ready')).toHaveClass(TONE.error.text);

    rerender(<Tiles nodes={{ ready: 0, total: 0 }} />);
    expect(screen.getByText('No nodes')).toHaveClass('text-muted-foreground');
  });

  it('reads the pods running of all, and the failing and pending', () => {
    const { rerender } = render(<Tiles pods={pods(1, 2)} />);
    expect(tile('Pods running')).toHaveTextContent('Pods running3 / 61 failing · 2 pending');
    expect(screen.getByText('1 failing')).toHaveClass(TONE.error.text);
    expect(screen.getByText('2 pending')).toHaveClass(TONE.attention.text);

    rerender(<Tiles pods={pods(1, 0)} />);
    expect(tile('Pods running')).toHaveTextContent(/1 failing$/);

    rerender(<Tiles pods={pods(0, 2)} />);
    expect(tile('Pods running')).toHaveTextContent(/3 \/ 52 pending$/);

    rerender(<Tiles pods={pods(0, 0)} />);
    expect(screen.getByText('All pods running')).toHaveClass(TONE.ok.text);

    // A scope with no pods, or Succeeded ones alone, has nothing running to vouch for.
    rerender(<Tiles pods={{ running: 0, failing: 0, pending: 0, total: 0 }} />);
    expect(tile('Pods running')).toHaveTextContent('Pods running0 / 0No pods');
    expect(screen.getByText('No pods')).toHaveClass('text-muted-foreground');
  });

  it('counts the namespaces and their workloads', () => {
    const row = (name: string, workloads: number) => ({ name, workloads, pods: 0 });
    const { rerender } = render(<Tiles namespaces={[row('a', 2), row('b', 3)]} />);
    expect(tile('Namespaces')).toHaveTextContent('Namespaces25 workloads');

    rerender(<Tiles namespaces={[row('a', 1)]} />);
    expect(tile('Namespaces')).toHaveTextContent('Namespaces11 workload');
  });

  it('draws the warnings red only when there are some', () => {
    const { rerender } = render(<Tiles warnings={warnings(2)} />);
    expect(tile('Warnings')).toHaveTextContent('Warnings2in the last hour');
    expect(screen.getByText('2')).toHaveClass(TONE.error.text);

    rerender(<Tiles warnings={[]} />);
    expect(screen.getByText('0')).not.toHaveClass(TONE.error.text);
  });
});
