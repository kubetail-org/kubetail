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

// What rides a send about the focused log view: the call that opened it, never its
// action, which the sidecar reads from the call's row, and what the viewer shows,
// the grep the user set from its toolbar included.
import type { LogsViewSnapshotInput } from '@/gql/graphql';
import type { LogViewport } from '@/lib/log-viewport';
import type { LogsView } from '@/lib/logs-view';

/** The most selected lines a snapshot carries. The sidecar refuses more. */
export const MAX_SELECTED = 50;

export function snapshotOf(view: Pick<LogsView, 'callId'>, viewport: LogViewport): LogsViewSnapshotInput {
  return {
    callID: view.callId,
    from: viewport.range?.from ?? null,
    to: viewport.range?.to ?? null,
    lines: viewport.visible,
    atEnd: viewport.atEnd,
    pinnedToEnd: viewport.pinnedToEnd,
    paused: viewport.paused,
    grep: viewport.grep,
    selected: viewport.selection.slice(0, MAX_SELECTED).map((line) => ({
      timestamp: line.timestamp,
      namespace: line.stream.namespace,
      pod: line.stream.pod,
      container: line.stream.container,
      previous: line.stream.previous,
      message: line.message,
    })),
    selectedCount: viewport.selectedCount,
  };
}
