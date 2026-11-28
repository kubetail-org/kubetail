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

// Which columns each kind's table draws: a hand-written entry, else the kind's own printer
// columns.
import ReactTimeAgo from 'react-timeago';

import { ageSortValue } from '@/components/dashboard/age';
import type { ClusterCachedDataObject } from '@/lib/cluster-cached-data-objects';
import type { StatusTone } from '@/lib/cluster-health';
import type { PrinterColumn } from '@/lib/dashboard-resources';
import { gvrKey } from '@/lib/gvr';
import type { GVR } from '@/lib/gvr';
import { readPath } from '@/lib/jsonpath';

import { accessKinds } from './access';
import { clusterKinds } from './cluster';
import { DASH, Text } from './common';
import type { ObjectColumn, Status } from './common';
import { configKinds } from './config';
import { draKinds } from './dra';
import { extensionKinds } from './extensions';
import { networkKinds } from './network';
import { podKinds } from './pods';
import { storageKinds } from './storage';
import { workloadKinds } from './workloads';

// Keyed by gvrKey — the canonical kind key the watch provenance and kinds catalog share.
const KINDS = new Map([
  ...podKinds,
  ...workloadKinds,
  ...clusterKinds,
  ...draKinds,
  ...networkKinds,
  ...configKinds,
  ...storageKinds,
  ...accessKinds,
  ...extensionKinds,
]);

// The tone of a kind's objects, where the kind has a status column.
export function toneForKind(gvr: GVR): ((o: ClusterCachedDataObject) => StatusTone) | undefined {
  return KINDS.get(gvrKey(gvr))?.find((c) => c.tone)?.tone;
}

// The status of a kind's objects, as its status column's pill draws it, where the kind has one.
export function statusForKind(gvr: GVR): ((o: ClusterCachedDataObject) => Status | undefined) | undefined {
  return KINDS.get(gvrKey(gvr))?.find((c) => c.status)?.status;
}

// Each descriptor's key: its name, not its place, so what a user chose for a column holds
// when the CRD adds or moves another. The prefix keeps it off the table's own keys, and a
// name used again takes its count.
function descriptorKeys(descriptors: readonly PrinterColumn[]): string[] {
  const seen = new Map<string, number>();
  return descriptors.map((d) => {
    const n = (seen.get(d.name) ?? 0) + 1;
    seen.set(d.name, n);
    return n === 1 ? `printer:${d.name}` : `printer:${d.name}#${n}`;
  });
}

// One column built from a CRD's descriptor: read the path, render by declared type.
//
// **Only scalars render.** A path landing on an object or an array has no cell form — kubectl
// would print its JSON, which is unreadable in a table — so it reads as absent.
function descriptorColumn(d: PrinterColumn, key: string): ObjectColumn {
  const numeric = d.type === 'integer' || d.type === 'number';
  const scalar = (o: ClusterCachedDataObject) => {
    const value = readPath(o.rawJSON, d.jsonPath);
    return typeof value === 'string' || typeof value === 'number' || typeof value === 'boolean' ? value : undefined;
  };
  return {
    key,
    header: d.name,
    className: numeric || d.type === 'date' ? 'truncate tabular-nums' : 'truncate',
    width: 128,
    // kubectl prints these under `-o wide` alone.
    hiddenByDefault: d.priority > 0,
    sortValue: (o) => {
      const value = scalar(o);
      if (value === undefined) return undefined;
      if (numeric) return Number(value);
      if (d.type === 'date') return ageSortValue(Date.parse(String(value)));
      return String(value);
    },
    descFirst: numeric,
    cell: (o) => {
      const value = scalar(o);
      if (value === undefined) return DASH;
      if (d.type === 'date') {
        const ms = Date.parse(String(value));
        return Number.isNaN(ms) ? DASH : <ReactTimeAgo date={ms} component="span" maxPeriod={60} />;
      }
      return <Text text={String(value)} />;
    },
  };
}

// A hand-written entry wins, else the kind's own descriptors, else [] (universal columns only).
//
// **The hand-written entry wins on purpose** — an accessor is hand-written precisely because it
// says something a jsonPath cannot, the way podStatus reads container state to correct a
// crash-looping pod that reports `phase: Running`.
export function columnsForKind(gvr: GVR, printerColumns: readonly PrinterColumn[] = []): ObjectColumn[] {
  const written = KINDS.get(gvrKey(gvr));
  if (written) return written;
  const keys = descriptorKeys(printerColumns);
  return printerColumns.map((d, i) => descriptorColumn(d, keys[i]));
}
