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

// The JSONPath subset a CRD's printer columns are read with, over an object's native body.
//
// **Deliberately not a JSONPath library.** Kubernetes validates a printer column's path by
// parsing it with the full jsonpath parser, so a filter (`[?(@.type=="Ready")]`) or a wildcard is
// a legal CRD — this reader does not evaluate those, and answers undefined so the cell renders
// empty. Closing that gap means a general JSONPath dependency evaluating cluster-authored
// expressions over untrusted bodies, for columns almost nobody writes.

// SEGMENT matches one step, anchored so anything it cannot describe ends the walk:
// `.name`, `[0]`, `['quoted key']`, `["quoted key"]`.
const SEGMENT = /^(?:\.([a-zA-Z0-9_-]+)|\[(\d+)\]|\['([^']*)'\]|\["([^"]*)"\])/;

// readPath resolves path against body, or undefined for a path that names nothing, uses a form
// this does not support, or runs off a non-object. It never throws: the body is `unknown` off the
// wire and may be partial, and a column is not worth a broken row.
export function readPath(body: unknown, path: string): unknown {
  // A leading dot is required of every printer column, so its absence is a path for someone
  // else's evaluator.
  if (!path.startsWith('.')) return undefined;

  let current = body;
  let rest = path;
  while (rest.length > 0) {
    const match = SEGMENT.exec(rest);
    if (!match) return undefined;

    const key = match[1] ?? match[2] ?? match[3] ?? match[4];
    if (current === null || typeof current !== 'object') return undefined;
    current = (current as Record<string, unknown>)[key];
    rest = rest.slice(match[0].length);
  }
  return current;
}
