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

import { fireEvent, render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { ClusterCachedDataObject } from '@/lib/cluster-cached-data-objects';

// The cards have suites of their own; one stands in, naming the object it draws and the
// events it is handed.
vi.mock('./cards', () => ({
  ObjectCards: ({
    object,
    namespaced,
    events,
  }: {
    object: ClusterCachedDataObject;
    namespaced: boolean;
    events: { events: unknown[]; phase: string };
  }) => (
    <div data-testid="cards" data-namespaced={namespaced} data-events={`${events.events.length}:${events.phase}`}>
      {object.rawJSON === undefined ? 'no body' : JSON.stringify(object.rawJSON)}
    </div>
  ),
}));

// Counts the detail's events watches as they mount, and keeps the options each was given.
const { eventsWatch } = vi.hoisted(() => ({ eventsWatch: { mounts: 0, options: [] as unknown[] } }));
vi.mock('@/lib/cluster-cached-data-events', async (importOriginal) => {
  const { useState } = await import('react');
  return {
    ...(await importOriginal<typeof import('@/lib/cluster-cached-data-events')>()),
    useClusterCachedDataEvents: (options: unknown) => {
      useState(() => {
        eventsWatch.mounts += 1;
        eventsWatch.options.push(options);
      });
      return { events: [{ uid: 'e1' }], active: true, phase: 'live' };
    },
  };
});

const { ObjectDetail } = await import('./detail');

const NOW = Date.parse('2026-11-22T11:14:00Z');
const PODS = { apiVersion: 'v1', resource: 'pods' };
const CONFIGMAPS = { apiVersion: 'v1', resource: 'configmaps' };

const POD: ClusterCachedDataObject = {
  uid: 'u1',
  apiVersion: 'v1',
  kind: 'Pod',
  namespace: 'default',
  name: 'worker-r7tnv',
  creationTimestamp: '2026-11-22T09:14:00Z',
  rawJSON: {
    metadata: {
      labels: { 'pod-template-hash': '6b7c9d8f5' },
      ownerReferences: [{ kind: 'ReplicaSet', name: 'worker-6b7c9d8f5', controller: true }],
    },
  },
};

type Props = Partial<Parameters<typeof ObjectDetail>[0]>;

function show(props: Props = {}) {
  const onAction = vi.fn();
  const onClose = vi.fn();
  const all = {
    uid: 'u1',
    gvr: PODS,
    kind: 'Pod',
    namespaced: true,
    object: POD,
    settled: true,
    onAction,
    onClose,
    ...props,
  };
  const view = render(<ObjectDetail {...all} />);
  const rerender = (next: Props) => view.rerender(<ObjectDetail {...all} {...next} />);
  return { onAction, onClose, rerender };
}

const detail = () => screen.getByRole('region', { name: 'Pod' });
const heading = () => screen.getByRole('heading', { level: 2 });
const subtitle = () => heading().parentElement!.nextElementSibling!;

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] });
  vi.setSystemTime(NOW);
});
afterEach(() => {
  vi.useRealTimers();
  eventsWatch.mounts = 0;
  eventsWatch.options = [];
});

