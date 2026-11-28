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

// A pod's columns: kubectl's, its status read as kubectl's `printPod` reads it, and more of
// its fields hidden until picked.
import { ownerOf } from '@/lib/object-owner';
import { podReady, podRestarts } from '@/lib/pod-detail';
import { podStatus } from '@/lib/pod-status';
import type { PodBody } from '@/lib/pod-status';

import { ageColumn, countColumn, DASH, kind, listColumn, readyColumn, statusColumn, textColumn } from './common';

// The mockup's order: Status, Ready, Restarts, Node.
export const podKinds = [
  kind<PodBody>('v1', 'pods', [
    statusColumn('Status', 136, podStatus),
    readyColumn('Ready', podReady),
    {
      ...countColumn('Restarts', 80, podRestarts),
      cell: (b) => {
        const n = podRestarts(b);
        if (n === undefined) return DASH;
        return <span className={n > 0 ? 'font-medium text-destructive' : 'text-muted-foreground'}>{n}</span>;
      },
    },
    textColumn('Node', 128, (b) => b.spec?.nodeName, { mono: true }),
    // Hidden until picked: kubectl's `-o wide` columns and more of the pod's own fields.
    textColumn('IP', 120, (b) => b.status?.podIP, { hidden: true, mono: true }),
    textColumn('Host IP', 120, (b) => b.status?.hostIP, { hidden: true, mono: true }),
    listColumn('Containers', 160, (b) => b.spec?.containers?.map((c) => c.name), { hidden: true }),
    listColumn('Images', 224, (b) => b.spec?.containers?.map((c) => c.image), { hidden: true, mono: true }),
    textColumn('QoS class', 104, (b) => b.status?.qosClass, { hidden: true }),
    textColumn('Service account', 160, (b) => b.spec?.serviceAccountName, { hidden: true }),
    textColumn('Priority class', 144, (b) => b.spec?.priorityClassName, { hidden: true }),
    textColumn('Controlled by', 176, ownerOf, { hidden: true }),
    ageColumn('Started', 72, (b) => b.status?.startTime, { hidden: true }),
  ]),
];
