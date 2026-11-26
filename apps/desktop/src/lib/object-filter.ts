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

// A kind page's text filter: a case-insensitive substring of the object's name, of the
// owner drawn under it (`deploy/checkout-api`), of one of its labels as `key=value`, of the
// node it runs on, or of its namespace when the table shows that. Spaces at the ends are
// ignored.
import { ownerOf } from '@/lib/object-owner';

// `spec` is the kind's own: a custom resource may hold anything at `nodeName`.
type Body = { metadata?: { labels?: Record<string, string> }; spec?: { nodeName?: unknown } };

export function matchesFilter(
  o: { name: string; namespace: string; rawJSON: unknown },
  query: string,
  { namespace = false }: { namespace?: boolean } = {},
): boolean {
  const q = query.trim().toLowerCase();
  if (!q) return true;
  const b = (o.rawJSON ?? {}) as Body;
  const labels = Object.entries(b.metadata?.labels ?? {}).map(([k, v]) => `${k}=${v}`);
  const node = b.spec?.nodeName;
  const texts = [
    o.name,
    ownerOf(o.rawJSON) ?? '',
    ...labels,
    typeof node === 'string' ? node : '',
    namespace ? o.namespace : '',
  ];
  return texts.some((text) => text.toLowerCase().includes(q));
}
