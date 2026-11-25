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

// The picker's list: one row per kubeconfig context, the window's first, each with
// its record's health. Names and summaries are cluster data, drawn as text. A
// filter appears once the list is long enough to need one.
import { Check } from 'lucide-react';
import { useState } from 'react';
import type { RefObject } from 'react';

import { ClusterDot } from '@/components/cluster/cluster-dot';
import { UnverifiedTLSBadge } from '@/components/cluster/unverified-tls-badge';
import { Input } from '@/components/primitives/text-entry';
import { contextHealth } from '@/lib/cluster-health';
import type { Cluster } from '@/lib/clusters';
import type { KubeContextInfo } from '@/lib/kube-config';

// From this many contexts the list grows a filter.
const FILTER_FROM = 6;

export function ClusterList({
  contexts,
  current,
  clusters,
  onSelect,
  filterRef,
}: {
  contexts: KubeContextInfo[];
  current: string;
  // null until the clusters snapshot lands; every context then reads as not reached.
  clusters: Cluster[] | null;
  onSelect: (name: string) => void;
  // The popup focuses the filter on open, when there is one.
  filterRef: RefObject<HTMLInputElement | null>;
}) {
  const [query, setQuery] = useState('');
  const needle = query.toLowerCase();
  const ordered = [...contexts.filter((c) => c.name === current), ...contexts.filter((c) => c.name !== current)];
  const visible = ordered.filter((c) => c.name.toLowerCase().includes(needle));

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-2">
      {contexts.length >= FILTER_FROM && (
        <Input
          ref={filterRef}
          placeholder="Filter clusters"
          aria-label="Filter clusters"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter' && visible.length > 0) onSelect(visible[0].name);
          }}
        />
      )}
      {visible.length === 0 ? (
        <p className="px-3 py-2 text-sm text-muted-foreground">No cluster matches.</p>
      ) : (
        <ul className="flex min-h-0 flex-col overflow-y-auto">
          {visible.map((c) => {
            const health = contextHealth(clusters, c.name);
            const isCurrent = c.name === current;
            return (
              <li key={c.name}>
                <button
                  type="button"
                  aria-current={isCurrent ? 'true' : undefined}
                  onClick={() => onSelect(c.name)}
                  className="flex w-full items-center gap-3 rounded-lg px-3 py-2 text-left hover:bg-accent focus-visible:bg-accent focus-visible:outline-hidden"
                >
                  <ClusterDot tone={health.tone} />
                  <span className="flex min-w-0 flex-1 flex-col">
                    <span data-testid="cluster-name" className="truncate text-sm">
                      {c.name}
                    </span>
                    <span className="truncate text-xs text-muted-foreground">{health.summary}</span>
                  </span>
                  <UnverifiedTLSBadge entry={c.clusterEntry} />
                  {isCurrent && <Check aria-hidden className="size-4 shrink-0" />}
                </button>
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
}
