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

// App-level dialog controller. Triggers live in the app bar; the render lives in
// `AppDialogs`, mounted beside the whole window shell — a dialog rendered inside a
// control's own chrome would vanish with it when the window narrows.
// `mountedDialog` trails `activeDialog` through the exit animation: closing clears
// only `activeDialog`; the shared `Dialog` wrapper calls `notifyClosed` once the
// close settles, which unmounts. One dialog open at a time. An open may name
// what its dialog opens on, which the dialog reads once as it mounts.
import type { ReactNode } from 'react';
import { createContext, useCallback, useContext, useMemo, useState } from 'react';

// Add a dialog by extending this union and rendering it in the `AppDialogs` host.
export type DialogId = 'clusters' | 'incidents' | 'settings';

// The Clusters dialog row, and which of its panes, to open on.
export type ClusterDetail = { clusterId: string; pane: 'connection' | 'sync' };

// What each dialog can be opened on: a Clusters row, an incident's id, a Settings tab's label.
type DialogDetail = { clusters: ClusterDetail; incidents: string; settings: string };

type OpenDialog = <Id extends DialogId>(id: Id, detail?: DialogDetail[Id]) => void;

// The last open, its detail typed by its dialog, and its number among the opens, from 1.
type Opened = { [Id in DialogId]: { id: Id; detail?: DialogDetail[Id]; seq: number } }[DialogId];

type DialogContextValue = {
  activeDialog: DialogId | null;
  // Trails `activeDialog` through the exit animation.
  mountedDialog: DialogId | null;
  openDialog: OpenDialog;
  // What the last open named, each null when it named nothing or opened another dialog.
  clusterDetail: ClusterDetail | null;
  incidentID: string | null;
  settingsTab: string | null;
  // Moves on every open, even of the dialog already open on the same detail, so a
  // dialog keyed on it chooses its detail afresh.
  openSeq: number;
  closeDialog: () => void;
  // Close transition settled → unmount, unless a dialog is open. Called by the
  // shared `Dialog` wrapper only.
  notifyClosed: () => void;
};

// Controlled props every overlay dialog forwards to the shared `Dialog` wrapper;
// close-completion is handled by the wrapper via context.
export type AppDialogProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
};

const DialogContext = createContext<DialogContextValue | null>(null);

export function DialogProvider({ children }: { children: ReactNode }) {
  const [activeDialog, setActiveDialog] = useState<DialogId | null>(null);
  const [mountedDialog, setMountedDialog] = useState<DialogId | null>(null);
  const [opened, setOpened] = useState<Opened | null>(null);
  // Stable, so a caller may open from an effect without the open re-running it.
  const openDialog = useCallback<OpenDialog>((id, detail) => {
    setOpened((last) => ({ id, detail, seq: (last?.seq ?? 0) + 1 }) as Opened);
    setMountedDialog(id);
    setActiveDialog(id);
  }, []);
  const closeDialog = useCallback(() => setActiveDialog(null), []);
  const value = useMemo<DialogContextValue>(
    () => ({
      activeDialog,
      mountedDialog,
      clusterDetail: opened?.id === 'clusters' ? (opened.detail ?? null) : null,
      incidentID: opened?.id === 'incidents' ? (opened.detail ?? null) : null,
      settingsTab: opened?.id === 'settings' ? (opened.detail ?? null) : null,
      openSeq: opened?.seq ?? 0,
      openDialog,
      closeDialog,
      // A dialog still open means what closed sat inside it, or was replaced by
      // it: either way the mounted one stays.
      notifyClosed: () => {
        if (activeDialog === null) setMountedDialog(null);
      },
    }),
    [activeDialog, mountedDialog, opened, openDialog, closeDialog],
  );
  return <DialogContext.Provider value={value}>{children}</DialogContext.Provider>;
}

export function useDialog(): DialogContextValue {
  const ctx = useContext(DialogContext);
  if (!ctx) throw new Error('useDialog must be used within a DialogProvider');
  return ctx;
}

// Non-throwing read for the shared `Dialog` wrapper — a `Dialog` outside the
// provider still works, it just has no host to notify.
export function useDialogHost(): DialogContextValue | null {
  return useContext(DialogContext);
}
