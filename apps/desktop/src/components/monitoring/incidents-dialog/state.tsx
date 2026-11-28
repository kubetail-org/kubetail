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

// An incident's state with its time, ticking: *Seen since*, *Resolved, last seen* or *Last seen*.
import { RelativeTime } from '@/components/primitives/relative-time';
import { humanDuration } from '@/lib/human-duration';
import { incidentState } from '@/lib/incidents';
import type { Incident } from '@/lib/incidents';

export function IncidentState({ incident }: { incident: Incident }) {
  const { label, at } = incidentState(incident);
  return <RelativeTime ms={Date.parse(at)} format={(ms) => `${label} ${humanDuration(ms)} ago`} />;
}
