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

// A rule's scope as the webview writes it: a path of `/`-separated segments
// whose first is the account. The sidecar matches it segment by segment, `*`
// never crossing `/`, `*` alone matching anything and a last `**` matching
// the segments before it and everything under them. An account is opaque: it
// is written only through `literal`, as the sidecar's permissions.Literal
// writes one.

/** literal is the pattern that matches s alone. */
export function literal(s: string): string {
  return s.replaceAll(/[*?\\]/g, (c) => `\\${c}`);
}

/** unliteral is the value a pattern matches alone, null for one `literal` would not have written. */
function unliteral(pattern: string): string | null {
  let out = '';
  for (let i = 0; i < pattern.length; i++) {
    const c = pattern[i];
    if (c === '*' || c === '?') return null;
    if (c === '\\') {
      i++;
      if (i === pattern.length) return null;
      out += pattern[i];
      continue;
    }
    out += c;
  }
  return literal(out) === pattern ? out : null;
}

/** ScopeChoice is a scope as the rule form picks it. */
export type ScopeChoice = {
  /** The account, as the sidecar names it; null for every account. */
  account: string | null;
  /** A pattern for the segment under the account (a namespace, a region); empty for anything. */
  place: string;
  /** The account's own objects alone, under no place: a cluster's cluster-scoped objects. */
  accountOnly?: boolean;
};

/**
 * scopeFor is the scope a choice writes. Every account is `*`, which matches anything, so
 * `accountOnly` reads only beside an account.
 */
export function scopeFor({ account, place, accountOnly }: ScopeChoice): string {
  if (account === null) return place ? `*/${place}` : '*';
  const named = literal(account);
  if (accountOnly) return named;
  return place ? `${named}/${place}` : `${named}/**`;
}

/** parseScope is the choice that writes scope, null for a scope the form cannot draw. */
export function parseScope(scope: string): Required<ScopeChoice> | null {
  const segments = scope.split('/');
  if (segments.length > 2) return null;
  const [first, place = ''] = segments;
  const account = first === '*' ? null : unliteral(first);
  if (first !== '*' && account === null) return null;
  if (segments.length === 1) return { account, place: '', accountOnly: account !== null };
  if (place === '**') return account === null ? null : { account, place: '', accountOnly: false };
  if (place === '') return null;
  return { account, place, accountOnly: false };
}
