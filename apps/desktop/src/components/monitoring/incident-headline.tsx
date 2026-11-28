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

// An incident's first two lines, in the bell and the Incidents dialog alike: its
// severity and signal, then its subject. The subject's label is a target's text.
import type { ReactNode } from 'react';

import { ClusterDot } from '@/components/cluster/cluster-dot';
import { VisibleText } from '@/components/primitives/visible-text';
import { SEVERITY_TONE } from '@/lib/incidents';
import type { Incident } from '@/lib/incidents';

export function IncidentHeadline({ incident, trailing }: { incident: Incident; trailing?: ReactNode }) {
  return (
    <>
      <span className="flex items-center gap-2">
        <ClusterDot tone={SEVERITY_TONE[incident.severity]} />
        <span className="flex-1 truncate font-medium">{incident.signalLabel}</span>
        {trailing}
      </span>
      <span className="truncate">
        <VisibleText text={incident.subjectLabel} />
      </span>
    </>
  );
}
