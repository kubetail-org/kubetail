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

// The actions on an object, each opening the object panel on its tab. YAML and Events are
// every kind's; Logs is a kind's whose objects a log view can follow.
import type { LogsViewSourceKind } from '@/gql/graphql';
import { gvrKey } from '@/lib/gvr';
import type { GVR } from '@/lib/gvr';
import type { ObjectTab } from '@/lib/object-view';

export type ObjectAction = ObjectTab;

// The kinds a log view reads, by gvrKey: matched on API group and plural, so a CRD reusing
// a built-in's kind name is not one.
const LOGS_KINDS = new Map<string, LogsViewSourceKind>([
  ['v1/pods', 'Pod'],
  ['apps/v1/deployments', 'Deployment'],
  ['apps/v1/statefulsets', 'StatefulSet'],
  ['apps/v1/daemonsets', 'DaemonSet'],
  ['apps/v1/replicasets', 'ReplicaSet'],
  ['batch/v1/jobs', 'Job'],
  ['batch/v1/cronjobs', 'CronJob'],
]);

export function logsSourceKind(gvr: GVR): LogsViewSourceKind | undefined {
  return LOGS_KINDS.get(gvrKey(gvr));
}

export function actionsForKind(gvr: GVR): ObjectAction[] {
  return logsSourceKind(gvr) ? ['yaml', 'events', 'logs'] : ['yaml', 'events'];
}
