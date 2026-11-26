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
import { describe, expect, it } from 'vitest';

import type { LogLine } from '@/lib/log-viewer';
import { EMPTY_LOG_VIEWPORT } from '@/lib/log-viewport';

import { MAX_SELECTED, snapshotOf } from './logs-snapshot';

const line = (i: number): LogLine => ({
  timestamp: `2026-10-17T09:14:${String(i % 60).padStart(2, '0')}.000000001Z`,
  cursor: `c${i}`,
  message: `line ${i}`,
  truncatedBytes: 0,
  stream: { namespace: 'payments', pod: 'webapp-x', container: 'app', previous: i === 1 },
  place: { node: 'n1', region: '', zone: '', os: 'linux', arch: 'arm64' },
  gap: null,
});

describe('snapshotOf', () => {
  it("carries the view's call and the viewport's range, count and flags", () => {
    const viewport = {
      range: { from: line(0).timestamp, to: line(9).timestamp },
      atEnd: true,
      pinnedToEnd: true,
      paused: true,
      visible: 10,
      grep: 'panic',
      selection: [line(0), line(1)],
      selectedCount: 2,
    };
    expect(snapshotOf({ callId: 'call-1' }, viewport)).toEqual({
      callID: 'call-1',
      from: line(0).timestamp,
      to: line(9).timestamp,
      lines: 10,
      atEnd: true,
      pinnedToEnd: true,
      paused: true,
      grep: 'panic',
      selected: [
        {
          timestamp: line(0).timestamp,
          namespace: 'payments',
          pod: 'webapp-x',
          container: 'app',
          previous: false,
          message: 'line 0',
        },
        {
          timestamp: line(1).timestamp,
          namespace: 'payments',
          pod: 'webapp-x',
          container: 'app',
          previous: true,
          message: 'line 1',
        },
      ],
      selectedCount: 2,
    });
  });

  it('sends neither stamp with no line on screen', () => {
    const snapshot = snapshotOf({ callId: 'call-1' }, EMPTY_LOG_VIEWPORT);
    expect(snapshot.from).toBeNull();
    expect(snapshot.to).toBeNull();
    expect(snapshot.grep).toBeNull();
    expect(snapshot.selected).toEqual([]);
    expect(snapshot.selectedCount).toBe(0);
  });

  it('keeps the first fifty selected lines and counts them all', () => {
    const selection = Array.from({ length: 80 }, (_, i) => line(i));
    const snapshot = snapshotOf({ callId: 'call-1' }, { ...EMPTY_LOG_VIEWPORT, selection, selectedCount: 80 });
    expect(snapshot.selected).toHaveLength(MAX_SELECTED);
    expect(snapshot.selected[0].message).toBe('line 0');
    expect(snapshot.selected.at(-1)!.message).toBe('line 49');
    expect(snapshot.selectedCount).toBe(80);
  });

  it("sends the viewport's count, which a window's carried lines can fall short of", () => {
    const snapshot = snapshotOf(
      { callId: 'call-1' },
      { ...EMPTY_LOG_VIEWPORT, selection: [line(0), line(1)], selectedCount: 7 },
    );
    expect(snapshot.selected).toHaveLength(2);
    expect(snapshot.selectedCount).toBe(7);
  });
});
