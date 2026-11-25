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

// The picker a scoped pane leads with, over useScope: every cluster, then the
// kubeconfig's clusters, then those no longer in it. A cluster's name is its own
// text, so it is drawn through VisibleText.
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectSeparator,
  SelectTrigger,
  SelectValue,
} from '@kubetail/ui/elements/select';
import { useId } from 'react';

import { VisibleText } from '@/components/primitives/visible-text';
import { EVERY, type Scope } from '@/lib/cluster-scope';
import { type Cluster, displayName } from '@/lib/clusters';

type Count = (scope: string) => number | undefined;

const noCount: Count = () => undefined;

export function ScopePicker({
  scope,
  onPick,
  count = noCount,
}: {
  scope: Scope;
  onPick: (picked: string) => void;
  /** What a scope holds, shown beside its name. */
  count?: Count;
}) {
  const labelID = useId();
  const { present, archived, cluster } = scope;
  return (
    <div className="flex flex-col gap-1.5">
      <span id={labelID} className="text-xs text-muted-foreground">
        Applies to
      </span>
      <Select value={cluster?.id ?? EVERY} onValueChange={(next) => onPick(next as string)}>
        <SelectTrigger size="sm" aria-labelledby={labelID} className="w-50">
          <SelectValue>
            <VisibleText text={cluster ? displayName(cluster) : 'Every cluster'} />
          </SelectValue>
        </SelectTrigger>
        <SelectContent alignItemWithTrigger={false}>
          <ScopeItem value={EVERY} name="Every cluster" count={count} />
          <SelectSeparator />
          <ScopeGroup label="Kubeconfig" clusters={present} count={count} />
          {archived.length > 0 && <ScopeGroup label="Archived" clusters={archived} count={count} />}
        </SelectContent>
      </Select>
    </div>
  );
}

function ScopeGroup({ label, clusters, count }: { label: string; clusters: Cluster[]; count: Count }) {
  return (
    <SelectGroup>
      <SelectLabel>{label}</SelectLabel>
      {clusters.map((c) => (
        <ScopeItem key={c.id} value={c.id} name={displayName(c)} count={count} />
      ))}
    </SelectGroup>
  );
}

function ScopeItem({ value, name, count }: { value: string; name: string; count: Count }) {
  return (
    <SelectItem value={value}>
      <VisibleText text={name} />
      <span className="ml-auto text-xs text-muted-foreground">{count(value)}</span>
    </SelectItem>
  );
}
