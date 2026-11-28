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

// The Overview's top row: four tiles, each a label, a figure and a line under it.
import { cn } from '@kubetail/ui/lib/utils';
import type { ReactNode } from 'react';

import type { ClusterCachedDataEvent } from '@/lib/cluster-cached-data-events';
import { TONE } from '@/lib/cluster-health';
import type { NamespaceRow, NodeCounts, PodCounts } from '@/lib/overview';

import { Loading } from './loading';

const MUTED = 'text-muted-foreground';

// Undefined data draws the spinner in place of the figure and its line.
function Tile({ label, value, sub }: { label: string; value?: ReactNode; sub?: ReactNode }) {
  return (
    <div className="flex min-w-0 flex-col gap-1 rounded-lg border border-border bg-background px-3.5 py-3">
      <div className="truncate text-xs/4.5 font-medium text-muted-foreground">{label}</div>
      {value === undefined ? (
        <Loading />
      ) : (
        <>
          <div className="truncate text-[22px] leading-none font-semibold text-foreground tabular-nums">{value}</div>
          <div className="truncate text-xs/4.5">{sub}</div>
        </>
      )}
    </div>
  );
}

function nodesLine({ ready, total }: NodeCounts): ReactNode {
  if (total === 0) return <span className={MUTED}>No nodes</span>;
  if (ready === total) return <span className={TONE.ok.text}>All nodes Ready</span>;
  return <span className={TONE.error.text}>{total - ready} not ready</span>;
}

function podsLine({ failing, pending, total }: PodCounts): ReactNode {
  if (total === 0) return <span className={MUTED}>No pods</span>;
  if (failing === 0 && pending === 0) return <span className={TONE.ok.text}>All pods running</span>;
  return (
    <>
      {failing > 0 && <span className={TONE.error.text}>{failing} failing</span>}
      {failing > 0 && pending > 0 && <span className={MUTED}> · </span>}
      {pending > 0 && <span className={TONE.attention.text}>{pending} pending</span>}
    </>
  );
}

function workloadsLine(namespaces: readonly NamespaceRow[]): ReactNode {
  const n = namespaces.reduce((sum, row) => sum + row.workloads, 0);
  return <span className={MUTED}>{n === 1 ? '1 workload' : `${n} workloads`}</span>;
}

type TilesProps = {
  nodes?: NodeCounts;
  pods?: PodCounts;
  namespaces?: readonly NamespaceRow[];
  warnings?: readonly ClusterCachedDataEvent[];
};

export function Tiles({ nodes, pods, namespaces, warnings }: TilesProps) {
  return (
    <div className="grid grid-cols-1 gap-3 @sm:grid-cols-2 @3xl:grid-cols-4">
      <Tile label="Nodes ready" value={nodes && `${nodes.ready} / ${nodes.total}`} sub={nodes && nodesLine(nodes)} />
      <Tile label="Pods running" value={pods && `${pods.running} / ${pods.total}`} sub={pods && podsLine(pods)} />
      <Tile label="Namespaces" value={namespaces?.length} sub={namespaces && workloadsLine(namespaces)} />
      <Tile
        label="Warnings"
        value={warnings && <span className={cn(warnings.length > 0 && TONE.error.text)}>{warnings.length}</span>}
        sub={<span className={MUTED}>in the last hour</span>}
      />
    </div>
  );
}
