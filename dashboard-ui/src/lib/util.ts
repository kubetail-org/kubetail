// Copyright 2024-2025 Andres Morey
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

import { type ClassValue, clsx } from 'clsx';
import { twMerge } from 'tailwind-merge';

import { config } from '@/app-config';

/**
 * Python-like counter class
 */

export class Counter<K = string> extends Map<K, number> {
  constructor(values?: K[] | Counter<K>) {
    super();

    if (values instanceof Counter) {
      values.forEach((value, key) => {
        this.set(key, value);
      });
    } else {
      values?.forEach((val) => this.update(val));
    }
  }

  update(key: K, incr = 1) {
    const count = this.get(key) || 0;
    this.set(key, count + incr);
  }

  orderedEntries() {
    const entries = Array.from(this.entries());

    // sort by counts
    entries.sort((a, b) => {
      const [aKey, aVal] = a;
      const [bKey, bVal] = b;

      if (aVal === bVal && typeof aKey === 'string' && typeof bKey === 'string') {
        return aKey.localeCompare(bKey);
      }
      return bVal - aVal;
    });

    return entries;
  }
}

/**
 * A map of sets class
 */

export class MapSet<K = string, T = string> extends Map<K, Set<T>> {
  constructor(values?: MapSet<K, T>) {
    super();

    if (values) {
      values.forEach((value, key) => {
        this.set(key, value);
      });
    }
  }

  add(key: K, value: T) {
    const s = this.get(key) || new Set();
    s.add(value);
    this.set(key, s);
  }
}

/**
 * Go-like WaitGroup class
 */

export class WaitGroup {
  private counter = 0;

  /**
   * Increment internal counter
   */
  add(n = 1): void {
    if (n <= 0) throw new Error('must be positive');
    this.counter += n;
  }

  /**
   * Decrement the counter by 1
   */
  done(): void {
    if (this.counter <= 0) throw new Error('done() called more times than add()');
    this.counter -= 1;
  }

  /**
   * Return true if wait group is empty
   */
  isEmpty(): boolean {
    return this.counter === 0;
  }
}

/**
 * Classname merger
 */

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

/**
 * CSS-safe encoder
 */

export function cssEncode(name: string) {
  return name.replace(/[^a-z0-9]/g, (s) => {
    const c = s.charCodeAt(0);
    if (c === 32) return '-';
    if (c >= 65 && c <= 90) return `_${s.toLowerCase()}`;
    const x = `000${c.toString(16)}`;
    return `__${x.slice(-4)}`;
  });
}

/**
 * Url path helper
 */

export function joinPaths(...paths: string[]) {
  return paths
    .map((part, index) => {
      if (index === 0) {
        return part.replace(/\/+$/, '');
      }
      return part.replace(/^\/+|\/+$/g, '');
    })
    .join('/');
}

/**
 * Get path basename
 */

let basename: string | undefined;

export function getBasename() {
  // check cache
  if (basename) return basename;

  const { pathname } = window.location;
  if (pathname.includes('/proxy/')) {
    const m = pathname.match(/^(.*?)\/proxy\//);
    if (m) [basename] = m;
  } else {
    basename = config.basePath;
  }

  return basename as string;
}

/**
 * Find intersection of multiple sets
 */

export function intersectSets<T = string>(sets: Set<T>[]): Set<T> {
  if (sets.length === 0) return new Set<T>();

  // Start with the first set
  let intersection = new Set(sets[0]);

  // Iterate over the rest of the sets
  sets.slice(1).forEach((set) => {
    // Retain only elements that are present in both sets
    intersection = new Set([...intersection].filter((x) => set.has(x)));
  });

  return intersection;
}

/*
 * Calculate hash with fallback if crypto.subtle is not available
 */

export async function safeDigest(input: string): Promise<DataView> {
  const bytes = new TextEncoder().encode(input);

  if (globalThis.crypto && 'subtle' in globalThis.crypto) {
    // Use Web Crypto API when available
    const buffer = await crypto.subtle.digest('SHA-256', bytes);
    return new DataView(buffer);
  }

  // Non-crypto fallback hash (FNV-1a 32-bit over UTF-8)
  let h = 0x811c9dc5; // FNV offset basis
  for (let i = 0; i < bytes.length; i += 1) {
    h ^= bytes[i]; // eslint-disable-line no-bitwise
    h = Math.imul(h, 0x01000193); // FNV prime
  }
  h >>>= 0; // eslint-disable-line no-bitwise

  const buf = new ArrayBuffer(32);
  const view = new DataView(buf);
  view.setUint32(0, h, false); // big-endian
  return view;
}
