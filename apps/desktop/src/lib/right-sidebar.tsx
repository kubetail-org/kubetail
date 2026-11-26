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

// Open/closed state for the right sidebar, shared by the panel and its toggle — the
// two are in different branches of the layout, so neither can own it. It is chrome,
// not what the window is looking at, so it stays out of the URL (see
// docs/adr/2026-08-09-url-params-as-window-state.md) and persists app-wide beside the
// panel's width: a new window opens looking like the last one left it.
//
// Kept per mode, since the panel's contents are: opening the dashboard's details
// says nothing about wanting the chat's. The layout supplies the mode rather than
// this reading the route, so the panel and its toggle cannot disagree about which
// one they are answering for.
import type { ReactNode } from 'react';
import { createContext, useContext, useMemo } from 'react';

import type { AppMode } from '@/lib/app-mode';
import { usePersistedFlag } from '@/lib/persisted-flag';

type RightSidebarContextValue = {
  mode: AppMode;
  open: boolean;
  toggle: () => void;
  show: () => void;
  close: () => void;
};

const RightSidebarContext = createContext<RightSidebarContextValue | null>(null);

const openKey = (mode: AppMode) => `right-sidebar-open:${mode}`;

export function RightSidebarProvider({ mode, children }: { mode: AppMode; children: ReactNode }) {
  const [open, setOpen] = usePersistedFlag(openKey(mode), false);
  const value = useMemo<RightSidebarContextValue>(
    () => ({
      mode,
      open,
      toggle: () => setOpen(!open),
      show: () => setOpen(true),
      close: () => setOpen(false),
    }),
    [mode, open, setOpen],
  );
  return <RightSidebarContext.Provider value={value}>{children}</RightSidebarContext.Provider>;
}

export function useRightSidebar(): RightSidebarContextValue {
  const ctx = useContext(RightSidebarContext);
  if (!ctx) throw new Error('useRightSidebar must be used within a RightSidebarProvider');
  return ctx;
}
