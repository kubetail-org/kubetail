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

// What every kind's columns are built from.
//
// `rawJSON` is `unknown` off the wire — the server does no per-field typing — so each kind
// names the narrow shape it reads (`kind<PodBody>(…)`), and its readers must degrade to "—"
// rather than throw on a partial body (e.g. a Deleted row's last-known state).
import type { ReactNode } from 'react';
import ReactTimeAgo from 'react-timeago';
import type { Formatter } from 'react-timeago';

import { StatusPill } from '@/components/dashboard/status-pill';
import type { StatusTone } from '@/components/dashboard/status-pill';
import type { VirtualColumn } from '@/components/dashboard/virtual-table';
import { VisibleText } from '@/components/primitives/visible-text';
import type { ClusterCachedDataObject } from '@/lib/cluster-cached-data-objects';
import { gvrKey } from '@/lib/gvr';
import { humanDuration } from '@/lib/human-duration';
import type { SortValue } from '@/lib/sort-rows';

export type Status = { label: string; tone: StatusTone };

// Every kind column is sized: the table's layout is fixed, and an unsized column would
// split the slack with Name.
export type ObjectColumn = VirtualColumn<ClusterCachedDataObject> & {
  width: number;
  // A status column's: the tone its pill draws, which the table's tabs count.
  tone?: (o: ClusterCachedDataObject) => StatusTone;
};

// A column that reads the object's body, so a kind names its body's shape once. Only the
// table's own actions column is pinned.
export type KindColumn<T> = Omit<ObjectColumn, 'cell' | 'sortValue' | 'tone' | 'pinned'> & {
  pinned?: false;
  cell: (b: T) => ReactNode;
  sortValue?: (b: T) => SortValue;
  tone?: (b: T) => StatusTone;
};

// One kind's columns under its `gvrKey`, each reading `rawJSON` as the kind's body.
export function kind<T>(apiVersion: string, resource: string, columns: KindColumn<T>[]): [string, ObjectColumn[]] {
  // {} so a missing body flows through the optional chains to "—".
  const body = (o: ClusterCachedDataObject) => (o.rawJSON ?? {}) as T;
  return [
    gvrKey({ apiVersion, resource }),
    columns.map(({ cell, sortValue, tone, ...column }) => ({
      ...column,
      cell: (o) => cell(body(o)),
      sortValue: sortValue && ((o) => sortValue(body(o))),
      tone: tone && ((o) => tone(body(o))),
    })),
  ];
}

export const DASH = '—';

// An age as kubectl prints it, largest unit only: 45s, 22m, 3d.
const UNIT: Record<string, string> = {
  second: 's',
  minute: 'm',
  hour: 'h',
  day: 'd',
  week: 'w',
  month: 'mo',
  year: 'y',
};
const shortAge: Formatter = (value, unit) => `${value}${UNIT[unit]}`;

// The time since `ms`, muted; a dash for a time that did not parse.
export function Age({ ms }: { ms: number }) {
  return (
    <span className="text-xs text-muted-foreground">
      {Number.isNaN(ms) ? DASH : <ReactTimeAgo date={ms} component="span" formatter={shortAge} maxPeriod={60} />}
    </span>
  );
}

// Youngest first.
export function ageSortValue(ms: number): number | undefined {
  return Number.isNaN(ms) ? undefined : -ms;
}

// Text the cluster wrote, cut to its column with the whole of it on hover.
export function Text({ text, mono = false }: { text: string | undefined; mono?: boolean }) {
  if (!text) return DASH;
  return (
    <span title={text} className={mono ? 'font-mono text-xs' : undefined}>
      <VisibleText text={text} />
    </span>
  );
}

// A condition's status by type, as the API writes it: "True", "False", "Unknown".
export function conditionStatus(
  conditions: readonly { type?: string; status?: string }[] | undefined,
  type: string,
): string | undefined {
  return conditions?.find((c) => c.type === type)?.status;
}

export type LabelSelector = {
  matchLabels?: Record<string, string>;
  matchExpressions?: { key?: string; operator?: string; values?: string[] }[];
};

// A label map as `k=v,k2=v2`; nothing for an empty one.
export function labelsText(labels: Record<string, string> | undefined): string | undefined {
  return (
    Object.entries(labels ?? {})
      .map(([k, v]) => `${k}=${v}`)
      .join(',') || undefined
  );
}

// A selector as kubectl's `FormatLabelSelector` prints it: `app=web,tier in (a,b),!legacy`;
// nothing for an empty one, which selects everything.
export function selectorText(s: LabelSelector | undefined): string | undefined {
  const parts = Object.entries(s?.matchLabels ?? {}).map(([k, v]) => `${k}=${v}`);
  for (const e of s?.matchExpressions ?? []) {
    const values = (e.values ?? []).join(',');
    if (e.operator === 'In') parts.push(`${e.key} in (${values})`);
    else if (e.operator === 'NotIn') parts.push(`${e.key} notin (${values})`);
    else if (e.operator === 'Exists') parts.push(`${e.key}`);
    else if (e.operator === 'DoesNotExist') parts.push(`!${e.key}`);
  }
  return parts.length ? parts.join(',') : undefined;
}

