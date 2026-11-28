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

// The Incidents dialog: the filter's target lines and incidents beside one incident's
// detail. The selection and the filter are its own state, so each window keeps its
// own and both start over at each opening. A filter or selection whose target or
// incident leaves the provider's lists is dropped in the render that sees it. An
// incident opened on that held lists lack waits for the next live ones.
import { Button } from '@kubetail/ui/elements/button';
import { X } from 'lucide-react';
import { useMemo, useState } from 'react';

import { DialogClose, DialogTitle, PanelDialog } from '@/components/primitives/dialog';
import { useActiveCluster } from '@/lib/active-cluster';
import { useClusters } from '@/lib/clusters';
import { type AppDialogProps, useDialogHost } from '@/lib/dialog';
import { useLastSettled } from '@/lib/graphql/use-last-settled';
import { useIncidents, useMonitorTargets } from '@/lib/incidents';
import type { Incident, MonitorTarget } from '@/lib/incidents';

import { MonitorTargetStatus } from '../target-status';
import { IncidentDetail } from './detail';
import { ALL_TARGETS, TargetFilter } from './filter';
import { IncidentList } from './list';

export function IncidentsDialog({ open, onOpenChange }: AppDialogProps) {
  return (
    <PanelDialog
      open={open}
      onOpenChange={onOpenChange}
      className="h-160 w-220 flex-col bg-popover shadow-2xl sm:max-w-[calc(100%-2rem)]"
    >
      <div className="flex items-center gap-3 border-b py-3 pr-4 pl-6">
        <DialogTitle className="flex-1 text-base font-medium text-popover-foreground">Incidents</DialogTitle>
        <DialogClose aria-label="Close" render={<Button variant="ghost" size="icon" className="size-7 rounded-md" />}>
          <X className="size-4" />
        </DialogClose>
      </div>
      <IncidentsBody />
    </PanelDialog>
  );
}

// Waits for the provider's snapshots, so the opening's filter and selection are
// chosen once, against whole lists, and holds the last lists through a reconnect,
// which would otherwise unmount the panes and lose what the user picked.
function IncidentsBody() {
  const host = useDialogHost();
  const liveTargets = useMonitorTargets();
  const liveIncidents = useIncidents();
  const { clusters } = useClusters();
  const settled = liveTargets !== null && liveIncidents !== null;
  const heldTargets = useLastSettled(liveTargets, settled);
  const heldIncidents = useLastSettled(liveIncidents, settled);
  // A cluster the clusters list drops takes its target and incidents with it at
  // once, though the held lists are older: the incident watch may be reconnecting.
  const [targets, incidents] = useMemo(() => {
    if (!heldTargets || !heldIncidents || !clusters) return [heldTargets, heldIncidents];
    const clusterIDs = new Set(clusters.map((c) => c.id));
    const kept = heldTargets.filter((t) => t.clusterID === null || clusterIDs.has(t.clusterID));
    const keptIDs = new Set(kept.map((t) => t.id));
    return [kept, heldIncidents.filter((i) => keptIDs.has(i.targetID))];
  }, [heldTargets, heldIncidents, clusters]);
  if (!targets || !incidents) return <p className="px-6 py-4 text-sm text-muted-foreground">Loading…</p>;
  // An open while open chooses its filter and selection afresh; a reconnect keeps them.
  return <IncidentsPanes key={host?.openSeq} targets={targets} incidents={incidents} live={settled} />;
}

function IncidentsPanes({
  targets,
  incidents,
  live,
}: {
  targets: MonitorTarget[];
  incidents: Incident[];
  // Whether the lists are the provider's current ones, not held through a reconnect.
  live: boolean;
}) {
  const host = useDialogHost();
  const { clusterID } = useActiveCluster();
  const findOpenedOn = () => incidents.find((i) => i.id === host?.incidentID);
  // A target's id, or ALL_TARGETS.
  const [filter, setFilter] = useState(
    () =>
      findOpenedOn()?.targetID ??
      targets.find((t) => clusterID !== undefined && t.clusterID === clusterID)?.id ??
      ALL_TARGETS,
  );
  const [selectedID, setSelectedID] = useState(() => findOpenedOn()?.id ?? null);
  // The incident opened on, while held lists lack it: a press can name one newer
  // than they are. Selected when it appears; dropped once live lists lack it.
  const [awaitedID, setAwaitedID] = useState(() => (live || findOpenedOn() ? null : (host?.incidentID ?? null)));
  if (awaitedID !== null) {
    const awaited = incidents.find((i) => i.id === awaitedID);
    if (awaited) {
      setAwaitedID(null);
      setFilter(awaited.targetID);
      setSelectedID(awaited.id);
    } else if (live) {
      setAwaitedID(null);
    }
  }

  const targetsByID = useMemo(() => new Map(targets.map((t) => [t.id, t])), [targets]);
  const filterTarget = targetsByID.get(filter);
  const selected = incidents.find((i) => i.id === selectedID);
  // Dropped for good: a target or incident that returns is not picked again.
  if (filter !== ALL_TARGETS && !filterTarget) setFilter(ALL_TARGETS);
  if (selectedID !== null && !selected) setSelectedID(null);

  const shownIncidents = filterTarget ? incidents.filter((i) => i.targetID === filterTarget.id) : incidents;

  // A filter that leaves the selection out clears it, so the detail never shows a
  // row the list does not.
  const pickFilter = (next: string) => {
    setFilter(next);
    if (selected && next !== ALL_TARGETS && selected.targetID !== next) setSelectedID(null);
  };
  const selectedTarget = selected && targetsByID.get(selected.targetID);

  return (
    <div className="flex min-h-0 flex-1">
      <div className="flex w-96 min-w-56 shrink flex-col gap-3 overflow-y-auto border-r px-4 py-3">
        <TargetFilter targets={targets} value={filter} onPick={pickFilter} />
        <div className="flex flex-col gap-1.5 px-2">
          {(filterTarget ? [filterTarget] : targets).map((t) => (
            <MonitorTargetStatus key={t.id} target={t} />
          ))}
        </div>
        {shownIncidents.length === 0 ? (
          <p className="px-2 text-sm text-muted-foreground">Nothing found</p>
        ) : (
          <IncidentList
            incidents={shownIncidents}
            targets={targetsByID}
            selectedID={selectedID}
            onSelect={setSelectedID}
          />
        )}
      </div>
      <div className="min-w-0 flex-1 overflow-y-auto px-6 py-4">
        {selected && selectedTarget ? (
          <IncidentDetail incident={selected} target={selectedTarget} />
        ) : (
          <p className="text-sm text-muted-foreground">Select an incident to see its occurrences.</p>
        )}
      </div>
    </div>
  );
}
