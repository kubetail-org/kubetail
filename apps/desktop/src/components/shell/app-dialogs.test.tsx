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

import { cleanup, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { useEffect } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { DialogProvider, useDialog, type AppDialogProps } from '@/lib/dialog';

// Stub the real panel (it needs the GraphQL/clusters provider stack) with a probe
// that reflects its controlled `open` prop and can request a close. The real
// panel's `Dialog` reports close-completion once the exit animation settles; stand
// in for that by calling `notifyClosed` when closed, exercising the host's
// exit-then-unmount lifecycle.
vi.mock('@/components/cluster/cluster-sync-panel/panel', () => ({
  ClusterSyncPanel: ({ open, onOpenChange }: AppDialogProps) => {
    const { notifyClosed } = useDialog();
    useEffect(() => {
      if (!open) notifyClosed();
    }, [open, notifyClosed]);
    return (
      <div data-testid="clusters" data-open={open}>
        <button type="button" onClick={() => onOpenChange(false)}>
          close-clusters
        </button>
      </div>
    );
  },
}));

const { AppDialogs } = await import('./app-dialogs');

// A trigger that opens the clusters dialog through the controller — standing in
// for the account menu, which lives in the (separately mounted) sidebar.
function OpenClusters() {
  const { openDialog } = useDialog();
  return (
    <button type="button" onClick={() => openDialog('clusters')}>
      open-clusters
    </button>
  );
}

function renderHost() {
  return render(
    <DialogProvider>
      <OpenClusters />
      <AppDialogs />
    </DialogProvider>,
  );
}

describe('AppDialogs', () => {
  afterEach(cleanup);

  it('lazily mounts the clusters dialog on request and unmounts it on dismiss', async () => {
    const user = userEvent.setup();
    renderHost();

    // Not mounted until requested.
    expect(screen.queryByTestId('clusters')).toBeNull();

    await user.click(screen.getByRole('button', { name: 'open-clusters' }));
    expect(screen.getByTestId('clusters')).toHaveAttribute('data-open', 'true');

    // The dialog's own close request (Escape / backdrop / close button) drives it
    // shut and, once the exit transition completes, unmounts it entirely.
    await user.click(screen.getByRole('button', { name: 'close-clusters' }));
    expect(screen.queryByTestId('clusters')).toBeNull();
  });
});
