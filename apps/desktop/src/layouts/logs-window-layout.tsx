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

// A log window's layout, mounted by the pathless `_logs` route: one column the
// window's height bounds, with no sidebars and no dialogs host. The routed page draws
// its own bar, since the bar names what the page reads. The column's height is
// definite and every plain flex item under it takes `min-h-0`, so the viewer scrolls
// and the window never does.
import { Outlet } from '@tanstack/react-router';

export function LogsWindowLayout() {
  return (
    <div data-testid="logs-window-layout" className="flex h-(--app-min-h) min-h-0 flex-col bg-background">
      <Outlet />
    </div>
  );
}
