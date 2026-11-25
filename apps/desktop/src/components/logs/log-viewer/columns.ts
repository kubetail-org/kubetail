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

// The log viewer's columns: which there are, which are on, and the colour of a container's dot.
// The set the user picks is chrome, kept in localStorage like the sidebars' widths.
import { useCallback, useState } from 'react';

import { storageKey } from '@/lib/storage-key';

export const COLUMNS = [
  'Timestamp',
  'Dot',
  'Pod',
  'Container',
  'Node',
  'Region',
  'Zone',
  'OS',
  'Arch',
  'Message',
] as const;

export type Column = (typeof COLUMNS)[number];

export const DEFAULT_COLUMNS: Column[] = ['Timestamp', 'Dot', 'Message'];

const isColumn = (name: unknown): name is Column => COLUMNS.includes(name as Column);

/** The visible set from what localStorage holds: known names alone, in column order, the message always last. */
export function readColumns(stored: string | null): Column[] {
  if (stored === null) return DEFAULT_COLUMNS;
  let names: unknown;
  try {
    names = JSON.parse(stored);
  } catch {
    return DEFAULT_COLUMNS;
  }
  if (!Array.isArray(names)) return DEFAULT_COLUMNS;
  const picked = new Set(names.filter(isColumn));
  picked.add('Message');
  return COLUMNS.filter((column) => picked.has(column));
}

/** What localStorage keeps: the visible names but the message, which is always on. */
export function writeColumns(columns: Column[]): string {
  return JSON.stringify(columns.filter((column) => column !== 'Message'));
}

const KEY = storageKey('log-viewer-columns');

/** The visible columns and their setter. Storage can refuse either way; the set then lives in state alone. */
export function useLogViewerColumns(): [Column[], (columns: Column[]) => void] {
  const [columns, setState] = useState(() => {
    try {
      return readColumns(localStorage.getItem(KEY));
    } catch {
      return DEFAULT_COLUMNS;
    }
  });
  const set = useCallback((next: Column[]) => {
    setState(next);
    try {
      localStorage.setItem(KEY, writeColumns(next));
    } catch {
      // Kept for this mount alone.
    }
  }, []);
  return [columns, set];
}

export const DOT_PALETTE = [
  '#3B6EDC',
  '#2F9E5F',
  '#D14343',
  '#D38B2A',
  '#8456D8',
  '#2C9CB3',
  '#7A6BD1',
  '#D14D8A',
  '#7FA83A',
  '#E06C3A',
  '#2F9A8A',
  '#5C63D6',
  '#A46A3D',
  '#C24A77',
  '#6B8F3A',
  '#4B4FCF',
  '#9A4EB3',
  '#BFA23A',
  '#4A84C8',
  '#247A8A',
];

/**
 * A container's colour, the same in every view and window with nothing stored. `/` can be in
 * none of the three names, so two containers never hash the same string.
 */
export function dotColor(namespace: string, pod: string, container: string): string {
  // FNV-1a, 32-bit.
  let hash = 0x811c9dc5;
  const text = `${namespace}/${pod}/${container}`;
  for (let i = 0; i < text.length; i += 1) {
    hash ^= text.charCodeAt(i);
    hash = Math.imul(hash, 0x01000193);
  }
  return DOT_PALETTE[(hash >>> 0) % DOT_PALETTE.length];
}
