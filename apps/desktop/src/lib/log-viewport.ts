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

// What the log viewer knows that nothing else does, readable on demand. The viewer
// writes its state here as it changes; a reader pulls it once when it needs it —
// the composer at send, for the snapshot that rides the question. The sidebar's
// handle is read in place; a log window's is pushed to a mirror in its app window
// (`log-window-push.ts`). The shape is useSyncExternalStore's.

import type { CoreState, LogLine } from '@/lib/log-viewer';

/** A selected line as the snapshot reads it. */
export type SelectedLine = Pick<LogLine, 'timestamp' | 'stream' | 'message'>;

export type LogViewport = Pick<CoreState, 'range' | 'atEnd' | 'pinnedToEnd' | 'visible'> & {
  /** Whether the user paused the viewer: no line arrives until they play it again. */
  paused: boolean;
  /** The grep the viewer runs, which the user can change from the view's. Null with no viewer. */
  grep: string | null;
  /** The lines the user selected, in order: all of them, or the first a log window's push carried. */
  selection: SelectedLine[];
  /** How many lines the user selected. */
  selectedCount: number;
};

export type LogViewportHandle = {
  get: () => LogViewport;
  subscribe: (listener: () => void) => () => void;
  /** The viewer's write. Nothing else calls it. */
  set: (next: LogViewport) => void;
};

export const EMPTY_LOG_VIEWPORT: LogViewport = {
  range: null,
  atEnd: false,
  pinnedToEnd: false,
  paused: false,
  visible: 0,
  grep: null,
  selection: [],
  selectedCount: 0,
};

export function createLogViewportHandle(): LogViewportHandle {
  let state = EMPTY_LOG_VIEWPORT;
  const listeners = new Set<() => void>();
  return {
    get: () => state,
    subscribe: (listener) => {
      listeners.add(listener);
      return () => listeners.delete(listener);
    },
    set: (next) => {
      state = next;
      listeners.forEach((listener) => listener());
    },
  };
}
