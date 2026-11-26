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

// The Tools pane: every tool a chat can be offered, grouped by what it does,
// each with the user's switch. A tool's row leaves room under it for that
// tool's own settings.
import { Switch } from '@kubetail/ui/elements/switch';
import { type ReactNode } from 'react';

import type { ToolGroup } from '@/gql/graphql';
import { type ToolSetting, useToolSettings } from '@/lib/tool-settings';

import { Group } from './group';

// Each group in the order the pane draws them.
const GROUPS: { group: ToolGroup; title: string }[] = [
  { group: 'Commands', title: 'Commands' },
  { group: 'Files', title: 'Files' },
  { group: 'Web', title: 'Web' },
  { group: 'Cluster', title: 'Cluster' },
  { group: 'Logs', title: 'Logs' },
  { group: 'Memory', title: 'Memory' },
  { group: 'Agents', title: 'Agents' },
];

export function ToolsSettings() {
  const { tools, changing, error, set } = useToolSettings();
  return (
    <>
      <p className="text-xs text-muted-foreground">
        What the model can use, in chats, their agents and monitor runs. A change applies from each chat&apos;s next
        turn.
      </p>
      {error && (
        <p role="alert" className="text-xs text-destructive">
          {error}
        </p>
      )}
      {tools &&
        GROUPS.map(({ group, title }) => {
          const rows = tools.filter((tool) => tool.group === group);
          if (rows.length === 0) return null;
          return (
            <Group key={group} title={title}>
              <ul className="divide-y rounded-lg border">
                {rows.map((tool) => (
                  <ToolRow
                    key={tool.name}
                    tool={tool}
                    busy={changing.has(tool.name)}
                    onSwitch={(enabled) => set(tool.name, enabled)}
                  />
                ))}
              </ul>
            </Group>
          );
        })}
    </>
  );
}

function ToolRow({
  tool,
  busy,
  onSwitch,
  children,
}: {
  tool: ToolSetting;
  busy: boolean;
  onSwitch: (enabled: boolean) => void;
  /** The tool's own settings, drawn under its switch. */
  children?: ReactNode;
}) {
  return (
    <li className="flex flex-col gap-3 px-4 py-3">
      <div className="flex items-center gap-4">
        <div className="flex min-w-0 flex-1 flex-col gap-0.5">
          <span className="font-mono text-sm text-popover-foreground">{tool.label}</span>
          <span className="text-xs text-muted-foreground">{tool.description}</span>
        </div>
        <Switch
          aria-label={tool.label}
          checked={tool.enabled}
          disabled={busy || tool.alwaysOn}
          onCheckedChange={onSwitch}
        />
      </div>
      {children}
    </li>
  );
}
