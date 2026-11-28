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

import { act, renderHook } from '@testing-library/react';
import type { ReactNode } from 'react';
import { describe, expect, it } from 'vitest';

import { DialogProvider, useDialog, useDialogHost } from './dialog';

// Helpers -------------------------------------------------------------

const wrapper = ({ children }: { children: ReactNode }) => <DialogProvider>{children}</DialogProvider>;

const renderDialog = () => renderHook(() => useDialog(), { wrapper });

// Tests ---------------------------------------------------------------

describe('useDialog', () => {
  it('opens nothing to begin with', () => {
    const { result } = renderDialog();
    expect(result.current.activeDialog).toBeNull();
    expect(result.current.mountedDialog).toBeNull();
  });

  it('mounts a dialog and marks it open', () => {
    const { result } = renderDialog();
    act(() => result.current.openDialog('settings'));
    expect(result.current.activeDialog).toBe('settings');
    expect(result.current.mountedDialog).toBe('settings');
  });

  it('keeps the dialog mounted through its exit animation', () => {
    const { result } = renderDialog();
    act(() => result.current.openDialog('clusters'));

    // Closing only ends the open state; unmounting here would cut the animation off.
    act(() => result.current.closeDialog());
    expect(result.current.activeDialog).toBeNull();
    expect(result.current.mountedDialog).toBe('clusters');

    act(() => result.current.notifyClosed());
    expect(result.current.mountedDialog).toBeNull();
  });

  // A confirm inside an open dialog closes through the same wrapper; it must not
  // take the dialog it sits in down with it.
  it('keeps an open dialog mounted when a dialog inside it closes', () => {
    const { result } = renderDialog();
    act(() => result.current.openDialog('settings'));

    act(() => result.current.notifyClosed());
    expect(result.current.mountedDialog).toBe('settings');
  });

  it('swaps straight to another dialog, one open at a time', () => {
    const { result } = renderDialog();
    act(() => result.current.openDialog('clusters'));
    act(() => result.current.openDialog('settings'));
    expect(result.current.activeDialog).toBe('settings');
    expect(result.current.mountedDialog).toBe('settings');
  });

  it('reopens the dialog that is still animating out', () => {
    const { result } = renderDialog();
    act(() => result.current.openDialog('settings'));
    act(() => result.current.closeDialog());
    act(() => result.current.openDialog('settings'));
    expect(result.current.activeDialog).toBe('settings');
    expect(result.current.mountedDialog).toBe('settings');
  });

  it('carries the cluster detail the Clusters dialog was opened on', () => {
    const { result } = renderDialog();
    expect(result.current.clusterDetail).toBeNull();

    act(() => result.current.openDialog('clusters', { clusterId: 'c1', pane: 'connection' }));
    expect(result.current.clusterDetail).toEqual({ clusterId: 'c1', pane: 'connection' });
  });

  it('clears the cluster detail on an open that names none', () => {
    const { result } = renderDialog();
    act(() => result.current.openDialog('clusters', { clusterId: 'c1', pane: 'connection' }));
    act(() => result.current.openDialog('clusters'));
    expect(result.current.clusterDetail).toBeNull();
  });

  it('carries the incident the Incidents dialog was opened on, until an open that names none', () => {
    const { result } = renderDialog();
    expect(result.current.incidentID).toBeNull();

    act(() => result.current.openDialog('incidents', 'i1'));
    expect(result.current.incidentID).toBe('i1');

    act(() => result.current.openDialog('incidents'));
    expect(result.current.incidentID).toBeNull();
  });

  it('counts every open, the same dialog on the same detail included', () => {
    const { result } = renderDialog();
    expect(result.current.openSeq).toBe(0);
    act(() => result.current.openDialog('incidents', 'i1'));
    act(() => result.current.openDialog('incidents', 'i1'));
    expect(result.current.openSeq).toBe(2);
  });

  it('carries the Settings tab to open on, and clears what another dialog was opened on', () => {
    const { result } = renderDialog();
    act(() => result.current.openDialog('clusters', { clusterId: 'c1', pane: 'sync' }));
    act(() => result.current.openDialog('settings', 'Monitoring'));
    expect(result.current.settingsTab).toBe('Monitoring');
    expect(result.current.clusterDetail).toBeNull();
    expect(result.current.incidentID).toBeNull();
  });

  it('keeps openDialog and closeDialog the same across opens and closes', () => {
    const { result } = renderDialog();
    const { openDialog, closeDialog } = result.current;
    act(() => result.current.openDialog('incidents', 'i1'));
    act(() => result.current.closeDialog());
    expect(result.current.openDialog).toBe(openDialog);
    expect(result.current.closeDialog).toBe(closeDialog);
  });

  it('refuses to work outside a provider, which would silently drop opens', () => {
    expect(() => renderHook(() => useDialog())).toThrow(/within a DialogProvider/);
  });
});

describe('useDialogHost', () => {
  it('reads the host when there is one', () => {
    const { result } = renderHook(() => useDialogHost(), { wrapper });
    expect(result.current?.activeDialog).toBeNull();
  });

  it('reads null outside a provider, so a lone Dialog still works', () => {
    const { result } = renderHook(() => useDialogHost());
    expect(result.current).toBeNull();
  });
});
