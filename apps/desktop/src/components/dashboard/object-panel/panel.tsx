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

// The focused object as the dashboard's right sidebar draws it in place of its chat: a
// header naming the object, with Close, over its YAML and Events tabs, and Logs for a kind
// a log view reads. Only the open tab is mounted, so only its watch or read runs.
import { Button } from '@kubetail/ui/elements/button';
import { Tabs, TabsList, TabsTrigger } from '@kubetail/ui/elements/tabs';
import { X } from 'lucide-react';
import type { ComponentType } from 'react';

import { VisibleText } from '@/components/primitives/visible-text';
import { logsSourceKind } from '@/lib/object-actions';
import type { FocusedObject, ObjectTab } from '@/lib/object-view';

import { ObjectEventsView } from './events-view';
import { ObjectLogsView } from './logs-view';
import { ObjectYamlView } from './yaml-view';

const VIEW: Record<ObjectTab, ComponentType<{ object: FocusedObject }>> = {
  yaml: ObjectYamlView,
  events: ObjectEventsView,
  logs: ObjectLogsView,
};

type ObjectPanelProps = {
  object: FocusedObject;
  tab: ObjectTab;
  onTab: (tab: ObjectTab) => void;
  onClose: () => void;
};

export function ObjectPanel({ object, tab, onTab, onClose }: ObjectPanelProps) {
  const View = VIEW[tab];
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <header className="flex items-start gap-2 p-4 pb-3">
        <div className="min-w-0 flex-1 text-sm">
          <h2 className="truncate font-semibold" title={object.name}>
            <VisibleText text={object.kind} />: <VisibleText text={object.name} />
          </h2>
          {object.namespace !== '' && (
            <p className="truncate text-xs text-muted-foreground" title={object.namespace}>
              <VisibleText text={object.namespace} />
            </p>
          )}
        </div>
        <Button variant="ghost" size="icon-sm" aria-label="Close object" title="Close" onClick={onClose}>
          <X aria-hidden />
        </Button>
      </header>
      <Tabs value={tab} onValueChange={(value) => onTab(value as ObjectTab)} className="border-b px-4 pb-3">
        <TabsList aria-label="Object views">
          <TabsTrigger value="yaml">YAML</TabsTrigger>
          <TabsTrigger value="events">Events</TabsTrigger>
          {logsSourceKind(object) && <TabsTrigger value="logs">Logs</TabsTrigger>}
        </TabsList>
      </Tabs>
      <View object={object} />
    </div>
  );
}
