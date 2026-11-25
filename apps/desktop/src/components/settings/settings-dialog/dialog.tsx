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

// The app's settings dialog: a sidebar of tabs beside the selected tab's pane.
// Rendered by `AppDialogs`, not inline in the account menu that opens it, so it
// outlives the sidebar card unmounting on auto-collapse. Settings persist to the
// host's host.json; see docs/adr/2026-08-09-host-json-settings.md
import { Tabs } from '@base-ui/react/tabs';
import { Button } from '@kubetail/ui/elements/button';
import {
  Brain,
  Container,
  Plug,
  ScanEye,
  Server,
  ShieldCheck,
  SlidersHorizontal,
  Sparkles,
  SquareTerminal,
  Wrench,
  X,
  type LucideIcon,
} from 'lucide-react';
import { type ComponentType, useState } from 'react';

import { DialogClose, DialogTitle, PanelDialog } from '@/components/primitives/dialog';
import { type AppDialogProps } from '@/lib/dialog';
import { SelectSettingsTab } from '@/lib/settings-tab';

import { ClustersSettings } from './clusters';
import { ConnectorsSettings } from './connectors';
import { EnvironmentSettings } from './environment';
import { GeneralSettings } from './general';
import { MemorySettings } from './memory';
import { MonitoringSettings } from './monitoring';
import { PermissionSettings } from './permissions';
import { ProvidersSettings } from './providers';
import { SandboxSettings } from './sandbox';
import { ToolsSettings } from './tools';

// A hidden tab's pane unmounts, so its watches run only while it is selected.
const TABS: { label: string; icon: LucideIcon; content: ComponentType }[] = [
  { label: 'General', icon: SlidersHorizontal, content: GeneralSettings },
  { label: 'Clusters', icon: Server, content: ClustersSettings },
  { label: 'Connectors', icon: Plug, content: ConnectorsSettings },
  { label: 'Providers', icon: Sparkles, content: ProvidersSettings },
  { label: 'Memory', icon: Brain, content: MemorySettings },
  { label: 'Environment', icon: SquareTerminal, content: EnvironmentSettings },
  { label: 'Tools', icon: Wrench, content: ToolsSettings },
  { label: 'Sandbox', icon: Container, content: SandboxSettings },
  { label: 'Permissions', icon: ShieldCheck, content: PermissionSettings },
  { label: 'Monitoring', icon: ScanEye, content: MonitoringSettings },
];

export function SettingsDialog({ open, onOpenChange }: AppDialogProps) {
  return (
    <PanelDialog
      open={open}
      onOpenChange={onOpenChange}
      className="h-160 w-220 bg-popover shadow-2xl sm:max-w-[calc(100%-2rem)]"
    >
      <SettingsTabs />
    </PanelDialog>
  );
}

// Inside the dialog's content, so each opening starts on the first tab.
function SettingsTabs() {
  const [tab, setTab] = useState(TABS[0].label);
  return (
    <SelectSettingsTab value={setTab}>
      <Tabs.Root orientation="vertical" value={tab} onValueChange={setTab} className="flex min-w-0 flex-1">
        <div className="flex w-54 shrink-0 flex-col gap-0.5 border-r bg-muted px-3 py-4">
          <DialogTitle className="px-2 pt-1 pb-3 text-sm font-medium text-muted-foreground">Settings</DialogTitle>
          <Tabs.List activateOnFocus className="flex flex-col gap-0.5">
            {TABS.map(({ label, icon: Icon }) => (
              <Tabs.Tab
                key={label}
                value={label}
                className="flex items-center gap-2 rounded-md px-2 py-1.5 text-sm text-popover-foreground outline-none hover:bg-accent/50 focus-visible:ring-[3px] focus-visible:ring-ring/50 aria-selected:bg-accent aria-selected:font-medium aria-selected:text-foreground"
              >
                <Icon className="size-4" />
                {label}
              </Tabs.Tab>
            ))}
          </Tabs.List>
        </div>
        {TABS.map(({ label, content: Content }) => (
          <Tabs.Panel key={label} value={label} className="flex min-w-0 flex-1 flex-col outline-none">
            <div className="flex items-center gap-3 pt-4 pr-4 pb-3 pl-6">
              <h3 className="flex-1 text-base font-medium text-popover-foreground">{label}</h3>
              <DialogClose
                aria-label="Close"
                render={<Button variant="ghost" size="icon" className="size-7 rounded-md" />}
              >
                <X className="size-4" />
              </DialogClose>
            </div>
            <div className="flex min-h-0 flex-1 flex-col gap-6 overflow-y-auto px-6 pt-2 pb-6">
              <Content />
            </div>
          </Tabs.Panel>
        ))}
      </Tabs.Root>
    </SelectSettingsTab>
  );
}
