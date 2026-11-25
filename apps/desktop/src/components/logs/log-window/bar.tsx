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

// A log window's title bar: the cluster's dot and name and the chat's title, then
// *Show chat*. It has no menu, omnibox or history. macOS keeps its traffic lights,
// which `window_manager.rs` centres in this bar's 40px; the frameless platforms get
// `WindowControls` and no `AppMenu`, so `Ctrl+N`/`Ctrl+Q` do nothing here.
import { Button } from '@kubetail/ui/elements/button';
import { invoke } from '@tauri-apps/api/core';

import { ClusterDot } from '@/components/cluster/cluster-dot';
import { TrafficLightGutter } from '@/components/primitives/traffic-light-gutter';
import { VisibleText } from '@/components/primitives/visible-text';
import { WindowControls } from '@/components/primitives/window-controls';
import { clusterHealth } from '@/lib/cluster-health';
import { displayName } from '@/lib/clusters';
import type { Cluster } from '@/lib/clusters';
import { reportHostError } from '@/lib/error-bus';
import { isMacOS } from '@/lib/platform';

function showChat() {
  invoke('log_window_show_chat').catch(reportHostError);
}

function ClusterName({ cluster }: { cluster: Cluster }) {
  const health = clusterHealth(cluster);
  return (
    <>
      <ClusterDot tone={health.tone} title={health.summary} />
      <span data-testid="logs-window-cluster" className="shrink-0 font-medium">
        <VisibleText text={displayName(cluster)} />
      </span>
    </>
  );
}

export function LogsWindowBar({ cluster, chatTitle }: { cluster: Cluster | undefined; chatTitle: string }) {
  const mac = isMacOS();
  return (
    // `deep`, as the app bar is: every pixel drags, and the button still clicks.
    <div
      data-testid="logs-window-bar"
      data-tauri-drag-region="deep"
      className="flex h-10 shrink-0 transform-gpu items-stretch bg-app-bar"
    >
      {mac && <TrafficLightGutter />}
      <div className="ml-4 flex min-w-0 flex-1 items-center gap-2 text-sm">
        {cluster && <ClusterName cluster={cluster} />}
        <span data-testid="logs-window-chat" className="truncate text-muted-foreground">
          <VisibleText text={chatTitle} />
        </span>
      </div>
      <div className="mr-3 flex items-center">
        <Button variant="ghost" size="sm" data-tauri-drag-region="false" onClick={showChat}>
          Show chat
        </Button>
      </div>
      {!mac && <WindowControls />}
    </div>
  );
}
