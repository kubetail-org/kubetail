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

// The object panel's Logs tab: the object's logs from the newest line, following, in the
// log viewer the chat's views use. A workload reads the pods it runs; every container is
// read, each line naming its own.
import { useState } from 'react';

import { LogViewer } from '@/components/logs/log-viewer';
import { useActiveCluster } from '@/lib/active-cluster';
import { createLogViewportHandle } from '@/lib/log-viewport';
import type { LogsViewAction } from '@/lib/logs-view';
import { logsSourceKind } from '@/lib/object-actions';
import type { FocusedObject } from '@/lib/object-view';

const TAIL: LogsViewAction['anchor'] = { kind: 'Tail', at: null };

// The viewer keeps the grep its toolbar applies for as long as it is mounted; nothing
// outside the tab reads it.
const ignoreGrep = () => {};

export function ObjectLogsView({ object }: { object: FocusedObject }) {
  const { clusterID } = useActiveCluster();
  // Nothing reads it yet: the dashboard's chat sends no snapshot of a view.
  const [viewport] = useState(createLogViewportHandle);
  const kind = logsSourceKind(object);

  /* v8 ignore next -- the panel offers the tab only for a log kind, under a known cluster */
  if (!clusterID || !kind) return null;
  const query: LogsViewAction = {
    sources: [
      { namespace: object.namespace, kind, name: object.name, containers: [], allContainers: true, previous: false },
    ],
    filters: [],
    grep: '',
    anchor: TAIL,
    pinToEnd: true,
    resolvedAt: '',
  };
  return (
    <LogViewer
      clusterId={clusterID}
      query={query}
      openAt={TAIL}
      pinToEnd
      viewport={viewport}
      onGrepApplied={ignoreGrep}
    />
  );
}
