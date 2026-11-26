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

// Which columns a kind page's table shows, and in what order, per kind. Chrome, kept in
// localStorage like the log viewer's columns. It keeps only the user's choices, each against
// the column's default, so a column the table gains later takes its own default place and
// visibility.
import { useState } from 'react';

import { gvrKey } from '@/lib/gvr';
import type { GVR } from '@/lib/gvr';
import { storageKey } from '@/lib/storage-key';

export type TableColumnChoices = {
  // Column key → shown.
  shown: Readonly<Record<string, boolean>>;
  // Column keys in the user's order; a column missing from it keeps its default place.
  order: readonly string[];
};

const NONE: TableColumnChoices = { shown: {}, order: [] };

/** The choices from what localStorage holds; a part that is not the right shape reads as none. */
export function readChoices(stored: string | null): TableColumnChoices {
  if (stored === null) return NONE;
  let value: unknown;
  try {
    value = JSON.parse(stored);
  } catch {
    return NONE;
  }
  if (typeof value !== 'object' || value === null) return NONE;
  const { shown, order } = value as { shown?: unknown; order?: unknown };
  return {
    shown:
      typeof shown === 'object' && shown !== null && !Array.isArray(shown)
        ? Object.fromEntries(Object.entries(shown).filter(([, on]) => typeof on === 'boolean'))
        : {},
    order: Array.isArray(order) ? order.filter((k): k is string => typeof k === 'string') : [],
  };
}

/**
 * The columns in the user's order. The columns it names fill the places those columns hold by
 * default, in its order; every other column keeps its default place.
 */
export function arrange<C extends { key: string }>(columns: readonly C[], order: readonly string[]): C[] {
  const rank = new Map(order.map((key, i) => [key, i]));
  const ranked = columns.filter((c) => rank.has(c.key)).toSorted((a, b) => rank.get(a.key)! - rank.get(b.key)!);
  let next = 0;
  return columns.map((c) => (rank.has(c.key) ? ranked[next++] : c));
}

export type TableColumns = {
  choices: TableColumnChoices;
  // Whether any column is off its default.
  changed: boolean;
  setShown: (key: string, shown: boolean) => void;
  setOrder: (keys: readonly string[]) => void;
  // Back to every column's default.
  reset: () => void;
};

/** The choices and their setters. Storage can refuse either way; the choices then live in state alone. */
export function useTableColumns(gvr: GVR): TableColumns {
  const key = storageKey(`table-columns:${gvrKey(gvr)}`);
  const [choices, setChoices] = useState(() => {
    try {
      return readChoices(localStorage.getItem(key));
    } catch {
      return NONE;
    }
  });
  const save = (next: TableColumnChoices) => {
    setChoices(next);
    try {
      if (next === NONE) localStorage.removeItem(key);
      else localStorage.setItem(key, JSON.stringify(next));
    } catch {
      // Kept for this mount alone.
    }
  };
  return {
    choices,
    changed: choices.order.length > 0 || Object.keys(choices.shown).length > 0,
    setShown: (column, shown) => save({ ...choices, shown: { ...choices.shown, [column]: shown } }),
    setOrder: (keys) => save({ ...choices, order: keys }),
    reset: () => save(NONE),
  };
}
