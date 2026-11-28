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

// The filter's incidents, in the order useIncidents returns them. A row's target and
// subject labels are text a target returned; the signal's label is the sidecar's own.
import { cn } from '@kubetail/ui/lib/utils';

import { IncidentHeadline } from '@/components/monitoring/incident-headline';
import { VisibleText } from '@/components/primitives/visible-text';
import { TONE, countLabel } from '@/lib/cluster-health';
import { LIST_ROW_CLASS } from '@/lib/control-style';
import { INTEGRATION_LABEL } from '@/lib/incidents';
import type { Incident, MonitorTarget } from '@/lib/incidents';

import { IncidentState } from './state';

export function IncidentList({
  incidents,
  targets,
  selectedID,
  onSelect,
}: {
  incidents: Incident[];
  targets: ReadonlyMap<string, MonitorTarget>;
  selectedID: string | null;
  onSelect: (incidentID: string) => void;
}) {
  return (
    <ul aria-label="Incidents" className="flex flex-col gap-0.5">
      {incidents.map((incident) => {
        const target = targets.get(incident.targetID);
        /* v8 ignore next -- the provider holds only the incidents of the targets it lists */
        if (!target) return null;
        return (
          <li key={incident.id}>
            <button
              type="button"
              aria-pressed={incident.id === selectedID}
              onClick={() => onSelect(incident.id)}
              className={cn(LIST_ROW_CLASS, 'aria-pressed:bg-accent')}
            >
              <IncidentSummary incident={incident} target={target} />
            </button>
          </li>
        );
      })}
    </ul>
  );
}

// The facts a row and the detail's header both show.
export function IncidentSummary({ incident, target }: { incident: Incident; target: MonitorTarget }) {
  return (
    <>
      <IncidentHeadline
        incident={incident}
        trailing={
          <span className="text-xs text-muted-foreground">{countLabel(incident.occurrenceCount, 'occurrence')}</span>
        }
      />
      <span className="truncate text-xs text-muted-foreground">
        <VisibleText text={target.label} /> · {INTEGRATION_LABEL[target.integration]}
      </span>
      <span className="text-xs text-muted-foreground">
        <IncidentState incident={incident} />
      </span>
      {incident.baseline && <span className="text-xs text-muted-foreground">Found when monitoring started</span>}
      {incident.triage.state === 'Done' && incident.triage.verdict === 'Dismiss' && (
        <span className="text-xs text-muted-foreground">Dismissed by triage</span>
      )}
      {target.currentUID !== null && incident.targetUID !== target.currentUID && (
        <span className={cn('text-xs', TONE.attention.text)}>On a server this target no longer reaches</span>
      )}
    </>
  );
}
