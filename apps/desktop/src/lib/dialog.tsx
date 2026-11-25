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
// close settles, which unmounts. One dialog open at a time.
import type { ReactNode } from 'react';
import { createContext, useContext, useMemo, useState } from 'react';

// Add a dialog by extending this union and rendering it in the `AppDialogs` host.
export type DialogId = 'clusters' | 'settings';

// The Clusters dialog row, and which of its panes, to open on.
export type ClusterDetail = { clusterId: string; pane: 'connection' | 'sync' };

type DialogContextValue = {
  activeDialog: DialogId | null;
  // Trails `activeDialog` through the exit animation.
  mountedDialog: DialogId | null;
  openDialog: (id: DialogId, clusterDetail?: ClusterDetail) => void;
  // The last open's; null when it named none. Read once, as the Clusters dialog mounts.
  clusterDetail: ClusterDetail | null;
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
  const [clusterDetail, setClusterDetail] = useState<ClusterDetail | null>(null);
  const value = useMemo<DialogContextValue>(
    () => ({
      activeDialog,
      mountedDialog,
      clusterDetail,
      openDialog: (id, detail) => {
        setClusterDetail(detail ?? null);
        setMountedDialog(id);
        setActiveDialog(id);
      },
      closeDialog: () => setActiveDialog(null),
      // A dialog still open means what closed sat inside it, or was replaced by
      // it: either way the mounted one stays.
      notifyClosed: () => {
        if (activeDialog === null) setMountedDialog(null);
      },
    }),
    [activeDialog, mountedDialog, clusterDetail],
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
