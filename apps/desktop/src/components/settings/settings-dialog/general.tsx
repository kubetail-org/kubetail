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

// The Settings dialog's General tab.
import { ColorSchemePicker } from './color-scheme-picker';
import { Group } from './group';

export function GeneralSettings() {
  return (
    <Group title="Appearance">
      {/* The row stacks while the card is too narrow for the text beside the
          picker's 264px, as at the window's minimum width. */}
      <div className="@container rounded-lg border">
        <div className="flex flex-col items-start gap-3 p-4 @lg:flex-row @lg:items-center">
          <div className="flex min-w-0 flex-col gap-0.5 @lg:flex-1">
            <span className="text-sm text-popover-foreground">Mode</span>
            <span className="text-xs text-muted-foreground">Follow your system, or pick one.</span>
          </div>
          <ColorSchemePicker />
        </div>
      </div>
    </Group>
  );
}
