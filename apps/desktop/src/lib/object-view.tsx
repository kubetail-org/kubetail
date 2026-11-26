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

// The focused object: the one cluster object the dashboard's right sidebar draws in place
// of its chat, on one of its tabs. It is chrome, like the focused log view: out of the URL,
// so Back and Forward leave it alone, and not persisted. `set` also opens the right
// sidebar, since an object focused with the panel closed is one nobody sees.
//
// An object belongs to the cluster it was focused in. However the window reaches another
// (the picker, Back, a new default context), the focus goes: its tabs would read the new
// cluster, calling the object deleted or showing a namesake's events.
import { createContext, useContext, useMemo, useState } from 'react';
import type { ReactNode } from 'react';

import { useActiveCluster } from '@/lib/active-cluster';
import { useRightSidebar } from '@/lib/right-sidebar';

export type FocusedObject = {
  apiVersion: string;
  resource: string;
  kind: string;
  uid: string;
  namespace: string;
  name: string;
};

export type ObjectTab = 'yaml' | 'events' | 'logs';

type ObjectViewContextValue = {
  object: FocusedObject | null;
  tab: ObjectTab;
  set: (object: FocusedObject, tab: ObjectTab) => void;
  setTab: (tab: ObjectTab) => void;
  clear: () => void;
};

const ObjectViewContext = createContext<ObjectViewContextValue | null>(null);

export function ObjectViewProvider({ children }: { children: ReactNode }) {
  const { clusterID } = useActiveCluster();
  const [focused, setFocused] = useState<{ object: FocusedObject; clusterID: string | undefined } | null>(null);
  const [tab, setTab] = useState<ObjectTab>('yaml');
  const { show } = useRightSidebar();
  // An undefined ID is no cluster resolved yet, not another one.
  if (focused && clusterID !== undefined && clusterID !== focused.clusterID) setFocused(null);
  const object = focused?.object ?? null;
  const value = useMemo<ObjectViewContextValue>(
    () => ({
      object,
      tab,
      set: (next, nextTab) => {
        setFocused({ object: next, clusterID });
        setTab(nextTab);
        show();
      },
      setTab,
      clear: () => setFocused(null),
    }),
    [object, tab, show, clusterID],
  );
  return <ObjectViewContext.Provider value={value}>{children}</ObjectViewContext.Provider>;
}

export function useObjectView(): ObjectViewContextValue {
  const ctx = useContext(ObjectViewContext);
  if (!ctx) throw new Error('useObjectView must be used within an ObjectViewProvider');
  return ctx;
}
