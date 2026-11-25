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

// The Settings dialog's Memory tab: the instructions and notes of one scope,
// every cluster or one.
import { Button } from '@kubetail/ui/elements/button';
import { Spinner } from '@kubetail/ui/elements/spinner';
import { Plus } from 'lucide-react';
import { type ReactNode, useState } from 'react';

import { MemoryInstructions } from '@/components/cluster/memory-instructions';
import { MemoryList } from '@/components/cluster/memory-list';
import { Dialog } from '@/components/primitives/dialog';
import { VisibleText } from '@/components/primitives/visible-text';
import { useActiveCluster } from '@/lib/active-cluster';
import { EVERY, useScope } from '@/lib/cluster-scope';
import { displayName, useClusters } from '@/lib/clusters';
import { type Memory, useMemories } from '@/lib/memories';
import { useMemoryWrites } from '@/lib/memory-writes';

import { ScopePicker } from './scope-picker';

// Placeholder: awaits a count field on `Cluster`, or a `memoryCounts` query.
const noteCount = (_scope: string): number | undefined => undefined;

export function MemorySettings() {
  const { clusters } = useClusters();
  const { clusterID: activeID } = useActiveCluster();
  const [picked, setPicked] = useState(EVERY);
  const [clearing, setClearing] = useState(false);

  const scope = useScope(picked);
  const { cluster } = scope;
  const name = cluster && displayName(cluster);
  const clusterID = cluster?.id ?? null;

  // Every cluster's notes ride on any cluster's watch.
  const watchID = cluster?.id ?? activeID ?? clusters?.[0]?.id;
  const { memories, phase } = useMemories(watchID);
  const notes = memories.filter((m) => m.clusterID === clusterID);

  let pending;
  if (!watchID && clusters !== null) {
    pending = <p className="text-xs text-muted-foreground">Kubetail has not reached a cluster yet.</p>;
  } else if (!watchID || phase === 'connecting') {
    pending = <Spinner />;
  }

  return (
    <>
      <div className="flex items-end justify-between gap-3">
        <ScopePicker scope={scope} onPick={setPicked} count={noteCount} />
        <Button variant="ghost" size="sm" className="text-destructive" onClick={() => setClearing(true)}>
          Clear memory
        </Button>
      </div>
      {/* Keyed on the scope, so no instructions or open editor carry over to another. */}
      <ScopeMemory
        key={clusterID}
        scope={name ?? 'every cluster'}
        clusterID={clusterID}
        notes={notes}
        pending={pending}
      />
      <Dialog
        open={clearing}
        onOpenChange={setClearing}
        title={<>Clear memory for {name ? <VisibleText text={name} /> : 'every cluster'}?</>}
        description={`This deletes its instructions and ${notes.length} notes.${cluster ? " Other clusters aren't affected." : ''}`}
      >
        <div className="flex justify-end gap-2">
          <Button variant="outline" onClick={() => setClearing(false)}>
            Cancel
          </Button>
          {/* Placeholder: awaits a `memoryClear(clusterID: ClusterID)` mutation. */}
          <Button variant="destructive" disabled>
            Clear
          </Button>
        </div>
      </Dialog>
    </>
  );
}

// One scope's instructions and notes. `pending` stands in for the list until
// the notes arrive.
function ScopeMemory({
  scope,
  clusterID,
  notes,
  pending,
}: {
  scope: string;
  clusterID: string | null;
  notes: Memory[];
  pending: ReactNode;
}) {
  const { save, remove } = useMemoryWrites();
  const [adding, setAdding] = useState(false);

  return (
    <>
      {/* Placeholder: local state, awaiting a stored instructions record per scope. */}
      <MemoryInstructions description="Given to every chat." />
      <section className="flex flex-col gap-2.5">
        <div className="flex items-end justify-between gap-3">
          <div className="flex flex-col gap-0.5">
            <h4 className="text-sm font-medium">Notes</h4>
            <p className="text-xs text-muted-foreground">
              Read at the start of each chat. You can edit or remove any note.
            </p>
          </div>
          <Button variant="outline" size="sm" disabled={adding || !!pending} onClick={() => setAdding(true)}>
            <Plus aria-hidden />
            Add note
          </Button>
        </div>
        {pending ?? (
          <MemoryList
            scope={scope}
            memories={notes}
            adding={adding}
            onSave={(memory, name, body) => save({ id: memory?.id ?? null, clusterID, name, body })}
            onAddDone={() => setAdding(false)}
            onDelete={remove}
          />
        )}
      </section>
    </>
  );
}
