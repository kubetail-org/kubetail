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

// A pane's width, clamped and remembered. Windows share one localStorage but never
// sync live — resizing one leaves the others untouched; a new window opens at the
// last-saved width.
import { useCallback, useState } from 'react';

import { storageKey } from '@/lib/storage-key';

export type WidthBounds = { min: number; max: number; initial: number };

export function usePersistedWidth(name: string, { min, max, initial }: WidthBounds): [number, (px: number) => void] {
  const key = storageKey(name);
  const clamp = useCallback((px: number) => Math.min(max, Math.max(min, px)), [min, max]);

  // Storage can be unavailable or refuse a write; the pane then keeps its width for the
  // session alone.
  const read = useCallback(() => {
    let stored = 0;
    try {
      stored = Number(localStorage.getItem(key));
    } catch {
      // The default below.
    }
    // `Number` reads `Infinity`, which no clamp to an unbounded maximum would catch.
    return Number.isFinite(stored) && stored > 0 ? clamp(stored) : initial;
  }, [clamp, initial, key]);

  // The key travels with the state, so a caller that switches names (a pane whose
  // width is kept per mode) reads the new one's width instead of carrying the old
  // one's across.
  const [state, setState] = useState(() => ({ key, width: read() }));
  if (state.key !== key) setState({ key, width: read() });

  const set = useCallback(
    (px: number) => {
      const next = clamp(px);
      setState({ key, width: next });
      try {
        localStorage.setItem(key, String(next));
      } catch {
        // Kept for the session alone.
      }
    },
    [clamp, key],
  );

  return [state.width, set];
}