describe('ObjectDetail', () => {
  it('heads the object with its name, its status and what it is', () => {
    show({ statusOf: () => ({ label: 'Running', tone: 'ok' }) });
    expect(heading()).toHaveTextContent('worker-r7tnv');
    expect(heading()).toHaveAttribute('title', 'worker-r7tnv');
    expect(heading()).toHaveClass('font-mono');
    expect(within(detail()).getByText('Running').closest('[data-tone]')).toHaveAttribute('data-tone', 'ok');
    expect(subtitle()).toHaveTextContent('Pod · deploy/worker · created 2h ago');
  });

  it('leaves out a status it cannot read, an owner and a creation time it lacks', () => {
    show({
      statusOf: () => undefined,
      object: { ...POD, rawJSON: {}, creationTimestamp: null },
    });
    expect(within(detail()).queryByText('Running')).not.toBeInTheDocument();
    expect(subtitle().textContent).toBe('Pod');
  });

  it('spells invisible characters in the name, its title and the close button', () => {
    show({ object: { ...POD, name: 'a​b' } });
    expect(heading()).toHaveTextContent('a\\u{200B}b');
    expect(heading()).toHaveAttribute('title', 'a\\u{200B}b');
    expect(screen.getByRole('button', { name: 'Close a\\u{200B}b' })).toBeInTheDocument();
  });

  it('draws the kind’s cards under the header, handing them its events watch', () => {
    show({ namespaced: false });
    expect(screen.getByTestId('cards')).toHaveAttribute('data-namespaced', 'false');
    expect(screen.getByTestId('cards')).toHaveAttribute('data-events', '1:live');
    expect(screen.getByTestId('cards').parentElement).toHaveClass('overflow-auto');
  });

  it('opens a log kind’s logs in the right sidebar', () => {
    const { onAction } = show();
    fireEvent.click(screen.getByRole('button', { name: 'Logs for worker-r7tnv' }));
    expect(onAction).toHaveBeenCalledWith('logs', POD);
  });

  it('offers no logs for a kind a log view cannot follow', () => {
    show({ gvr: CONFIGMAPS, kind: 'ConfigMap' });
    expect(screen.queryByRole('button', { name: /^Logs for/ })).not.toBeInTheDocument();
  });

  it('hands the menu’s action on with the object', async () => {
    vi.useRealTimers();
    const user = userEvent.setup();
    const { onAction } = show();
    await user.click(screen.getByRole('button', { name: 'Actions for worker-r7tnv' }));
    await user.click(await screen.findByRole('menuitem', { name: 'View YAML' }));
    expect(onAction).toHaveBeenCalledWith('yaml', POD);
  });

  it('closes on Escape from inside it, and on its X', () => {
    const { onClose } = show();
    const close = screen.getByRole('button', { name: 'Close worker-r7tnv' });
    fireEvent.keyDown(close, { key: 'Enter' });
    expect(onClose).not.toHaveBeenCalled();
    fireEvent.keyDown(close, { key: 'Escape' });
    fireEvent.click(close);
    expect(onClose).toHaveBeenCalledTimes(2);
  });

  it('takes focus as it opens', () => {
    show();
    expect(detail()).toHaveFocus();
  });

  it('draws the open object’s newest body', () => {
    const { rerender } = show();
    rerender({ object: { ...POD, rawJSON: { v: 2 } } });
    expect(screen.getByTestId('cards')).toHaveTextContent('{"v":2}');
  });

  it('holds the object while unsettled rows lack it, and says it is gone once they settle', () => {
    const { rerender } = show();
    rerender({ object: undefined, settled: false });
    expect(screen.getByTestId('cards')).toBeInTheDocument();
    expect(screen.queryByText('This object no longer exists.')).not.toBeInTheDocument();
    rerender({ object: undefined, settled: true });
    expect(screen.getByText('This object no longer exists.')).toBeInTheDocument();
    expect(screen.queryByTestId('cards')).not.toBeInTheDocument();
    expect(heading()).toHaveTextContent('worker-r7tnv');
  });

  it('stays gone through a reconnect once settled rows lack it, until it comes back', () => {
    const { rerender } = show();
    rerender({ object: undefined, settled: true });
    rerender({ object: undefined, settled: false });
    expect(screen.getByText('This object no longer exists.')).toBeInTheDocument();
    rerender({ object: POD, settled: true });
    expect(screen.queryByText('This object no longer exists.')).not.toBeInTheDocument();
  });

  it('keeps one events watch across the objects opened, mounting each object afresh', () => {
    const { rerender } = show();
    expect(detail()).toHaveFocus();
    (document.activeElement as HTMLElement).blur();
    rerender({ uid: 'u2', object: { ...POD, uid: 'u2', name: 'worker-x9k2p' } });
    expect(heading()).toHaveTextContent('worker-x9k2p');
    expect(detail()).toHaveFocus();
    expect(eventsWatch.mounts).toBe(1);
    expect(eventsWatch.options).toEqual([{ unshared: true }]);
  });

  it('draws no actions once the object is gone, so none reaches an object of the same name', () => {
    const { rerender } = show();
    rerender({ object: undefined, settled: true });
    expect(
      within(detail())
        .getAllByRole('button')
        .map((b) => b.getAttribute('aria-label')),
    ).toEqual(['Close worker-r7tnv']);
  });

  it('draws only its close button for an object it never saw', () => {
    show({ object: undefined, settled: false });
    expect(
      within(detail())
        .getAllByRole('button')
        .map((b) => b.getAttribute('aria-label')),
    ).toEqual(['Close']);
  });
});
