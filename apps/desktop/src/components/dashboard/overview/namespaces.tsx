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

// The namespaces in scope, the busiest first, with their workloads and pods.
import { VisibleText } from '@/components/primitives/visible-text';
import type { NamespaceRow } from '@/lib/overview';
import { visibleTitle } from '@/lib/visible-text';

import { OverviewCard } from './card';
import type { CardColumn } from './card';

const COLUMNS: CardColumn<NamespaceRow>[] = [
  {
    key: 'namespace',
    header: 'Namespace',
    cell: ({ name }) => (
      <span className="font-mono text-foreground" title={visibleTitle(name)}>
        <VisibleText text={name} />
      </span>
    ),
  },
  { key: 'workloads', header: 'Workloads', cell: (r) => <span className="tabular-nums">{r.workloads}</span> },
  { key: 'pods', header: 'Pods', cell: (r) => <span className="tabular-nums">{r.pods}</span> },
];

export function Namespaces({ rows }: { rows?: readonly NamespaceRow[] }) {
  const n = rows?.length ?? 0;
  return (
    <OverviewCard
      title="Namespaces"
      count={n}
      grid="grid-cols-[minmax(0,1fr)_64px_40px]"
      columns={COLUMNS}
      rows={rows}
      rowKey={(r) => r.name}
      empty="No namespaces"
      link={{ label: `Show all ${n}`, resource: 'namespaces' }}
    />
  );
}
