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

// The Sandbox pane's New chats: the sandbox switch a new chat starts with on
// one cluster, picked as Memory's scope is; every cluster's is the status
// card's switch. A chat copies it when it is created, so a change leaves every
// chat that exists alone. Any change that starts new chats outside the
// sandbox, a reset included, is said in a dialog first; one that keeps them in
// it is not.
import { Button } from '@kubetail/ui/elements/button';
import { Switch } from '@kubetail/ui/elements/switch';
import { useState } from 'react';

import { Dialog } from '@/components/primitives/dialog';
import { useScope } from '@/lib/cluster-scope';
import { useSandboxDefault, useSandboxDefaultSwitch } from '@/lib/sandbox-default';

import { Group } from './group';
import { ScopePicker } from './scope-picker';

export function SandboxNewChats() {
  const [picked, setPicked] = useState('');
  const scope = useScope(picked);
  // Until one is picked, the first cluster listed.
  const cluster = scope.cluster ?? scope.present[0] ?? scope.archived[0];
  if (!cluster) return null;
  return (
    <Group
      title="New chats on a cluster"
      lede="Where a new chat's commands run on one cluster, in place of the switch above, until you switch the chat. Chats that exist keep their own switch."
    >
      <ScopePicker scope={{ ...scope, cluster }} onPick={setPicked} everyCluster={false} />
      {/* Keyed on the cluster, so an open confirm does not carry over to another. */}
      <ScopeDefault key={cluster.id} clusterID={cluster.id} />
    </Group>
  );
}

// What a confirm would do: write the scope's own value, or reset it.
type Pending = 'outside' | 'reset' | null;

function ScopeDefault({ clusterID }: { clusterID: string }) {
  const own = useSandboxDefault(clusterID);
  // Whether a reset confirms rests on every cluster's value.
  const every = useSandboxDefault(null);
  const { switching, setSandboxDisabled, reset, everyClusterChanged } = useSandboxDefaultSwitch(clusterID);
  const [pending, setPending] = useState<Pending>(null);
  if (!own) return null;

  const onReset = () => {
    /* v8 ignore next -- the button is disabled until every cluster's value is known */
    if (!every) return;
    // Every reset that lands outside confirms, even from a cluster already
    // outside: another window may have put it inside since, and the sidecar
    // checks only where the reset lands.
    if (every.sandboxDisabled) setPending('reset');
    else reset(every.sandboxDisabled);
  };
  const confirm = () => {
    // The confirm named a move outside, so the reset must land there.
    if (pending === 'reset') reset(true);
    else setSandboxDisabled(true);
    setPending(null);
  };

  return (
    <>
      <ul className="divide-y rounded-lg border">
        <li className="flex items-center gap-4 px-4 py-3">
          <div className="flex min-w-0 flex-1 flex-col gap-0.5">
            <span className="text-sm text-popover-foreground">Start new chats in the sandbox</span>
            <span className="text-xs text-muted-foreground">
              {own.sandboxDisabled
                ? 'New chats run their commands as you, with your files, credentials and network, each asking first.'
                : 'New chats run their commands in the sandbox.'}
              {!own.clusterOverride && " Follows every cluster's setting."}
            </span>
          </div>
          {own.clusterOverride && (
            // Disabled until every cluster's value is known, which says whether a reset confirms.
            <Button size="sm" variant="ghost" disabled={switching || !every} onClick={onReset}>
              Use every cluster&apos;s
            </Button>
          )}
          <Switch
            aria-label="Start new chats in the sandbox"
            checked={!own.sandboxDisabled}
            disabled={switching}
            onCheckedChange={(inside) => (inside ? setSandboxDisabled(false) : setPending('outside'))}
          />
        </li>
      </ul>
      {/* The sidecar refused a reset that would have landed elsewhere than the dialog said. */}
      {everyClusterChanged && (
        <p className="text-xs text-destructive">Every cluster&apos;s default changed. Check it, then try again.</p>
      )}
      <Dialog
        open={pending !== null}
        onOpenChange={(open) => !open && setPending(null)}
        title="Start new chats outside the sandbox?"
        description={
          'Every command in a chat started from now on will run as you, with your files, your credentials and the network, after you approve it. ' +
          'Chats that exist keep their own switch.'
        }
      >
        <div className="flex justify-end gap-2">
          <Button variant="outline" onClick={() => setPending(null)}>
            Cancel
          </Button>
          <Button variant="destructive" onClick={confirm}>
            Start outside the sandbox
          </Button>
        </div>
      </Dialog>
    </>
  );
}
