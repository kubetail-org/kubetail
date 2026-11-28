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

// The Events page's type tabs: every event, the Warnings, the Normals. An event of any other
// type, or of none, is under All alone.
import { Tabs, TabsList, TabsTrigger } from '@kubetail/ui/elements/tabs';
import { cn } from '@kubetail/ui/lib/utils';

import type { ClusterCachedDataEvent } from '@/lib/cluster-cached-data-events';
import { TONE } from '@/lib/cluster-health';

export type EventTab = 'all' | 'warning' | 'normal';

const TABS: readonly EventTab[] = ['all', 'warning', 'normal'];
const LABEL: Record<EventTab, string> = { all: 'All', warning: 'Warning', normal: 'Normal' };
const TYPE: Record<Exclude<EventTab, 'all'>, string> = { warning: 'Warning', normal: 'Normal' };

export function inTab(e: ClusterCachedDataEvent, tab: EventTab): boolean {
  return tab === 'all' || e.type === TYPE[tab];
}

export function tabCounts(events: readonly ClusterCachedDataEvent[]): Record<EventTab, number> {
  const counts: Record<EventTab, number> = { all: events.length, warning: 0, normal: 0 };
  for (const e of events) {
    if (inTab(e, 'warning')) counts.warning += 1;
    else if (inTab(e, 'normal')) counts.normal += 1;
  }
  return counts;
}

type TypeTabsProps = {
  value: EventTab;
  onChange: (tab: EventTab) => void;
  counts: Record<EventTab, number>;
};

export function TypeTabs({ value, onChange, counts }: TypeTabsProps) {
  return (
    <Tabs value={value} onValueChange={(v) => onChange(v as EventTab)}>
      <TabsList aria-label="Events by type">
        {TABS.map((t) => (
          <TabsTrigger key={t} value={t} className="gap-1.5 px-2.5">
            {LABEL[t]}
            <span
              className={cn(
                'text-xs font-normal tabular-nums',
                // Only a Warning count above zero is coloured.
                t === 'warning' && counts[t] > 0 ? TONE.attention.text : 'text-muted-foreground',
              )}
            >
              {counts[t]}
            </span>
          </TabsTrigger>
        ))}
      </TabsList>
    </Tabs>
  );
}
