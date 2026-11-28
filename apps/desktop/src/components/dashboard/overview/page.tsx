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

// Dashboard → Overview: the tiles, then *Needs attention*, then the namespaces beside the
// recent warnings. Every number is `useOverview`'s; a section waits for its own watches.
import { useOverview } from '@/lib/overview';

import { Namespaces } from './namespaces';
import { NeedsAttention } from './needs-attention';
import { RecentWarnings } from './recent-warnings';
import { Tiles } from './tiles';

export function OverviewPage() {
  const { active, syncing, nodes, pods, namespaces, warnings } = useOverview();
  if (!active) {
    return (
      <p className="text-sm text-muted-foreground">
        No synced cache for this cluster yet — the overview appears once syncing has started.
      </p>
    );
  }
  return (
    // The stack sits inside the scroller: as the scroller itself, a flex column would squeeze
    // the cards before it scrolled. The scroller is a container, so the layout follows the
    // page's width, which the right sidebar narrows to 320px.
    <div className="@container flex-1 overflow-y-auto">
      <div className="flex flex-col gap-3">
        {syncing && <p className="text-xs/4.5 text-muted-foreground">Still syncing — counts may be incomplete.</p>}
        <Tiles nodes={nodes} pods={pods?.counts} namespaces={namespaces} warnings={warnings} />
        <NeedsAttention problems={pods?.problems} syncing={syncing} />
        <div className="grid grid-cols-1 gap-3 @3xl:grid-cols-2">
          <Namespaces rows={namespaces} />
          <RecentWarnings warnings={warnings} syncing={syncing} />
        </div>
      </div>
    </div>
  );
}