// A quantity's suffixes, as the API writes them: milli, decimal and binary.
const SUFFIX: Record<string, number> = {
  m: 1e-3,
  '': 1,
  k: 1e3,
  M: 1e6,
  G: 1e9,
  T: 1e12,
  P: 1e15,
  E: 1e18,
  Ki: 2 ** 10,
  Mi: 2 ** 20,
  Gi: 2 ** 30,
  Ti: 2 ** 40,
  Pi: 2 ** 50,
  Ei: 2 ** 60,
};

// A quantity as a number; undefined for a form it does not read (an exponent), which sorts last.
export function quantityValue(q: string | undefined): number | undefined {
  const m = q?.match(/^(\d+(?:\.\d+)?)([A-Za-z]*)$/);
  if (!m || !Object.hasOwn(SUFFIX, m[2])) return undefined;
  return Number(m[1]) * SUFFIX[m[2]];
}

type ColumnOptions = { hidden?: boolean };
type TextOptions = ColumnOptions & { mono?: boolean };

// What every builder's column starts from. Its header is its key: a kind names each once.
function base(header: string, width: number, { hidden }: ColumnOptions = {}) {
  return { key: header, header, width, hiddenByDefault: hidden };
}

// The same, for a column of figures.
function figures(header: string, width: number, options?: ColumnOptions) {
  return { ...base(header, width, options), className: 'tabular-nums' };
}

// A column of one text field.
export function textColumn<T>(
  header: string,
  width: number,
  read: (b: T) => string | undefined,
  options: TextOptions = {},
): KindColumn<T> {
  return {
    ...base(header, width, options),
    className: 'truncate',
    sortValue: (b) => read(b) || undefined,
    cell: (b) => <Text text={read(b)} mono={options.mono} />,
  };
}

// A column of a list, joined by commas.
export function listColumn<T>(
  header: string,
  width: number,
  read: (b: T) => readonly (string | undefined)[] | undefined,
  options?: TextOptions,
): KindColumn<T> {
  return textColumn(header, width, (b) => read(b)?.filter(Boolean).join(', '), options);
}

// A column of a quantity, drawn as written and sorted by its size, largest first.
export function quantityColumn<T>(
  header: string,
  width: number,
  read: (b: T) => string | undefined,
  options?: ColumnOptions,
): KindColumn<T> {
  return {
    ...figures(header, width, options),
    sortValue: (b) => quantityValue(read(b)),
    descFirst: true,
    cell: (b) => <Text text={read(b)} />,
  };
}

// A column of a count, largest first.
export function countColumn<T>(
  header: string,
  width: number,
  read: (b: T) => number | undefined,
  options?: ColumnOptions,
): KindColumn<T> {
  return {
    ...figures(header, width, options),
    sortValue: read,
    descFirst: true,
    cell: (b) => read(b) ?? DASH,
  };
}

// A column of `ready/total`, least ready first; a total of zero is not short of any.
export function readyColumn<T>(
  header: string,
  read: (b: T) => { ready: number; total: number } | undefined,
  options?: ColumnOptions,
): KindColumn<T> {
  return {
    ...figures(header, 64, options),
    sortValue: (b) => {
      const r = read(b);
      return r && (r.total ? r.ready / r.total : 1);
    },
    cell: (b) => {
      const r = read(b);
      return r ? `${r.ready}/${r.total}` : DASH;
    },
  };
}

// A column of a length of time in seconds, as kubectl prints one, longest first.
export function durationColumn<T>(
  header: string,
  width: number,
  read: (b: T) => number | undefined,
  options?: ColumnOptions,
): KindColumn<T> {
  return {
    ...figures(header, width, options),
    sortValue: read,
    descFirst: true,
    cell: (b) => {
      const seconds = read(b);
      return seconds === undefined ? DASH : humanDuration(seconds * 1000);
    },
  };
}

// A column of the time since a timestamp the object carries.
export function ageColumn<T>(
  header: string,
  width: number,
  read: (b: T) => string | undefined,
  options?: ColumnOptions,
): KindColumn<T> {
  const ms = (b: T) => {
    const at = read(b);
    return at ? Date.parse(at) : Number.NaN;
  };
  return {
    ...figures(header, width, options),
    sortValue: (b) => ageSortValue(ms(b)),
    cell: (b) => <Age ms={ms(b)} />,
  };
}

// A status pill. `read` names the status and its tone, or nothing for a dash; the table's
// tabs count an object with no status among the pending.
export function statusColumn<T>(header: string, width: number, read: (b: T) => Status | undefined): KindColumn<T> {
  return {
    ...base(header, width),
    sortValue: (b) => read(b)?.label,
    tone: (b) => read(b)?.tone ?? 'pending',
    cell: (b) => {
      const s = read(b);
      return s ? <StatusPill label={s.label} tone={s.tone} /> : DASH;
    },
  };
}
