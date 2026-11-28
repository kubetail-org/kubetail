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

// One incident: the facts its row shows, its subject's attributes, its triage, and
// its occurrences from the feed, which is subscribed while the detail is mounted.
import { humanDuration } from '@/lib/human-duration';
import { END_REASON_TEXT, MAX_OCCURRENCES, useIncidentOccurrenceEventFeed } from '@/lib/incidents';
import type { Incident, IncidentOccurrence, MonitorTarget } from '@/lib/incidents';

import { Attributes } from './attributes';
import { IncidentSummary } from './list';
import { IncidentTriageSection } from './triage';

export function IncidentDetail({ incident, target }: { incident: Incident; target: MonitorTarget }) {
  const { occurrences, snapshotComplete } = useIncidentOccurrenceEventFeed(incident.id);
  return (
    <section aria-label="Incident" className="flex flex-col gap-4 text-sm">
      <div className="flex flex-col gap-0.5">
        <IncidentSummary incident={incident} target={target} />
      </div>
      <Attributes attributes={incident.subjectAttributes} />
      <IncidentTriageSection incident={incident} target={target} />
      <div className="flex flex-col gap-2">
        <h4 className="text-xs font-medium text-muted-foreground">Occurrences</h4>
        {snapshotComplete ? (
          <>
            {incident.occurrenceCount > MAX_OCCURRENCES && (
              <p className="text-xs text-muted-foreground">
                Showing the newest {MAX_OCCURRENCES} of {incident.occurrenceCount} occurrences.
              </p>
            )}
            <ul aria-label="Occurrences" className="flex flex-col gap-2">
              {occurrences.map((occurrence) => (
                <Occurrence key={occurrence.id} occurrence={occurrence} />
              ))}
            </ul>
          </>
        ) : (
          <p className="text-xs text-muted-foreground">Loading occurrences…</p>
        )}
      </div>
    </section>
  );
}

function Occurrence({ occurrence }: { occurrence: IncidentOccurrence }) {
  const { number, startedAt, detail } = occurrence;
  return (
    <li className="flex flex-col gap-1 rounded-md border px-3 py-2">
      <span className="flex flex-wrap gap-x-2 text-xs">
        <span className="font-medium">#{number}</span>
        <span className="text-muted-foreground">{new Date(startedAt).toLocaleString()}</span>
        <span className="ml-auto">{occurrenceEnd(occurrence)}</span>
      </span>
      <Attributes attributes={detail} />
    </li>
  );
}

// How long an occurrence lasted and why it ended, or that it has not.
function occurrenceEnd({ startedAt, endedAt, endReason }: IncidentOccurrence): string {
  if (!endedAt) return 'ongoing';
  const lasted = humanDuration(Date.parse(endedAt) - Date.parse(startedAt));
  return endReason ? `${lasted}, ${END_REASON_TEXT[endReason]}` : lasted;
}
