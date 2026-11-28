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

import { fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { ClusterCachedDataObject } from '@/lib/cluster-cached-data-objects';
import type { StatusTone } from '@/lib/cluster-health';
import { stubLayout } from '@/test-utils';

import { ObjectsList } from './list';
import type { ObjectsListProps } from './list';

const NOW = Date.parse('2026-11-22T11:14:00Z');

function obj(uid: string, over: Partial<ClusterCachedDataObject> = {}): ClusterCachedDataObject {
  return {
    uid,
    apiVersion: 'v1',
    kind: 'Pod',
    namespace: 'shop',
    name: uid,
    creationTimestamp: '2026-11-22T09:14:00Z',
    rawJSON: undefined,
    ...over,
  };
}

const owned = {
  metadata: {
    labels: { 'pod-template-hash': '6b7c9d8f5' },
    ownerReferences: [{ kind: 'ReplicaSet', name: 'worker-6b7c9d8f5', controller: true }],
  },
};

function show(props: Partial<ObjectsListProps> = {}) {
  const onOpen = vi.fn();
  render(
    <ObjectsList
      rows={[obj('a', { rawJSON: owned }), obj('b')]}
      inScope={3}
      plural="pods"
      showNamespace
      openUid="a"
      onOpen={onOpen}
      {...props}
    />,
  );
  return onOpen;
}

const row = (name: string) => screen.getByText(name, { selector: '.font-mono' }).closest('button')!;

stubLayout(400);

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] });
  vi.setSystemTime(NOW);
});
afterEach(() => vi.useRealTimers());

describe('ObjectsList', () => {
  it('draws a button per row, naming the object over its namespace and owner, with its age', () => {
    show();
    expect(row('a')).toHaveTextContent('ashop · deploy/worker2h');
    expect(row('b')).toHaveTextContent('bshop2h');
    expect(screen.queryAllByRole('columnheader')).toHaveLength(0);
  });

  it('leaves out the namespace while the table would not show it, and the line with nothing on it', () => {
    show({ showNamespace: false });
    expect(row('a')).toHaveTextContent('adeploy/worker2h');
    expect(row('b').querySelectorAll('.block')).toHaveLength(1);
  });

  it('marks the open row, and opens the one pressed', () => {
    const onOpen = show();
    expect(row('a')).toHaveAttribute('aria-current', 'true');
    expect(row('a')).toHaveClass('bg-sidebar-accent');
    expect(row('b')).not.toHaveAttribute('aria-current');
    fireEvent.click(row('b'));
    expect(onOpen).toHaveBeenCalledWith('b');
  });

  it('colours each dot by its tone, muted for a kind without one', () => {
    const toneOf = (o: ClusterCachedDataObject): StatusTone => (o.uid === 'a' ? 'failing' : 'ok');
    const { unmount } = render(
      <ObjectsList
        rows={[obj('a'), obj('b')]}
        inScope={2}
        plural="pods"
        showNamespace
        openUid="a"
        onOpen={vi.fn()}
        toneOf={toneOf}
      />,
    );
    const dot = (name: string) => row(name).querySelector('.rounded-full');
    expect(dot('a')).toHaveClass('size-1.75', 'bg-red-500');
    expect(dot('b')).toHaveClass('bg-emerald-500');
    unmount();
    show();
    expect(row('a').querySelector('.rounded-full')).toHaveClass('bg-muted-foreground');
  });

  it('spells invisible characters in the name, its title too', () => {
    show({ rows: [obj('a', { name: 'a​b' })] });
    const name = document.querySelector('button .font-mono')!;
    expect(name).toHaveTextContent('a\\u{200B}b');
    expect(name).toHaveAttribute('title', 'a\\u{200B}b');
  });

  it('counts the rows it shows against those in scope', () => {
    show();
    expect(screen.getByText(/^Showing/)).toHaveTextContent('Showing 2 of 3 pods');
  });
});
