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
import { describe, expect, it, vi } from 'vitest';

import type { LogLine } from '@/lib/log-viewer';
import { createLogViewportHandle, EMPTY_LOG_VIEWPORT } from '@/lib/log-viewport';
import type { LogViewport } from '@/lib/log-viewport';
import { MAX_SELECTED } from '@/lib/logs-snapshot';

import { createMirror, createViewportPublisher, cutSelection, PUSH_LINES_BYTES } from './log-window-push';
import type { ViewportPush } from './log-window-push';

const line = (i: number, message = `line ${i}`): LogLine => ({
  timestamp: `2026-10-17T09:14:${String(i % 60).padStart(2, '0')}.000000001Z`,
  cursor: `c${i}`,
  message,
  truncatedBytes: 0,
  stream: { namespace: 'payments', pod: 'webapp-x', container: 'app', previous: false },
  place: { node: 'n1', region: '', zone: '', os: 'linux', arch: 'arm64' },
  gap: null,
});

const bytes = (v: unknown) => new TextEncoder().encode(JSON.stringify(v)).length;

// The sidecar's MaxLine: 16 KiB and the ellipsis that ends a cut message.
const MAX_LINE_MESSAGE = `${'\x1b[m'.repeat(Math.floor((16 * 1024) / 3))}…`;

describe('cutSelection', () => {
  it('takes the first lines in order, at most MAX_SELECTED, each with three fields', () => {
    const cut = cutSelection(Array.from({ length: 80 }, (_, i) => line(i)));
    expect(cut).toHaveLength(MAX_SELECTED);
    expect(cut[0]).toEqual({ timestamp: line(0).timestamp, stream: line(0).stream, message: 'line 0' });
    expect(cut.at(-1)!.message).toBe(`line ${MAX_SELECTED - 1}`);
  });

  it('stops at the first line past the bound, measured as UTF-8, and cuts no message', () => {
    // 40 KB of UTF-8 each, half that in UTF-16 units: four fit, the fifth would pass 192 KiB.
    const wide = 'é'.repeat(20_000);
    const selection = [...Array.from({ length: 5 }, (_, i) => line(i, wide)), line(5)];
    const cut = cutSelection(selection);
    expect(cut).toHaveLength(4);
    expect(cut.every((l) => l.message === wide)).toBe(true);
    expect(bytes(cut)).toBeLessThanOrEqual(PUSH_LINES_BYTES);
  });

  it('fits the longest message the sidecar hands out, dense with SGR sequences', () => {
    const cut = cutSelection([line(0, MAX_LINE_MESSAGE), line(1, MAX_LINE_MESSAGE)]);
    expect(cut.map((l) => l.message)).toEqual([MAX_LINE_MESSAGE, MAX_LINE_MESSAGE]);
    expect(bytes(cut)).toBeLessThanOrEqual(PUSH_LINES_BYTES);
  });

  it('cuts nothing from an empty selection', () => {
    expect(cutSelection([])).toEqual([]);
  });
});

// A scheduler a test runs by hand: `due()` says whether a run is waiting, `run()` runs it.
function manual() {
  let waiting: (() => void) | null = null;
  return {
    schedule: (run: () => void) => {
      waiting = run;
      return () => {
        if (waiting === run) waiting = null;
      };
    },
    due: () => waiting !== null,
    run: () => {
      const run = waiting;
      waiting = null;
      run?.();
    },
  };
}

// A publisher over a fresh handle, sending into `pushes` and settling each send at once
// unless `send` says otherwise.
function publisher({
  send = () => Promise.resolve(),
  grep = 'error',
}: { send?: (push: ViewportPush) => Promise<unknown>; grep?: string | null } = {}) {
  const viewport = createLogViewportHandle();
  const soon = manual();
  const later = manual();
  const pushes: ViewportPush[] = [];
  const report = vi.fn();
  const p = createViewportPublisher({
    generation: 3,
    grep,
    viewport,
    send: (push) => {
      pushes.push(push);
      return send(push);
    },
    report,
    soon: soon.schedule,
    later: later.schedule,
  });
  const stop = p.start();
  const write = (next: Partial<LogViewport>) => viewport.set({ ...viewport.get(), ...next });
  const select = (lines: LogLine[]) => write({ selection: lines, selectedCount: lines.length });
  return { p, stop, viewport, soon, later, pushes, report, write, select };
}

