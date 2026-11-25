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

// A log window's viewport on its way to the app window. The log window pushes it to
// the host (`log_window_viewport`), which forwards it down the app window's channel
// into a mirror the composer reads.
import { scheduleFlush } from '@/lib/graphql/use-folded-subscription';
import { createLogViewportHandle, EMPTY_LOG_VIEWPORT } from '@/lib/log-viewport';
import type { LogViewport, LogViewportHandle, SelectedLine } from '@/lib/log-viewport';
import { MAX_SELECTED } from '@/lib/logs-snapshot';

/**
 * The most bytes of selected lines one push carries, serialized as UTF-8. The host
 * refuses a push over 256 KiB; the rest of one is a grep and a few stamps.
 */
export const PUSH_LINES_BYTES = 192 * 1024;

const encoder = new TextEncoder();

/**
 * The selected lines a push carries: whole lines in order, at most `MAX_SELECTED`,
 * while their serialized size stays within `PUSH_LINES_BYTES`. A message is never
 * cut, since the sidecar redacts each whole one before it cuts it. The longest
 * message the sidecar hands out serializes well within the bound, so the first
 * selected line always fits.
 */
export function cutSelection(selection: readonly SelectedLine[]): SelectedLine[] {
  const lines = selection
    .slice(0, MAX_SELECTED)
    .map(({ timestamp, stream, message }) => ({ timestamp, stream, message }));
  // The brackets, then each line and a comma before every line but the first.
  let size = 2;
  const leftOut = lines.findIndex((line, i) => {
    size += encoder.encode(JSON.stringify(line)).length + (i > 0 ? 1 : 0);
    return size > PUSH_LINES_BYTES;
  });
  return leftOut === -1 ? lines : lines.slice(0, leftOut);
}

/** A viewport as it crosses to the app window. */
export type PushedViewport = LogViewport & {
  /** Steps each time the viewer writes a new selection array, so the mirror can keep its own. */
  selectionRevision: number;
};

/** What `log_window_viewport` carries. The host reads `generation` and `grep` alone. */
export type ViewportPush = { generation: number; grep: string | null; viewport: PushedViewport };

/** Runs `run` later, once; returns a cancel. */
export type Schedule = (run: () => void) => () => void;

const PUSH_FLOOR_MS = 250;

const afterFloor: Schedule = (run) => {
  const timer = setTimeout(run, PUSH_FLOOR_MS);
  return () => clearTimeout(timer);
};

type PublisherOptions = {
  /** The page load the host answered for. */
  generation: number;
  /** The route's grep in effect. */
  grep: string | null;
  viewport: LogViewportHandle;
  send: (push: ViewportPush) => Promise<unknown>;
  report: (e: unknown) => void;
  /** Paces what the chip reads: the next frame, or the floor in a window with none. */
  soon?: Schedule;
  /** Paces the rest, which a scroll or a following tail changes on every frame. */
  later?: Schedule;
};

type Sent = { viewport: LogViewport; grep: string | null };

// What a push would change since the last one: what the chip reads, the rest alone,
// or nothing.
function changeOf(sent: Sent | null, viewport: LogViewport, grep: string | null): 'soon' | 'later' | null {
  if (
    sent === null ||
    sent.viewport.selection !== viewport.selection ||
    sent.viewport.selectedCount !== viewport.selectedCount ||
    sent.viewport.grep !== viewport.grep ||
    sent.grep !== grep
  ) {
    return 'soon';
  }
  const was = sent.viewport;
  if (
    was.range?.from !== viewport.range?.from ||
    was.range?.to !== viewport.range?.to ||
    was.atEnd !== viewport.atEnd ||
    was.pinnedToEnd !== viewport.pinnedToEnd ||
    was.paused !== viewport.paused ||
    was.visible !== viewport.visible
  ) {
    return 'later';
  }
  return null;
}

/**
 * Pushes a log window's viewport to the host as it changes. One push is in flight at
 * a time: the host accepts every push of the current generation in the order it
 * lands, so two in flight could leave an older one standing. A send that comes due
 * meanwhile goes out when it settles, with the latest state. The revision counter
 * lives as long as the publisher, so a viewer mounted again never reuses one.
 */
export function createViewportPublisher({
  generation,
  grep: initialGrep,
  viewport,
  send,
  report,
  soon = scheduleFlush,
  later = afterFloor,
}: PublisherOptions) {
  let grep = initialGrep;
  let sent: Sent | null = null;
  let inFlight = false;
  let due = false;
  let pending: { kind: 'soon' | 'later'; cancel: () => void } | null = null;

  // The cut is computed once per selection array and reused until the viewer writes
  // a new one.
  let { selection } = viewport.get();
  let cut = cutSelection(selection);
  let revision = 0;

  const cancel = () => {
    pending?.cancel();
    pending = null;
  };

  const push = () => {
    if (inFlight) {
      due = true;
      return;
    }
    const current = viewport.get();
    if (changeOf(sent, current, grep) === null) return;
    if (current.selection !== selection) {
      ({ selection } = current);
      cut = cutSelection(selection);
      revision += 1;
    }
    sent = { viewport: current, grep };
    inFlight = true;
    send({ generation, grep, viewport: { ...current, selection: cut, selectionRevision: revision } })
      .catch(report)
      .finally(() => {
        inFlight = false;
        if (due) {
          due = false;
          push();
        }
      });
  };

  const changed = () => {
    const kind = changeOf(sent, viewport.get(), grep);
    if (kind === null || pending?.kind === 'soon' || pending?.kind === kind) return;
    cancel();
    pending = {
      kind,
      cancel: (kind === 'soon' ? soon : later)(() => {
        pending = null;
        push();
      }),
    };
  };

  return {
    /** Listens to the viewport until the returned stop. */
    start: () => {
      const unsubscribe = viewport.subscribe(changed);
      return () => {
        unsubscribe();
        cancel();
      };
    },
    /** The route's grep in effect changed. */
    setGrep: (next: string | null) => {
      grep = next;
      changed();
    },
    /** Sends now, as the user leaves for the app window. */
    flush: () => {
      cancel();
      push();
    },
  };
}

/**
 * The app window's copy of one log window's viewport, which the composer reads at
 * the press. `fold` takes each push the host forwards; a null push is a page that
 * reloaded.
 */
export type ViewportMirror = {
  viewport: LogViewportHandle;
  fold: (push: ViewportPush | null) => void;
};

export function createMirror(): ViewportMirror {
  const viewport = createLogViewportHandle();
  // Which selection the mirror holds, as the last push named it.
  let held: { generation: number; revision: number } | null = null;
  return {
    viewport,
    fold: (push) => {
      if (push === null) {
        held = null;
        viewport.set(EMPTY_LOG_VIEWPORT);
        return;
      }
      const { selectionRevision: revision, ...next } = push.viewport;
      // The selection array is kept while the push names the one held: the composer
      // compares selections by identity, so a scroll must not bring back a selection
      // the user dismissed.
      const same = held?.generation === push.generation && held.revision === revision;
      held = { generation: push.generation, revision };
      viewport.set(same ? { ...next, selection: viewport.get().selection } : next);
    },
  };
}
