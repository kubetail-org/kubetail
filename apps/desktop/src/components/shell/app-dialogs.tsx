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

// Render site for the app's overlay dialogs. Must stay mounted beside the window
// shell in AppLayout, so an open dialog outlives whatever chrome asked for it.
// Only the mounted dialog renders, so a closed dialog's hooks and
// subscriptions cost nothing; the mount lifecycle lives in `DialogProvider`.
// Register a dialog by adding a `DIALOGS` line (and a `DialogId` variant).
import type { ComponentType } from 'react';

import { ClusterSyncPanel } from '@/components/cluster/cluster-sync-panel';
import { SettingsDialog } from '@/components/settings/settings-dialog';
import { useDialog, type AppDialogProps, type DialogId } from '@/lib/dialog';

// Partial: a `DialogId` may be reserved before its dialog is built.
const DIALOGS: Partial<Record<DialogId, ComponentType<AppDialogProps>>> = {
  clusters: ClusterSyncPanel,
  settings: SettingsDialog,
};

export function AppDialogs() {
  const { activeDialog, mountedDialog, closeDialog } = useDialog();
  const Mounted = mountedDialog ? DIALOGS[mountedDialog] : undefined;
  if (!Mounted) return null;

  return (
    <Mounted
      // False once dismissed, driving the exit animation before the provider
      // unmounts it (via `notifyClosed`).
      open={activeDialog === mountedDialog}
      onOpenChange={(open) => {
        if (!open) closeDialog();
      }}
    />
  );
}
