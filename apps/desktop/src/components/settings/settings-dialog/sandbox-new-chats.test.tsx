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

import { cleanup, render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import type { SandboxDefault } from '@/lib/sandbox-default';

// The seams: each scope's watched default, and its change, recorded; the
// clusters dev and prod, dev listed first; the scope picker, a button that
// picks prod.
const { defaults, clusters, setMock, resetMock } = vi.hoisted(() => {
  const byScope: Record<string, SandboxDefault | undefined> = {};
  return {
    defaults: { byScope, switching: false, everyClusterChanged: false },
    clusters: { present: [] as { id: string }[] },
    setMock: vi.fn(),
    resetMock: vi.fn(),
  };
});
vi.mock('@/lib/sandbox-default', () => ({
  useSandboxDefault: (clusterID: string | null) => defaults.byScope[clusterID ?? ''],
  useSandboxDefaultSwitch: (clusterID: string | null) => ({
    switching: defaults.switching,
    setSandboxDisabled: (disabled: boolean) => setMock(clusterID, disabled),
    reset: (everyCluster: boolean) => resetMock(clusterID, everyCluster),
    everyClusterChanged: defaults.everyClusterChanged,
  }),
}));
vi.mock('@/lib/cluster-scope', () => ({
  useScope: (picked: string) => ({
    present: clusters.present,
    archived: [],
    cluster: clusters.present.find((c) => c.id === picked),
  }),
}));
vi.mock('./scope-picker', () => ({
  ScopePicker: ({ onPick, everyCluster }: { onPick: (picked: string) => void; everyCluster?: boolean }) => (
    <button type="button" data-every-cluster={String(everyCluster)} onClick={() => onPick('prod')}>
      pick prod
    </button>
  ),
}));

const { SandboxNewChats } = await import('./sandbox-new-chats');

const toggle = () => screen.getByRole('switch', { name: 'Start new chats in the sandbox' });
const SANDBOXED = { sandboxDisabled: false, clusterOverride: false };
const OUTSIDE = { sandboxDisabled: true, clusterOverride: false };
const OWN_SANDBOXED = { sandboxDisabled: false, clusterOverride: true };
const OWN_OUTSIDE = { sandboxDisabled: true, clusterOverride: true };

beforeEach(() => {
  vi.clearAllMocks();
  clusters.present = [{ id: 'dev' }, { id: 'prod' }];
  defaults.byScope = { '': SANDBOXED, dev: SANDBOXED, prod: SANDBOXED };
  defaults.switching = false;
  defaults.everyClusterChanged = false;
});

describe('SandboxNewChats', () => {
  // Every cluster's default is the status card's switch.
  it('picks among the clusters alone, the first listed until one is picked', async () => {
    const user = userEvent.setup();
    defaults.byScope = { '': SANDBOXED, dev: SANDBOXED, prod: OWN_OUTSIDE };
    render(<SandboxNewChats />);
    expect(screen.getByRole('button', { name: 'pick prod' })).toHaveAttribute('data-every-cluster', 'false');
    expect(toggle()).toBeChecked();

    await user.click(screen.getByRole('button', { name: 'pick prod' }));
    expect(toggle()).not.toBeChecked();
  });

  it('draws nothing with no cluster', () => {
    clusters.present = [];
    render(<SandboxNewChats />);
    expect(screen.queryByRole('switch')).toBeNull();
    expect(screen.queryByText('New chats on a cluster')).toBeNull();
  });

  it('draws nothing until the default arrives', () => {
    defaults.byScope = {};
    render(<SandboxNewChats />);
    expect(screen.queryByRole('switch')).toBeNull();
  });

  it("draws a cluster's default, worded as what new chats start with", () => {
    render(<SandboxNewChats />);
    expect(toggle()).toBeChecked();
    expect(screen.getByText(/Chats that exist keep their own switch/)).toBeInTheDocument();
    expect(screen.getByText(/Follows every cluster's setting/)).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: "Use every cluster's" })).toBeNull();
  });

  // Widening what a new chat's commands reach is said before it is done.
  it('starts new chats outside only once the dialog is confirmed', async () => {
    const user = userEvent.setup();
    render(<SandboxNewChats />);

    await user.click(toggle());
    let dialog = await screen.findByRole('dialog', { name: 'Start new chats outside the sandbox?' });
    await user.click(within(dialog).getByRole('button', { name: 'Cancel' }));
    expect(setMock).not.toHaveBeenCalled();

    await user.click(toggle());
    dialog = await screen.findByRole('dialog', { name: 'Start new chats outside the sandbox?' });
    await user.click(within(dialog).getByRole('button', { name: 'Start outside the sandbox' }));
    expect(setMock).toHaveBeenCalledWith('dev', true);
  });

  it('keeps new chats in the sandbox when the dialog is dismissed', async () => {
    const user = userEvent.setup();
    render(<SandboxNewChats />);
    await user.click(toggle());
    await screen.findByRole('dialog');
    await user.keyboard('{Escape}');
    expect(screen.queryByRole('dialog')).toBeNull();
    expect(setMock).not.toHaveBeenCalled();
  });

  // Narrowing needs no warning.
  it('starts new chats in the sandbox again at once', async () => {
    defaults.byScope = { '': OUTSIDE, dev: OUTSIDE };
    const user = userEvent.setup();
    render(<SandboxNewChats />);
    expect(toggle()).not.toBeChecked();

    await user.click(toggle());
    expect(setMock).toHaveBeenCalledWith('dev', false);
    expect(screen.queryByRole('dialog')).toBeNull();
  });

  it('holds the switch while a change is in flight', () => {
    defaults.switching = true;
    render(<SandboxNewChats />);
    expect(toggle()).toHaveAttribute('aria-disabled', 'true');
  });

  it("takes a cluster back to every cluster's at once when that keeps it in the sandbox", async () => {
    defaults.byScope = { '': SANDBOXED, dev: OWN_OUTSIDE };
    const user = userEvent.setup();
    render(<SandboxNewChats />);

    await user.click(screen.getByRole('button', { name: "Use every cluster's" }));
    expect(resetMock).toHaveBeenCalledWith('dev', false);
    expect(screen.queryByRole('dialog')).toBeNull();
  });

  // Whether a reset confirms rests on every cluster's value, so it waits for it.
  it("holds the reset until every cluster's default arrives", () => {
    defaults.byScope = { dev: OWN_SANDBOXED };
    render(<SandboxNewChats />);
    expect(screen.getByRole('button', { name: "Use every cluster's" })).toBeDisabled();
  });

  // A reset that takes the cluster outside widens it, so it confirms too.
  it('confirms a reset that takes a cluster outside', async () => {
    defaults.byScope = { '': OUTSIDE, dev: OWN_SANDBOXED };
    const user = userEvent.setup();
    render(<SandboxNewChats />);

    await user.click(screen.getByRole('button', { name: "Use every cluster's" }));
    const dialog = await screen.findByRole('dialog', { name: 'Start new chats outside the sandbox?' });
    expect(resetMock).not.toHaveBeenCalled();
    await user.click(within(dialog).getByRole('button', { name: 'Start outside the sandbox' }));
    expect(resetMock).toHaveBeenCalledWith('dev', true);
  });

  // The cluster's value on screen can be stale, so a reset that lands outside
  // confirms even from a cluster already outside.
  it('confirms a reset that lands outside from a cluster already outside', async () => {
    defaults.byScope = { '': OUTSIDE, dev: OWN_OUTSIDE };
    const user = userEvent.setup();
    render(<SandboxNewChats />);

    await user.click(screen.getByRole('button', { name: "Use every cluster's" }));
    expect(await screen.findByRole('dialog', { name: 'Start new chats outside the sandbox?' })).toBeInTheDocument();
    expect(resetMock).not.toHaveBeenCalled();
  });

  it("says when a reset was refused because every cluster's default changed", () => {
    render(<SandboxNewChats />);
    expect(screen.queryByText(/default changed/)).toBeNull();
    cleanup();

    defaults.everyClusterChanged = true;
    render(<SandboxNewChats />);
    expect(screen.getByText("Every cluster's default changed. Check it, then try again.")).toBeInTheDocument();
  });
});
