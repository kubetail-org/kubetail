// Copyright 2024 Andres Morey
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

import { ArrowTopRightOnSquareIcon } from '@heroicons/react/24/outline';

import { typenameMap } from '@/lib/workload';

type ConsoleLinkSourceFragment = {
  __typename?: string;
  metadata: {
    namespace: string;
    name: string;
  };
};

const openConsole = (source: ConsoleLinkSourceFragment) => {
  const workload = typenameMap[source.__typename || ''];
  if (!workload) throw new Error(`not implemented: ${source.__typename}`);

  const urlArg = `${workload}/${encodeURIComponent(source.metadata.namespace)}/${encodeURIComponent(source.metadata.name)}`;
  const windowArgs = 'width=700,height=400,location=no,menubar=no,resizable=yes,scrollbars=no,status=no,titlebar=no,toolbar=no';
  window.open(`/console?source=${urlArg}`, '_blank', windowArgs);
};

export function ConsoleLink({ source }: { source: ConsoleLinkSourceFragment }) {
  return (
    <span
      className="flex items-center space-x-[2px] underline text-primary cursor-pointer"
      onClick={() => openConsole(source)}
    >
      <span>logs</span>
      <ArrowTopRightOnSquareIcon className="h-[16px]" />
    </span>
  );
}

/**
 * CSS-safe encoder
 */

export function cssEncode(name: string) {
  return name.replace(/[^a-z0-9]/g, function (s) {
    var c = s.charCodeAt(0);
    if (c == 32) return '-';
    if (c >= 65 && c <= 90) return '_' + s.toLowerCase();
    return '__' + ('000' + c.toString(16)).slice(-4);
  });
}

/**
 * Python-like counter class
 */

export class Counter<K = string> extends Map<K, number> {
  constructor(values?: K[] | Counter<K>) {
    super();

    if (values instanceof Counter) {
      for (const [key, value] of values.entries()) {
        this.set(key, value);
      }
    } else {
      values?.forEach(val => this.update(val));
    }
  }

  update(key: K, incr: number = 1) {
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
    })

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
      for (const [key, value] of values.entries()) {
        this.set(key, value);
      }
    }
  }

  add(key: K, value: T) {
    const s = this.get(key) || new Set();
    s.add(value);
    this.set(key, s);
  }
}

/**
 * Get CSRF token from server
 */

let csrfToken: string | null = null;

export async function getCSRFToken() {
  if (csrfToken === null) {
    const url = new URL(joinPaths(getBasename(), '/csrf-token'), window.location.origin);
    const resp = await fetch(url);
    csrfToken = (await resp.json()).value;
  }
  return csrfToken;
}

/**
 * Find intersection of multiple sets
 */

export function intersectSets<T = string>(sets: Set<T>[]): Set<T> {
  if (sets.length === 0) return new Set<T>();

  // Start with the first set
  let intersection = new Set(sets[0]);

  // Iterate over the rest of the sets
  for (let set of sets.slice(1)) {
    // Retain only elements that are present in both sets
    intersection = new Set([...intersection].filter(x => set.has(x)));
  }

  return intersection;
}

/**
 * Get path basename
 */

let basename: string | undefined;

export function getBasename() {
  // check cache
  if (basename) return basename;

  const pathname = window.location.pathname;
  if (pathname.includes('/proxy/')) {
    const m = pathname.match(/^(.*?)\/proxy\//);
    if (m) basename = m[0];
  } else {
    basename = '/';
  }

  return basename as string;
}

/**
 * Url path helper
 */

export function joinPaths(...paths: string[]) {
  return paths.map((part, index) => {
      if (index === 0) {
          return part.replace(/\/+$/, '');
      } else {
          return part.replace(/^\/+|\/+$/g, '');
      }
  }).join('/');
}
