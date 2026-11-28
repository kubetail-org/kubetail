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

// The bell at the app bar's right end, beside Settings: every target's open incidents
// but a baseline one, counted on the button, each opening the Incidents dialog on it
// in this window. Labels a target returned are drawn through VisibleText.
import { Button } from '@kubetail/ui/elements/button';
import { Popover, PopoverContent, PopoverTrigger } from '@kubetail/ui/elements/popover';
import { Bell } from 'lucide-react';
import { useMemo, useState } from 'react';

import { IncidentHeadline } from '@/components/monitoring/incident-headline';
import { MonitorTargetStatus } from '@/components/monitoring/target-status';
import { VisibleText } from '@/components/primitives/visible-text';
import { LIST_ROW_CLASS } from '@/lib/control-style';
import { useDialog } from '@/lib/dialog';
import { targetStatus, useIncidents, useMonitorTargets } from '@/lib/incidents';
import type { Incident, MonitorTarget } from '@/lib/incidents';

import { AppBarButton } from './button';

export function NotificationButton() {
  const { openDialog } = useDialog();
  const [open, setOpen] = useState(false);
  const targets = useMonitorTargets();
  const incidents = useIncidents();

  const targetLabels = useMemo(() => new Map(targets?.map((t) => [t.id, t.label])), [targets]);
  // Newest start first, whatever the severity. A baseline incident was there when
  // monitoring started, and raised no alert: it is the list's alone.
  const openIncidents = useMemo(
    () =>
      incidents
        ?.filter((i) => i.open && !i.baseline)
        .toSorted((a, b) => Date.parse(b.lastStartedAt) - Date.parse(a.lastStartedAt)) ?? null,
    [incidents],
  );
  // The targets that would find nothing: off, or cannot be read.
  const unwatchedTargets = (targets ?? []).filter((t) => {
    const kind = targetStatus(t)?.kind;
    return kind === 'off' || kind === 'failed';
  });

  const showInDialog = (incidentID?: string) => {
    setOpen(false);
    openDialog('incidents', incidentID);
  };

  const count = openIncidents?.length ?? 0;
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        render={<AppBarButton className="relative" />}
        aria-label={
          count > 0 ? `Notifications, ${count} open ${count === 1 ? 'incident' : 'incidents'}` : 'Notifications'
        }
      >
        <Bell aria-hidden />
        {count > 0 && (
          <span className="absolute -top-0.5 -right-0.5 min-w-4 rounded-full bg-destructive px-1 text-[10px]/4 font-medium text-white">
            {count}
          </span>
        )}
      </PopoverTrigger>
      <PopoverContent
        align="end"
        sideOffset={8}
        aria-label="Open incidents"
        className="flex max-h-(--available-height) w-80 flex-col gap-2"
      >
        <p className="text-sm font-medium">Open incidents</p>
        {/* Scrolls on its own, so View all stays in a short window. */}
        <div className="-mx-2 flex max-h-80 min-h-0 flex-col gap-2 overflow-y-auto px-2">
          <OpenIncidents
            incidents={openIncidents}
            unwatchedTargets={unwatchedTargets}
            targetLabels={targetLabels}
            onShow={showInDialog}
            onLeave={() => setOpen(false)}
          />
        </div>
        <Button variant="ghost" size="sm" className="self-end" onClick={() => showInDialog()}>
          View all
        </Button>
      </PopoverContent>
    </Popover>
  );
}

// The bell's list: the open incidents, or with none the lines of the targets that
// would find nothing. `incidents` is null until the snapshots land.
function OpenIncidents({
  incidents,
  unwatchedTargets,
  targetLabels,
  onShow,
  onLeave,
}: {
  incidents: Incident[] | null;
  unwatchedTargets: MonitorTarget[];
  targetLabels: ReadonlyMap<string, string>;
  onShow: (incidentID: string) => void;
  onLeave: () => void;
}) {
  /* v8 ignore start -- the provider holds only the incidents of the targets it lists */
  const targetLabelOf = (targetID: string) => targetLabels.get(targetID) ?? '';
  /* v8 ignore stop */
  if (!incidents) return <p className="text-sm text-muted-foreground">Loading…</p>;
  if (incidents.length === 0) {
    return (
      <>
        <p className="text-sm text-muted-foreground">No open incidents.</p>
        {unwatchedTargets.map((t) => (
          <MonitorTargetStatus key={t.id} target={t} onOpenSettings={onLeave} />
        ))}
      </>
    );
  }
  return (
    <ul className="-mx-2 flex flex-col gap-0.5">
      {incidents.map((incident) => (
        <li key={incident.id}>
          <button type="button" onClick={() => onShow(incident.id)} className={LIST_ROW_CLASS}>
            <IncidentHeadline incident={incident} />
            <span className="truncate text-xs text-muted-foreground">
              <VisibleText text={targetLabelOf(incident.targetID)} />
            </span>
          </button>
        </li>
      ))}
    </ul>
  );
}
