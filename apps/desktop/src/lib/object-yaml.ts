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

// An object's body as YAML, the way `kubectl get -o yaml` prints it.
import { stringify } from 'yaml';

// kubectl's order for the top-level keys; the rest keep the body's own.
const LEADING = ['apiVersion', 'kind', 'metadata', 'spec', 'status'];

export function objectYaml(body: unknown): string {
  if (typeof body !== 'object' || body === null || Array.isArray(body)) return stringify(body ?? null);
  const fields = body as Record<string, unknown>;
  const ordered = Object.fromEntries([
    ...LEADING.filter((k) => k in fields).map((k): [string, unknown] => [k, fields[k]]),
    ...Object.entries(fields).filter(([k]) => !LEADING.includes(k)),
  ]);
  // A long value stays on its line: folding it would change what the user copies.
  return stringify(ordered, { lineWidth: 0 });
}