// Lets a settled send's handlers run.
const flush = () =>
  new Promise<void>((resolve) => {
    setTimeout(resolve, 0);
  });

describe('createViewportPublisher: the push', () => {
  it("carries the answer's generation, the route's grep, the cut, the whole count and a revision", () => {
    const { soon, pushes, write } = publisher();
    const selection = Array.from({ length: 80 }, (_, i) => line(i));
    write({ ...EMPTY_LOG_VIEWPORT, visible: 4, grep: 'panic', selection, selectedCount: 80 });
    soon.run();
    expect(pushes).toEqual([
      {
        generation: 3,
        grep: 'error',
        viewport: {
          ...EMPTY_LOG_VIEWPORT,
          visible: 4,
          grep: 'panic',
          selection: cutSelection(selection),
          selectedCount: 80,
          selectionRevision: 1,
        },
      },
    ]);
  });

  it('steps the revision on a new selection array alone, and reuses the cut while it holds', async () => {
    const { soon, later, pushes, write, select } = publisher();
    const lines = [line(0), line(1)];
    select(lines);
    soon.run();
    await flush();
    write({ visible: 9 });
    later.run();
    await flush();
    select([...lines]);
    soon.run();
    expect(pushes.map((p) => p.viewport.selectionRevision)).toEqual([1, 1, 2]);
    expect(pushes[1].viewport.selection).toBe(pushes[0].viewport.selection);
    expect(pushes[2].viewport.selection).not.toBe(pushes[0].viewport.selection);
  });
});

describe('createViewportPublisher: the pacing', () => {
  it('sends a change to the selection, the count or the grep on the next frame', async () => {
    const { p, soon, later, pushes, select, write } = publisher();
    select([line(0)]);
    expect(soon.due()).toBe(true);
    expect(later.due()).toBe(false);
    soon.run();
    await flush();
    write({ selectedCount: 9 });
    soon.run();
    await flush();
    write({ grep: 'panic' });
    soon.run();
    await flush();
    p.setGrep('warn');
    soon.run();
    expect(pushes.map((x) => [x.grep, x.viewport.grep, x.viewport.selectedCount])).toEqual([
      ['error', null, 1],
      ['error', null, 9],
      ['error', 'panic', 9],
      ['warn', 'panic', 9],
    ]);
  });

  it('waits out the floor for a change to the rest alone, and a frame change overtakes it', async () => {
    const { soon, later, pushes, select, write } = publisher();
    select([line(0)]);
    soon.run();
    await flush();
    write({ visible: 4, range: { from: 'a', to: 'b' } });
    expect(soon.due()).toBe(false);
    expect(later.due()).toBe(true);
    write({ visible: 5 });
    select([line(1)]);
    expect(later.due()).toBe(false);
    soon.run();
    expect(pushes.at(-1)!.viewport).toMatchObject({ visible: 5, selection: [{ message: 'line 1' }] });
  });

  it('sends nothing when no field changed since the last push', async () => {
    const { soon, later, pushes, select, write, viewport } = publisher();
    select([line(0)]);
    soon.run();
    await flush();
    viewport.set({ ...viewport.get() });
    write({ visible: 3 });
    write({ visible: 0 });
    later.run();
    expect(soon.due()).toBe(false);
    expect(pushes).toHaveLength(1);
  });
});

describe('createViewportPublisher: one in flight', () => {
  it('holds every send until the one in flight settles, then sends the latest once', async () => {
    const held: (() => void)[] = [];
    const { soon, pushes, select } = publisher({
      send: () =>
        new Promise<void>((resolve) => {
          held.push(resolve);
        }),
    });
    select([line(0)]);
    soon.run();
    select([line(1)]);
    soon.run();
    select([line(2)]);
    soon.run();
    expect(pushes).toHaveLength(1);

    held[0]();
    await flush();
    expect(pushes.map((x) => x.viewport.selection[0].message)).toEqual(['line 0', 'line 2']);
    held[1]();
    await flush();
    expect(pushes).toHaveLength(2);
  });

  it('sends at once on flush, as the page blurs', () => {
    const { p, soon, later, pushes, write } = publisher();
    write({ visible: 2 });
    p.flush();
    expect(pushes).toHaveLength(1);
    expect(soon.due() || later.due()).toBe(false);
  });

  it('reports a refused push and does not send it again', async () => {
    const refusal = new Error('a push over its bound');
    const { p, soon, pushes, report, select } = publisher({ send: () => Promise.reject(refusal) });
    select([line(0)]);
    soon.run();
    await flush();
    expect(report).toHaveBeenCalledWith(refusal);
    p.flush();
    expect(pushes).toHaveLength(1);

    select([line(1)]);
    soon.run();
    expect(pushes).toHaveLength(2);
  });

  it('stops listening, and drops a send due, on stop', () => {
    const { stop, soon, pushes, select } = publisher();
    select([line(0)]);
    stop();
    expect(soon.due()).toBe(false);
    select([line(1)]);
    expect(soon.due()).toBe(false);
    expect(pushes).toHaveLength(0);
  });
});

describe('createMirror', () => {
  const pushOf = (generation: number, selectionRevision: number, messages: string[], visible = 1): ViewportPush => ({
    generation,
    grep: null,
    viewport: {
      ...EMPTY_LOG_VIEWPORT,
      visible,
      selection: cutSelection(messages.map((m, i) => line(i, m))),
      selectedCount: messages.length,
      selectionRevision,
    },
  });

  it('starts empty, and folds a push into the viewport it carries, without the revision', () => {
    const mirror = createMirror();
    expect(mirror.viewport.get()).toBe(EMPTY_LOG_VIEWPORT);
    const listener = vi.fn();
    mirror.viewport.subscribe(listener);
    mirror.fold(pushOf(1, 1, ['boom'], 7));
    expect(listener).toHaveBeenCalledOnce();
    expect(mirror.viewport.get()).toEqual({
      ...EMPTY_LOG_VIEWPORT,
      visible: 7,
      selection: [{ timestamp: line(0).timestamp, stream: line(0).stream, message: 'boom' }],
      selectedCount: 1,
    });
  });

  it('keeps the selection array while the generation and the revision hold', () => {
    const mirror = createMirror();
    mirror.fold(pushOf(1, 1, ['boom']));
    const kept = mirror.viewport.get().selection;
    mirror.fold(pushOf(1, 1, ['boom'], 9));
    expect(mirror.viewport.get().visible).toBe(9);
    expect(mirror.viewport.get().selection).toBe(kept);

    mirror.fold(pushOf(1, 2, ['boom']));
    expect(mirror.viewport.get().selection).not.toBe(kept);
  });

  it('lets the selection go on a new generation, and empties on a null push', () => {
    const mirror = createMirror();
    mirror.fold(pushOf(1, 1, ['boom']));
    const kept = mirror.viewport.get().selection;
    mirror.fold(pushOf(2, 1, ['boom']));
    expect(mirror.viewport.get().selection).not.toBe(kept);

    mirror.fold(null);
    expect(mirror.viewport.get()).toBe(EMPTY_LOG_VIEWPORT);
    const after = pushOf(2, 1, ['boom']);
    mirror.fold(after);
    expect(mirror.viewport.get().selection).toBe(after.viewport.selection);
  });
});
